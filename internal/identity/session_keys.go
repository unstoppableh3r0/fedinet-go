package identity

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

const (
	SessionKeyRotationPeriod = 24 * time.Hour
	SessionKeyGracePeriod    = 2 * time.Hour
	AESKeySize               = 32 // AES-256
)

// SessionKey represents a user's symmetric encryption key
type SessionKey struct {
	ID                    string
	UserID                string
	SymmetricKeyEncrypted string
	KeyVersion            int
	Signature             string
	CreatedAt             time.Time
	ExpiresAt             time.Time
	IsActive              bool
}

// GenerateSessionKey creates a new AES-256 session key for a user
func GenerateSessionKey(userID string) (*SessionKey, error) {
	// Generate random AES-256 key
	keyBytes := make([]byte, AESKeySize)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}
	symmetricKey := hex.EncodeToString(keyBytes)

	// Get SERVER_MASTER_KEY for encryption
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
		log.Println("WARNING: Using insecure default SERVER_MASTER_KEY")
	}

	// Encrypt the symmetric key
	encryptedKey, err := crypto.Encrypt(symmetricKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt session key: %w", err)
	}

	// Get server's private key to sign the session key
	var serverPrivKeyEncrypted string
	err = db.QueryRow("SELECT private_key_encrypted FROM server_identity WHERE id = 1").Scan(&serverPrivKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to get server private key: %w", err)
	}

	serverPrivKey, err := crypto.Decrypt(serverPrivKeyEncrypted, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt server private key: %w", err)
	}

	// Sign the encrypted session key
	signature, err := crypto.SignData([]byte(encryptedKey), serverPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign session key: %w", err)
	}

	// Get current max version for this user
	var maxVersion int
	err = db.QueryRow(`
		SELECT COALESCE(MAX(key_version), 0) 
		FROM user_session_keys 
		WHERE user_id = $1
	`, userID).Scan(&maxVersion)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get max key version: %w", err)
	}

	newVersion := maxVersion + 1
	now := time.Now()
	expiresAt := now.Add(SessionKeyRotationPeriod)

	// Insert into database
	var id string
	err = db.QueryRow(`
		INSERT INTO user_session_keys (
			user_id, symmetric_key_encrypted, key_version, 
			signature, created_at, expires_at, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, TRUE)
		RETURNING id
	`, userID, encryptedKey, newVersion, signature, now, expiresAt).Scan(&id)

	if err != nil {
		return nil, fmt.Errorf("failed to store session key: %w", err)
	}

	return &SessionKey{
		ID:                    id,
		UserID:                userID,
		SymmetricKeyEncrypted: encryptedKey,
		KeyVersion:            newVersion,
		Signature:             signature,
		CreatedAt:             now,
		ExpiresAt:             expiresAt,
		IsActive:              true,
	}, nil
}

// GetActiveSessionKey retrieves the current active session key for a user
func GetActiveSessionKey(userID string) (*SessionKey, error) {
	var sk SessionKey
	err := db.QueryRow(`
		SELECT id, user_id, symmetric_key_encrypted, key_version, 
			   signature, created_at, expires_at, is_active
		FROM user_session_keys
		WHERE user_id = $1 AND is_active = TRUE AND expires_at > NOW()
		ORDER BY key_version DESC
		LIMIT 1
	`, userID).Scan(
		&sk.ID, &sk.UserID, &sk.SymmetricKeyEncrypted, &sk.KeyVersion,
		&sk.Signature, &sk.CreatedAt, &sk.ExpiresAt, &sk.IsActive,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active session key found for user %s", userID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active session key: %w", err)
	}

	return &sk, nil
}

// DecryptSessionKey decrypts a session key for use
func DecryptSessionKey(sk *SessionKey) (string, error) {
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	return crypto.Decrypt(sk.SymmetricKeyEncrypted, masterKey)
}

// RotateSessionKey creates a new session key and deactivates the old one
func RotateSessionKey(userID string) (*SessionKey, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Deactivate old keys but keep them for grace period
	_, err = tx.Exec(`
		UPDATE user_session_keys 
		SET is_active = FALSE 
		WHERE user_id = $1 AND is_active = TRUE
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to deactivate old keys: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Generate new key
	newKey, err := GenerateSessionKey(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new session key: %w", err)
	}

	log.Printf("Rotated session key for user %s to version %d", userID, newKey.KeyVersion)
	return newKey, nil
}

// CleanupExpiredKeys removes expired session keys beyond grace period
func CleanupExpiredKeys() error {
	cutoff := time.Now().Add(-SessionKeyGracePeriod)

	result, err := db.Exec(`
		DELETE FROM user_session_keys 
		WHERE expires_at < $1
	`, cutoff)

	if err != nil {
		return fmt.Errorf("failed to cleanup expired keys: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("Cleaned up %d expired session keys", rows)
	}

	return nil
}

// RotateExpiredKeys rotates session keys that are due for rotation
func RotateExpiredKeys() error {
	rows, err := db.Query(`
		SELECT DISTINCT user_id 
		FROM user_session_keys 
		WHERE is_active = TRUE 
		AND expires_at < NOW()
	`)
	if err != nil {
		return fmt.Errorf("failed to query expired keys: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			log.Printf("Error scanning user_id: %v", err)
			continue
		}

		if _, err := RotateSessionKey(userID); err != nil {
			log.Printf("Failed to rotate key for user %s: %v", userID, err)
			continue
		}
		count++
	}

	if count > 0 {
		log.Printf("Rotated %d expired session keys", count)
	}
	return nil
}
