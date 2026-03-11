package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// FederatedMessageRequest represents a message sent to a remote server
type FederatedMessageRequest struct {
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
	Signature  string `json:"signature"`
	ServerID   string `json:"server_id"`
}

// DeliverFederatedMessage sends a message to a user on a remote server
func DeliverFederatedMessage(fromUserID, toUserID, content string) error {
	// Parse recipient to get server name
	parts := strings.Split(toUserID, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid federated user ID format: %s", toUserID)
	}

	serverName := parts[1]

	// Ensure the server is trusted — auto-handshake via FEDERATION_PEERS if not yet
	remoteServer, _, err := EnsureServerTrusted(serverName)
	if err != nil {
		return fmt.Errorf("could not reach server '%s': %v", serverName, err)
	}

	// Prepare message payload
	timestamp := time.Now().UTC().Format(time.RFC3339)
	currentServerID := getServerID()

	// Create signature payload
	signaturePayload := fmt.Sprintf("%s|%s|%s|%s", fromUserID, toUserID, content, timestamp)
	signature, err := SignMessage(signaturePayload)
	if err != nil {
		return fmt.Errorf("failed to sign message: %v", err)
	}

	// Prepare request
	messageReq := FederatedMessageRequest{
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Content:    content,
		Timestamp:  timestamp,
		Signature:  signature,
		ServerID:   currentServerID,
	}

	reqJSON, err := json.Marshal(messageReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	// Send to remote server
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/api/message/federated", remoteServer.Endpoint),
		"application/json",
		bytes.NewBuffer(reqJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to deliver message to %s: %v", serverName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote server returned status %d", resp.StatusCode)
	}

	log.Printf("✅ Federated message delivered to %s on %s", toUserID, serverName)
	return nil
}

// HandleIncomingFederatedMessage handles messages received from remote servers
func HandleIncomingFederatedMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req FederatedMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate required fields (Signature is optional — trust is handled at server level)
	if req.FromUserID == "" || req.ToUserID == "" || req.Content == "" || req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// Extract sender's server from FromUserID
	parts := strings.Split(req.FromUserID, "@")
	if len(parts) != 2 {
		RespondWithError(w, http.StatusBadRequest, "invalid from_user_id format")
		return
	}
	senderServer := parts[1]

	// Verify sender's server matches the ServerID in request
	if senderServer != req.ServerID {
		RespondWithError(w, http.StatusBadRequest, "from_user_id does not match server_id")
		return
	}

	// Server-level trust check: the mutual handshake already established identity.
	// Per-message signatures are not required; EnsureServerTrusted auto-handshakes
	// unknown peers via FEDERATION_PEERS so first contact also works.
	_, _, err := EnsureServerTrusted(senderServer)
	if err != nil {
		log.Printf("Received message from untrusted server %s: %v", senderServer, err)
		RespondWithError(w, http.StatusForbidden, "server not trusted")
		return
	}

	// Check if recipient exists on this server
	recipientParts := strings.Split(req.ToUserID, "@")
	recipientUsername := recipientParts[0]

	// Verify recipient is on this server
	var recipientExists bool
	err = db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM identities 
			WHERE user_id LIKE $1
		)
	`, recipientUsername+"@%").Scan(&recipientExists)

	if err != nil || !recipientExists {
		RespondWithError(w, http.StatusNotFound, "recipient not found on this server")
		return
	}

	// Store the message
	err = StoreIncomingFederatedMessage(req.FromUserID, req.ToUserID, req.Content, req.ServerID)
	if err != nil {
		log.Printf("Failed to store federated message: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store message")
		return
	}

	log.Printf("✅ Received federated message from %s to %s (origin: %s)",
		req.FromUserID, req.ToUserID, req.ServerID)

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"status": "message_delivered",
	})
}

// StoreIncomingFederatedMessage stores a message received from a remote server.
// fromUserID is the external sender ID (e.g. alice@server_a) — stored as-is since it is cross-server.
// toUserID is the external recipient ID (e.g. bob@server_b) — resolved to internal ID via DB lookup.
func StoreIncomingFederatedMessage(fromUserID, toUserID, content, originServer string) error {
	// Resolve recipient internal user_id: the recipient is on THIS server.
	// Their user_id in the DB is "username@<InternalServerName>".
	// toUserID arrives as "username@server_b" (external). ToInternalID handles the conversion.
	internalTo := ToInternalID(toUserID)

	// Sender is on a REMOTE server. Keep the ID as-is (e.g. "alice@server_a").
	// This way GetConversationMessages can match using the exact sender string.
	_, err := db.Exec(`
		INSERT INTO messages (sender_id, recipient_id, content, created_at, is_federated, origin_server)
		VALUES ($1, $2, $3, NOW(), TRUE, $4)
	`, fromUserID, internalTo, content, originServer)

	return err
}

// StoreSentFederatedMessage stores a copy of the sent message for the sender's history
func StoreSentFederatedMessage(fromUserID, toUserID, content string) error {
	// Extract recipient server
	parts := strings.Split(toUserID, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid recipient format")
	}
	recipientServer := parts[1]

	_, err := db.Exec(`
		INSERT INTO messages (sender_id, recipient_id, content, created_at, is_federated, origin_server)
		VALUES ($1, $2, $3, NOW(), TRUE, $4)
	`, fromUserID, toUserID, content, recipientServer)

	return err
}

// SignMessage signs a message payload with the server's private key
func SignMessage(payload string) (string, error) {
	privKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	if privKeyHex == "" {
		return "", fmt.Errorf("SERVER_IDENTITY_PRIVATE_KEY not configured")
	}

	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key format: %v", err)
	}

	privateKey := ed25519.PrivateKey(privKeyBytes)
	signature := ed25519.Sign(privateKey, []byte(payload))

	return hex.EncodeToString(signature), nil
}

// VerifyMessageSignature verifies a message signature using the sender server's public key
func VerifyMessageSignature(payload, signatureHex, publicKeyHex string) bool {
	// Decode public key
	publicKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		log.Printf("Failed to decode public key: %v", err)
		return false
	}

	// Decode signature
	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		log.Printf("Failed to decode signature: %v", err)
		return false
	}

	publicKey := ed25519.PublicKey(publicKeyBytes)
	return ed25519.Verify(publicKey, []byte(payload), signatureBytes)
}

// IsFederatedUser checks if a user ID represents a user on a different federated server.
// Uses case-insensitive comparison to handle external-format IDs correctly.
func IsFederatedUser(userID string) (bool, string) {
	parts := strings.Split(strings.ToLower(userID), "@")
	if len(parts) != 2 {
		return false, ""
	}

	serverSuffix := parts[1]
	localServerName := strings.ToLower(getCurrentServerName())

	// Same server (internal format) — local user
	if serverSuffix == localServerName {
		return false, ""
	}

	// Check the public-facing server name from DB config if available
	// (handles edge case where frontend sends user@PublicName vs user@server_a)
	config, configErr := GetServerConfig()
	if configErr == nil && config.ServerName != "" {
		if serverSuffix == strings.ToLower(config.ServerName) {
			return false, ""
		}
	}

	return true, serverSuffix
}

// getCurrentServerName gets the current server's name from SERVER_ID env
func getCurrentServerName() string {
	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server-a"
	}
	return serverID
}
