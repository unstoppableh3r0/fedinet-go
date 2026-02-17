package identity

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// CreateAccountWithClientKey creates a new user account with optional client public key
func CreateAccountWithClientKey(userID, homeServer, passwordHash, clientPublicKey string) (string, error) {
	if !ValidateUserID(userID) {
		return "", fmt.Errorf("invalid user_id format")
	}

	// Generate server-managed key pair for the user
	pubKey, privKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", err
	}

	// Generate recovery key
	recoveryKey, recoveryHash, err := crypto.GenerateRecoveryKey()
	if err != nil {
		return "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Check if user exists
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)", userID).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("user already exists")
	}

	// Encrypt private key
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
		fmt.Println("WARNING: Using insecure default SERVER_MASTER_KEY")
	}

	encryptedPrivKey, err := crypto.Encrypt(privKey, masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Generate DID
	did := "did:fedinet:" + crypto.HashString(pubKey)

	// Insert identity with client_public_key if provided
	identityID := uuid.New()
	_, err = tx.Exec(`
		INSERT INTO identities (
			id, did, user_id, home_server, public_key, private_key, 
			key_version, recovery_key_hash, password_hash, client_public_key,
			allow_discovery, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, true, NOW(), NOW())
	`, identityID, did, userID, homeServer, pubKey, encryptedPrivKey, recoveryHash, passwordHash, clientPublicKey)
	if err != nil {
		return "", err
	}

	// Create profile
	_, err = tx.Exec(`
		INSERT INTO profiles (
			user_id, display_name, bio, location, 
			followers_visibility, following_visibility, created_at, updated_at, version
		) VALUES (
			$1, $2, 'Just joined Gotham Social', 'Unknown',
			'public', 'public', NOW(), NOW(), 1
		)
	`, userID, userID)
	if err != nil {
		return "", err
	}

	return recoveryKey, tx.Commit()
}
