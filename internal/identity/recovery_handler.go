package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

func RecoverAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID      string `json:"user_id"`
		RecoveryKey string `json:"recovery_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.RecoveryKey == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and recovery_key required")
		return
	}

	internalID := ToInternalID(req.UserID)

	identity, err := GetIdentityByUserID(internalID)
	if err != nil {
		log.Println("Recovery lookup error:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if identity == nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	inputHash := crypto.HashString(req.RecoveryKey)

	if inputHash != identity.RecoveryKeyHash {
		log.Printf("Recovery failed for %s: hash mismatch", req.UserID)
		RespondWithError(w, http.StatusUnauthorized, "invalid recovery key")
		return
	}

	newPubKey, newPrivKey, err := crypto.GenerateKeyPair()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "key generation failed")
		return
	}

	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	encryptedPrivKey, err := crypto.Encrypt(newPrivKey, masterKey)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	newRecoveryKey, newRecoveryHash, err := crypto.GenerateRecoveryKey()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "recovery key generation failed")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO key_revocations (key_id, identity_id, reason, revoked_at, signature)
		VALUES ($1, $2, 'account_recovery', NOW(), '')
	`, identity.PublicKey, identity.ID)

	if err != nil {
		log.Println("Revocation insert failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "revocation failed")
		return
	}

	_, err = tx.Exec(`
		UPDATE identities 
		SET public_key=$1, private_key=$2, key_version=key_version+1, recovery_key_hash=$3, updated_at=NOW()
		WHERE id=$4
	`, newPubKey, encryptedPrivKey, newRecoveryHash, identity.ID)

	if err != nil {
		log.Println("models.Identity update failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "update failed")
		return
	}

	if err := tx.Commit(); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}

	accessToken, refreshToken, err := GenerateTokenPair(identity.UserID, externalURL)
	if err != nil {
		log.Println("Token generation failed after recovery:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message":          "account recovered successfully",
		"new_recovery_key": newRecoveryKey,
		"user_id":          ToExternalID(identity.UserID),
		"home_server":      externalURL,
		"access_token":     accessToken,
		"refresh_token":    refreshToken,
	})
}
