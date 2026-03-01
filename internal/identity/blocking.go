package identity

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
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
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.BlockerID == "" || req.BlockedID == "" {
		RespondWithError(w, http.StatusBadRequest, "blocker_id and blocked_id are required")
		return
	}

	if req.BlockerID == req.BlockedID {
		RespondWithError(w, http.StatusBadRequest, "cannot block yourself")
		return
	}

	internalBlockerID := ToInternalID(req.BlockerID)
	internalBlockedID := ToInternalID(req.BlockedID)

	_, err := db.Exec(`
        INSERT INTO block_events (blocker_id, blocked_id, reason, created_at)
        VALUES ($1, $2, $3, NOW())
        ON CONFLICT (blocker_id, blocked_id) DO UPDATE SET
            reason = $3,
            created_at = NOW()
    `, internalBlockerID, internalBlockedID, req.Reason)

	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to record block")
		return
	}

	if err := propagateBlock(req.BlockerID, req.BlockedID); err != nil {
		// Log but don't fail the request
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
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.BlockerID == "" || req.BlockedID == "" {
		RespondWithError(w, http.StatusBadRequest, "blocker_id and blocked_id are required")
		return
	}

	internalBlockerID := ToInternalID(req.BlockerID)
	internalBlockedID := ToInternalID(req.BlockedID)

	_, err := db.Exec(`DELETE FROM block_events WHERE blocker_id=$1 AND blocked_id=$2`, internalBlockerID, internalBlockedID)
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

	internalUserID := ToInternalID(userID)

	rows, err := db.Query(`
        SELECT blocked_id, reason, created_at
        FROM block_events 
        WHERE blocker_id = $1
    `, internalUserID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	var blocks []models.BlockEvent
	for rows.Next() {
		var b models.BlockEvent
		b.BlockerID = userID
		if err := rows.Scan(&b.BlockedID, &b.Reason, &b.CreatedAt); err != nil {
			continue
		}
		b.BlockedID = ToExternalID(b.BlockedID)
		blocks = append(blocks, b)
	}

	if blocks == nil {
		blocks = []models.BlockEvent{}
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
