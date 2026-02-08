package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// BlockUserHandler handles block requests
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

	// 1. Verify Blocker exists and get Public Key
	// Note: IDs in request are likely external IDs (username@server) or internal user_ids.
	// Models use 'string' for IDs.
	// Let's assume they are the 'user_id' string stored in database.

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

	// 2. Verify Signature
	// Payload: "BLOCK:{blocker_id}:{blocked_id}:{reason}"
	msg := []byte("BLOCK:" + req.BlockerID + ":" + req.BlockedID + ":" + req.Reason)

	valid, err := crypto.VerifySignature(msg, req.Signature, blockerPubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// 3. Insert Block Event
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

	// Trigger federation propagation
	if err := propagateBlock(req.BlockerID, req.BlockedID); err != nil {
		// Log error but don't fail the request as the local block is recorded
		// In production, use a background job
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "user blocked"})
}

// UnblockUserHandler handles unblock requests
func UnblockUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Similar to Block, but deletes from DB.
	// Also needs signature verification to prevent unauthorized unblocks.

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

	// Payload: "UNBLOCK:{blocker_id}:{blocked_id}"
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

// GetBlocksHandler lists users blocked by identity
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

	var blocks []BlockEvent
	for rows.Next() {
		var b BlockEvent
		b.BlockerID = userID
		if err := rows.Scan(&b.BlockedID, &b.Reason, &b.CreatedAt, &b.Signature); err != nil {
			continue
		}
		blocks = append(blocks, b)
	}

	RespondWithJSON(w, http.StatusOK, blocks)
}

func propagateBlock(blockerID, blockedID string) error {
	// 1. Check if blocked user is remote
	// IDs are like "user@server" or "user" (local)
	parts := strings.Split(blockedID, "@")
	if len(parts) < 2 {
		// Local user, no federation needed (or internal block)
		return nil
	}
	targetServer := parts[1]

	// 2. Construct Payload
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

	// 3. Insert into Outbox
	// Assuming outbox_activities table exists in the same DB (shared DB architecture)
	_, err = db.Exec(`
        INSERT INTO outbox_activities (
            activity_type, actor_id, target_server, target_id, payload, delivery_status
        ) VALUES ($1, $2, $3, $4, $5, 'pending')
    `, "Block", blockerID, targetServer, blockedID, payloadBytes)

	return err
}
