package identity

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)


func RevokeKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		IdentityID uuid.UUID `json:"identity_id"`
		KeyID      string    `json:"key_id"` 
		Reason     string    `json:"reason"`
		Signature  string    `json:"signature"` 
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	
	var currentPubKey string
	err := db.QueryRow("SELECT public_key FROM identities WHERE id=$1", req.IdentityID).Scan(&currentPubKey)
	if err != nil {
		if err == sql.ErrNoRows {
			RespondWithError(w, http.StatusNotFound, "identity not found")
			return
		}
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	
	
	
	msg := []byte("REVOKE:" + req.KeyID + ":" + req.Reason)

	
	
	

	valid, err := crypto.VerifySignature(msg, req.Signature, currentPubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	
	_, err = db.Exec(`
        INSERT INTO key_revocations (key_id, identity_id, reason, signature, revoked_at)
        VALUES ($1, $2, $3, $4, NOW())
    `, req.KeyID, req.IdentityID, req.Reason, req.Signature)

	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to record revocation")
		return
	}

	
	go propagateRevocation(req.IdentityID, req.KeyID, req.Reason, req.Signature)

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "key revoked"})
}


func GetRevocationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	identityIDStr := r.URL.Query().Get("identity_id")
	if identityIDStr == "" {
		RespondWithError(w, http.StatusBadRequest, "identity_id required")
		return
	}

	identityID, err := uuid.Parse(identityIDStr)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid UUID")
		return
	}

	rows, err := db.Query(`
        SELECT key_id, reason, revoked_at, signature 
        FROM key_revocations 
        WHERE identity_id = $1
    `, identityID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	var revocations []models.KeyRevocation
	for rows.Next() {
		var r models.KeyRevocation
		r.IdentityID = identityID
		if err := rows.Scan(&r.KeyID, &r.Reason, &r.RevokedAt, &r.Signature); err != nil {
			continue
		}
		revocations = append(revocations, r)
	}

	RespondWithJSON(w, http.StatusOK, revocations)
}

func propagateRevocation(identityID uuid.UUID, keyID, reason, signature string) {
	
	var userID string
	err := db.QueryRow("SELECT user_id FROM identities WHERE id=$1", identityID).Scan(&userID)
	if err != nil {
		return
	}

	
	rows, err := db.Query(`
        SELECT DISTINCT follower_home_server 
        FROM follows 
        WHERE followee_user_id = $1 
        AND follower_home_server != 'http://localhost:8080'
        AND follower_home_server != ''
    `, userID)
	if err != nil {
		return
	}
	defer rows.Close()

	var servers []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			servers = append(servers, s)
		}
	}

	if len(servers) == 0 {
		return
	}

	
	payload := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"type":     "RevokeKey", 
		"actor":    userID,
		"object": map[string]string{
			"key_id":    keyID,
			"reason":    reason,
			"signature": signature,
		},
	}

	payloadBytes, _ := json.Marshal(payload)

	
	for _, server := range servers {
		db.Exec(`
            INSERT INTO outbox_activities (
                activity_type, actor_id, target_server, payload, delivery_status
            ) VALUES ($1, $2, $3, $4, 'pending')
        `, "RevokeKey", userID, server, payloadBytes)
	}
}


func IsKeyRevoked(keyID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM key_revocations WHERE key_id=$1)", keyID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
