package identity

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

func BlockUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		BlockerID string `json:"blocker_id"`
		BlockedID string `json:"blocked_id"`
		Reason    string `json:"reason"`
		Signature string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var blockerPubKey string
	err := db.QueryRow("SELECT public_key FROM identities WHERE user_id=$1", req.BlockerID).Scan(&blockerPubKey)
	if err != nil {
		if err == sql.ErrNoRows {
			RespondWithError(w, http.StatusNotFound, "blocker identity not found")
			return
		}
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	msg := []byte("BLOCK:" + req.BlockerID + ":" + req.BlockedID + ":" + req.Reason)

	valid, err := crypto.VerifySignature(msg, req.Signature, blockerPubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	_, err = db.Exec(`
        INSERT INTO block_events (blocker_id, blocked_id, reason, signature, created_at)
        VALUES ($1, $2, $3, $4, NOW())
        ON CONFLICT (blocker_id, blocked_id) DO UPDATE SET
            reason = $3,
            signature = $4,
            created_at = NOW()
    `, req.BlockerID, req.BlockedID, req.Reason, req.Signature)

	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to record block")
		return
	}

	if err := propagateBlock(req.BlockerID, req.BlockedID); err != nil {

	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "user blocked"})
}

func UnblockUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		BlockerID string `json:"blocker_id"`
		BlockedID string `json:"blocked_id"`
		Signature string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var blockerPubKey string
	err := db.QueryRow("SELECT public_key FROM identities WHERE user_id=$1", req.BlockerID).Scan(&blockerPubKey)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "blocker not found")
		return
	}

	msg := []byte("UNBLOCK:" + req.BlockerID + ":" + req.BlockedID)

	valid, err := crypto.VerifySignature(msg, req.Signature, blockerPubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	_, err = db.Exec(`DELETE FROM block_events WHERE blocker_id=$1 AND blocked_id=$2`, req.BlockerID, req.BlockedID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "user unblocked"})
}

func GetBlocksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	rows, err := db.Query(`
        SELECT blocked_id, reason, created_at, signature 
        FROM block_events 
        WHERE blocker_id = $1
    `, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	var blocks []models.BlockEvent
	for rows.Next() {
		var b models.BlockEvent
		b.BlockerID = userID
		if err := rows.Scan(&b.BlockedID, &b.Reason, &b.CreatedAt, &b.Signature); err != nil {
			continue
		}
		blocks = append(blocks, b)
	}

	RespondWithJSON(w, http.StatusOK, blocks)
}

func propagateBlock(blockerID, blockedID string) error {

	parts := strings.Split(blockedID, "@")
	if len(parts) < 2 {

		return nil
	}
	targetServer := parts[1]

	payload := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"type":     "Block",
		"actor":    blockerID,
		"object":   blockedID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
        INSERT INTO outbox_activities (
            activity_type, actor_id, target_server, target_id, payload, delivery_status
        ) VALUES ($1, $2, $3, $4, $5, 'pending')
    `, "Block", blockerID, targetServer, blockedID, payloadBytes)

	return err
}
