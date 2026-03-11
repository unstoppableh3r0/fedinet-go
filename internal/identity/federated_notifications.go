package identity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// FEDERATION PROTOCOL: NOTIFICATION SYNCHRONIZATION
// ============================================================================

// FederatedNotificationRequest acts as the DTO (Data Transfer Object) for
// transmitting social alerts between instances. It bridges the gap between
// internal system events and the federated ActivityStreams standard.
type FederatedNotificationRequest struct {
	// RecipientID is the fully qualified target (e.g., bob@server_b).
	RecipientID string `json:"recipient_id"`

	// ActorID is the user who triggered the event (e.g., alice@server_a).
	ActorID string `json:"actor_id"`

	// Type defines the notification category (FOLLOW, LIKE, REPLY, etc.).
	Type string `json:"type"`

	// EntityID is the primary object of the notification (e.g., a Post ID).
	EntityID string `json:"entity_id"`

	// ActivityStream contains the raw AS2 JSON-LD payload for semantic interop.
	ActivityStream json.RawMessage `json:"activity_stream,omitempty"`

	// ServerID identifies the sending instance for trust verification.
	ServerID string `json:"server_id"`

	// Timestamp provides an audit trail and protects against replay attacks.
	Timestamp string `json:"timestamp"`
}

// DeliverFederatedNotification is the "Outbound Relay". It handles the
// delivery of a social event to a remote user's home instance.
//
// Parameters:
// - recipientID: The remote user to notify.
// - actorID: The local user who performed the action.
// - typeStr: The internal notification type.
// - entityID: The ID of the post or entity involved.
// - as2Bytes: The pre-rendered ActivityStreams JSON.
func DeliverFederatedNotification(recipientID, actorID, typeStr, entityID string, as2Bytes []byte) {
	// 1. HOST DISCOVERY
	// Extract the target domain from the Recipient ID.
	parts := strings.Split(recipientID, "@")
	if len(parts) != 2 {
		log.Printf("DeliverFederatedNotification: invalid recipient ID %s", recipientID)
		return
	}
	targetServer := parts[1]

	// 2. TRUST ESTABLISHMENT
	// Ensure the remote server is trusted and we have their public key.
	// If unknown, this triggers a handshake to verify the remote instance.
	remoteServer, _, err := EnsureServerTrusted(targetServer)
	if err != nil {
		log.Printf("DeliverFederatedNotification: cannot reach %s: %v", targetServer, err)
		return
	}

	// 3. ENVELOPE ASSEMBLY
	payload := FederatedNotificationRequest{
		RecipientID:    recipientID,
		ActorID:        actorID,
		Type:           typeStr,
		EntityID:       entityID,
		ActivityStream: json.RawMessage(as2Bytes),
		ServerID:       getServerID(), // The unique ID of this local instance
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	// 4. SERIALIZATION
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("DeliverFederatedNotification: marshal error: %v", err)
		return
	}

	// 5. HTTP TRANSMISSION
	// 10-second timeout ensures our local event loop isn't blocked by slow remotes.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/api/notification/federated", remoteServer.Endpoint),
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		log.Printf("DeliverFederatedNotification: delivery to %s failed: %v", targetServer, err)
		return
	}
	defer resp.Body.Close()

	// 6. RESPONSE VALIDATION
	if resp.StatusCode != http.StatusOK {
		log.Printf("DeliverFederatedNotification: remote %s returned %d", targetServer, resp.StatusCode)
		return
	}

	log.Printf("✅ Federated notification delivered to %s on %s", recipientID, targetServer)
}

// HandleIncomingFederatedNotification is the "Inbound Gateway". It processes
// notifications received from other servers and commits them to the local DB.
func HandleIncomingFederatedNotification(w http.ResponseWriter, r *http.Request) {
	// 1. HTTP METHOD ENFORCEMENT
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 2. JSON DECODING
	var req FederatedNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 3. SCHEMA VALIDATION
	if req.RecipientID == "" || req.ActorID == "" || req.Type == "" || req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// 4. SECURITY: ORIGIN VERIFICATION
	// Cross-reference the sending server against our trust store.
	_, _, err := EnsureServerTrusted(req.ServerID)
	if err != nil {
		log.Printf("HandleIncomingFederatedNotification: untrusted server %s: %v", req.ServerID, err)
		RespondWithError(w, http.StatusForbidden, "server not trusted")
		return
	}

	// 5. SECURITY: DESTINATION VERIFICATION
	// Ensure the message is actually intended for a user on THIS server.
	recipientParts := strings.Split(req.RecipientID, "@")
	if len(recipientParts) != 2 {
		RespondWithError(w, http.StatusBadRequest, "invalid recipient_id format")
		return
	}

	localServerID := getServerID()
	if recipientParts[1] != localServerID {
		RespondWithError(w, http.StatusBadRequest, "recipient is not on this server")
		return
	}

	// 6. IDENTITY VALIDATION
	// Verify the recipient actually exists in our local database.
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)`, req.RecipientID).Scan(&exists); err != nil || !exists {
		RespondWithError(w, http.StatusNotFound, "recipient not found on this server")
		return
	}

	// 7. DATABASE PERSISTENCE
	// We store the notification and the raw ActivityStream payload.
	// The ActivityStream payload allows our UI to render complex objects
	// that originated on other platforms (like custom emojis or attachments).
	var dbErr error
	if len(req.ActivityStream) > 0 {
		_, dbErr = db.Exec(`
            INSERT INTO notifications (recipient_id, actor_id, type, entity_id, activity_stream, created_at)
            VALUES ($1, $2, $3, $4, $5, NOW())
        `, req.RecipientID, req.ActorID, req.Type, req.EntityID, []byte(req.ActivityStream))
	} else {
		// Fallback for notifications without a full AS2 envelope.
		_, dbErr = db.Exec(`
            INSERT INTO notifications (recipient_id, actor_id, type, entity_id, created_at)
            VALUES ($1, $2, $3, $4, NOW())
        `, req.RecipientID, req.ActorID, req.Type, req.EntityID)
	}

	if dbErr != nil {
		log.Printf("HandleIncomingFederatedNotification: db error: %v", dbErr)
		RespondWithError(w, http.StatusInternalServerError, "failed to store notification")
		return
	}

	log.Printf("✅ Stored federated notification for %s from %s (type: %s)", req.RecipientID, req.ActorID, req.Type)

	// 8. ACKNOWLEDGMENT
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "notification_stored"})
}
