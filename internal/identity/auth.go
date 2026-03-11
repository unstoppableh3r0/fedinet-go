// Package identity implements the core identity service for the federated social network.
// It handles user registration, authentication, profile management, post operations,
// federated follow/messaging, privacy controls, notifications, and moderation.
//
// The identity service is the backbone of each federated server node. Every server that
// participates in the federation runs an instance of this service. Users are homed on
// exactly one server (their "home server"), and cross-server interactions are handled
// via the federation layer.
package identity

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// UserClaims holds the JWT payload for an authenticated user session.
// It embeds the standard jwt.RegisteredClaims so that expiry, issuance time,
// and issuer fields are handled automatically by the JWT library.
//
// Fields:
//   - UserID: the fully-qualified user identifier in "user@server" format.
//   - HomeServer: the base URL of the server that issued the token (e.g. "http://localhost:8080").
//     This is included so that downstream services can determine where to route
//     federated requests without an additional database lookup.
type UserClaims struct {
	UserID     string `json:"user_id"`
	HomeServer string `json:"home_server"`
	jwt.RegisteredClaims
}

// HashPassword hashes a plaintext password using bcrypt at the default cost factor.
// The resulting hash is safe to store in the database; the original password is never
// persisted. bcrypt.DefaultCost is currently 10 rounds, which balances security with
// performance on commodity hardware. Increase the cost constant for higher-value accounts
// if the attack surface warrants it.
//
// Returns the bcrypt hash string and any error from the crypto library.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash compares a plaintext password against a stored bcrypt hash.
// It uses constant-time comparison internally, making it safe against timing attacks.
// Returns true only when the password matches the stored hash exactly.
//
// Note: this function deliberately swallows the bcrypt error because all error paths
// (wrong password, malformed hash, etc.) correctly map to "authentication failed".
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateTokenPair creates a short-lived access token and a long-lived refresh token
// for the given user. Both tokens are signed HS256 JWTs using the shared JWT_SECRET
// environment variable.
//
// Token lifetimes:
//   - Access token:  15 minutes  — used for authenticating API requests.
//   - Refresh token: 7 days      — used to obtain a new access token without re-login.
//
// The issuer field differentiates the two types ("federated-identity" vs
// "federated-identity-refresh"), preventing a refresh token from being accepted
// in place of an access token by handlers that only call ValidateUserToken.
//
// Returns (accessToken, refreshToken, error). On any signing error both token strings
// are returned as empty strings.
func GenerateTokenPair(userID, homeServer string) (string, string, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", "", fmt.Errorf("JWT_SECRET not set")
	}

	// Build the short-lived access token.
	// Clients include this in the Authorization: Bearer <token> header for every
	// authenticated request. A 15-minute window limits the blast radius of a
	// leaked access token.
	accessClaims := UserClaims{
		UserID:     userID,
		HomeServer: homeServer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-identity",
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessString, err := accessToken.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", err
	}

	// Build the long-lived refresh token.
	// The client stores this securely (e.g. httpOnly cookie or encrypted local storage)
	// and uses it to call /refresh-token when the access token expires.
	// A 7-day window strikes a balance between user convenience and security.
	refreshClaims := UserClaims{
		UserID:     userID,
		HomeServer: homeServer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-identity-refresh",
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshString, err := refreshToken.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", err
	}

	return accessString, refreshString, nil
}

// ValidateUserToken parses and verifies a JWT access token string.
// It rejects tokens that:
//   - Are signed with a non-HMAC algorithm (guards against the "alg:none" attack).
//   - Have expired (jwt library handles this via RegisteredClaims.ExpiresAt).
//   - Have an invalid signature (tampered payload or wrong secret).
//   - Cannot be cast to *UserClaims (structural mismatch, e.g. an arbitrary JWT).
//
// On success it returns the embedded *UserClaims so callers can access UserID and
// HomeServer without another database round-trip. On failure it returns nil and a
// descriptive error suitable for logging (do not surface the raw error to clients).
func ValidateUserToken(tokenString string) (*UserClaims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET not set")
	}

	// ParseWithClaims verifies the signature, expiry, and structural validity in one call.
	// The key function also enforces the expected signing algorithm, preventing downgrade attacks.
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
