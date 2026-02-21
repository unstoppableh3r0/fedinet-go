package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// FederatedNotificationRequest is the payload posted to /api/notification/federated
type FederatedNotificationRequest struct {
	RecipientID    string          `json:"recipient_id"`
	ActorID        string          `json:"actor_id"`
	Type           string          `json:"type"`
	EntityID       string          `json:"entity_id"`
	ActivityStream json.RawMessage `json:"activity_stream,omitempty"`
	ServerID       string          `json:"server_id"`
	Timestamp      string          `json:"timestamp"`
}

// DeliverFederatedNotification sends an ActivityStreams notification to the recipient's home server.
// Called when the recipient is on a different server than the actor.
func DeliverFederatedNotification(recipientID, actorID, typeStr, entityID string, as2Bytes []byte) {
	parts := strings.Split(recipientID, "@")
	if len(parts) != 2 {
		log.Printf("DeliverFederatedNotification: invalid recipient ID %s", recipientID)
		return
	}
	targetServer := parts[1]

	// Ensure the remote server is trusted (auto-handshake if needed)
	remoteServer, _, err := EnsureServerTrusted(targetServer)
	if err != nil {
		log.Printf("DeliverFederatedNotification: cannot reach %s: %v", targetServer, err)
		return
	}

	payload := FederatedNotificationRequest{
		RecipientID:    recipientID,
		ActorID:        actorID,
		Type:           typeStr,
		EntityID:       entityID,
		ActivityStream: json.RawMessage(as2Bytes),
		ServerID:       getServerID(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("DeliverFederatedNotification: marshal error: %v", err)
		return
	}

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

	if resp.StatusCode != http.StatusOK {
		log.Printf("DeliverFederatedNotification: remote %s returned %d", targetServer, resp.StatusCode)
		return
	}

	log.Printf("✅ Federated notification delivered to %s on %s", recipientID, targetServer)
}

// HandleIncomingFederatedNotification receives a notification from a remote server and stores it locally.
func HandleIncomingFederatedNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req FederatedNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RecipientID == "" || req.ActorID == "" || req.Type == "" || req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// Verify sender is a trusted server (auto-handshake if not yet)
	_, _, err := EnsureServerTrusted(req.ServerID)
	if err != nil {
		log.Printf("HandleIncomingFederatedNotification: untrusted server %s: %v", req.ServerID, err)
		RespondWithError(w, http.StatusForbidden, "server not trusted")
		return
	}

	// Verify the recipient actually lives on this server
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

	// Verify recipient exists locally
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)`, req.RecipientID).Scan(&exists); err != nil || !exists {
		RespondWithError(w, http.StatusNotFound, "recipient not found on this server")
		return
	}

	// Store the notification with its ActivityStream payload
	var dbErr error
	if len(req.ActivityStream) > 0 {
		_, dbErr = db.Exec(`
			INSERT INTO notifications (recipient_id, actor_id, type, entity_id, activity_stream, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`, req.RecipientID, req.ActorID, req.Type, req.EntityID, []byte(req.ActivityStream))
	} else {
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
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "notification_stored"})
}
