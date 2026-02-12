package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Token expiry durations
const (
	AccessTokenExpiry  = 15 * time.Minute
	RefreshTokenExpiry = 7 * 24 * time.Hour
)

// UserClaims represents JWT claims for regular users
type UserClaims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// RefreshToken represents a stored refresh token
type RefreshToken struct {
	ID         string
	UserID     string
	TokenHash  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Revoked    bool
	RevokedAt  *time.Time
	DeviceInfo string
	IPAddress  string
}

// GenerateAccessToken creates a short-lived JWT access token
func GenerateAccessToken(userID string) (string, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", fmt.Errorf("JWT_SECRET not set in environment")
	}

	claims := UserClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-backend",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// GenerateRefreshToken creates a long-lived refresh token and stores it in the database
func GenerateRefreshToken(userID, deviceInfo, ipAddress string) (string, error) {
	// Generate a random refresh token
	tokenID := uuid.New().String()
	rawToken := fmt.Sprintf("%s:%s", userID, tokenID)

	// Hash the token for storage
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Store in database
	expiresAt := time.Now().Add(RefreshTokenExpiry)
	_, err := db.Exec(`
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, device_info, ip_address)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, tokenHash, expiresAt, deviceInfo, ipAddress)

	if err != nil {
		return "", fmt.Errorf("failed to store refresh token: %w", err)
	}

	log.Printf("Refresh token generated for user %s", userID)
	return rawToken, nil
}

// ValidateAccessToken validates a JWT access token and returns the claims
func ValidateAccessToken(tokenString string) (*UserClaims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET not set in environment")
	}

	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ValidateRefreshToken validates a refresh token and returns the associated user ID
func ValidateRefreshToken(rawToken string) (string, error) {
	// Hash the provided token
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Look up token in database
	var userID string
	var expiresAt time.Time
	var revoked bool

	err := db.QueryRow(`
		SELECT user_id, expires_at, revoked
		FROM refresh_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&userID, &expiresAt, &revoked)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("invalid refresh token")
	}
	if err != nil {
		return "", fmt.Errorf("database error: %w", err)
	}

	// Check if token is revoked
	if revoked {
		return "", fmt.Errorf("refresh token has been revoked")
	}

	// Check if token is expired
	if time.Now().After(expiresAt) {
		return "", fmt.Errorf("refresh token has expired")
	}

	return userID, nil
}

// RevokeRefreshToken marks a refresh token as revoked
func RevokeRefreshToken(rawToken string) error {
	// Hash the provided token
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Mark as revoked in database
	result, err := db.Exec(`
		UPDATE refresh_tokens
		SET revoked = true, revoked_at = NOW()
		WHERE token_hash = $1 AND revoked = false
	`, tokenHash)

	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("token not found or already revoked")
	}

	log.Printf("Refresh token revoked: %s", tokenHash[:16]+"...")
	return nil
}

// RevokeAllUserTokens revokes all refresh tokens for a specific user
func RevokeAllUserTokens(userID string) error {
	_, err := db.Exec(`
		UPDATE refresh_tokens
		SET revoked = true, revoked_at = NOW()
		WHERE user_id = $1 AND revoked = false
	`, userID)

	if err != nil {
		return fmt.Errorf("failed to revoke user tokens: %w", err)
	}

	log.Printf("All tokens revoked for user %s", userID)
	return nil
}

// CleanupExpiredTokens removes expired refresh tokens from the database
func CleanupExpiredTokens() {
	for {
		time.Sleep(1 * time.Hour)

		result, err := db.Exec(`
			DELETE FROM refresh_tokens
			WHERE expires_at < NOW() OR (revoked = true AND revoked_at < NOW() - INTERVAL '7 days')
		`)

		if err != nil {
			log.Printf("Error cleaning up expired tokens: %v", err)
			continue
		}

		rows, _ := result.RowsAffected()
		if rows > 0 {
			log.Printf("Cleaned up %d expired/revoked refresh tokens", rows)
		}
	}
}

// GetIdentityByEmail retrieves a user identity by email address
func GetIdentityByEmail(email string) (*struct {
	ID           uuid.UUID
	UserID       string
	HomeServer   string
	Email        string
	PasswordHash string
}, error) {
	var identity struct {
		ID           uuid.UUID
		UserID       string
		HomeServer   string
		Email        string
		PasswordHash string
	}

	err := db.QueryRow(`
		SELECT id, user_id, home_server, email, password_hash
		FROM identities
		WHERE email = $1
	`, email).Scan(&identity.ID, &identity.UserID, &identity.HomeServer, &identity.Email, &identity.PasswordHash)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &identity, nil
}
