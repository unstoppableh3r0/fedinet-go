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

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// FederatedMessageRequest is the standard envelope for cross-server communication.
// It encapsulates the message content along with metadata required for verification
// and routing. This struct is marshaled to JSON and sent over HTTP POST.
type FederatedMessageRequest struct {
	FromUserID string `json:"from_user_id"` // The sender's full ID (e.g., alice@server_a)
	ToUserID   string `json:"to_user_id"`   // The recipient's full ID (e.g., bob@server_b)
	Content    string `json:"content"`      // The actual message text or encrypted payload
	Timestamp  string `json:"timestamp"`    // ISO-8601 formatted UTC time to prevent replay attacks
	Signature  string `json:"signature"`    // Cryptographic proof that this message originated from the sender server
	ServerID   string `json:"server_id"`    // The unique identifier of the originating server
}

// ============================================================================
// OUTBOUND MESSAGING LOGIC
// ============================================================================

// DeliverFederatedMessage handles the complexity of sending a message to a foreign server.
// It involves server discovery, identity verification (trust establishment),
// cryptographic signing of the payload, and reliable network delivery.
func DeliverFederatedMessage(fromUserID, toUserID, content string) error {
	// 1. DISCOVERY PHASE:
	// We extract the server portion of the recipient's user ID.
	// Example: In "bob@server_b", "server_b" is the target hostname or ID.
	parts := strings.Split(toUserID, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid federated user ID format: %s", toUserID)
	}

	serverName := parts[1]

	// 2. TRUST PHASE:
	// EnsureServerTrusted checks our local database for the remote server's public key.
	// If the server is unknown, it attempts an automatic handshake (mutual exchange of keys).
	// This ensures that we don't send data to unauthorized or unverified endpoints.
	remoteServer, _, err := EnsureServerTrusted(serverName)
	if err != nil {
		return fmt.Errorf("could not reach server '%s': %v", serverName, err)
	}

	// 3. PREPARATION PHASE:
	// Use UTC for the timestamp to ensure synchronization across different time zones.
	timestamp := time.Now().UTC().Format(time.RFC3339)
	currentServerID := getServerID()

	// 4. SECURITY PHASE (SIGNING):
	// We construct a canonical string representation of the message to sign.
	// This "Signature Payload" ensures that if the content, sender, or recipient
	// is changed in transit, the signature will become invalid.
	signaturePayload := fmt.Sprintf("%s|%s|%s|%s", fromUserID, toUserID, content, timestamp)
	signature, err := SignMessage(signaturePayload)
	if err != nil {
		return fmt.Errorf("failed to sign message: %v", err)
	}

	// 5. PACKAGING PHASE:
	// Populate the request struct with all calculated data.
	messageReq := FederatedMessageRequest{
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Content:    content,
		Timestamp:  timestamp,
		Signature:  signature,
		ServerID:   currentServerID,
	}

	// Transform the Go struct into a JSON byte array for transmission.
	reqJSON, err := json.Marshal(messageReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	// 6. TRANSMISSION PHASE:
	// We use a 10-second timeout to prevent the application from hanging if
	// the remote server is unresponsive or the network is slow.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/api/message/federated", remoteServer.Endpoint),
		"application/json",
		bytes.NewBuffer(reqJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to deliver message to %s: %v", serverName, err)
	}
	defer resp.Body.Close() // Crucial: Always close response bodies to prevent socket leaks.

	// 7. VERIFICATION PHASE (HTTP LAYER):
	// A 200 OK status indicates the remote server accepted the message and verified the trust.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote server returned status %d", resp.StatusCode)
	}

	log.Printf("✅ Federated message delivered to %s on %s", toUserID, serverName)
	return nil
}

// ============================================================================
// INBOUND MESSAGING LOGIC (API HANDLER)
// ============================================================================

// HandleIncomingFederatedMessage is the entry point for other servers sending messages here.
// It acts as a gatekeeper, validating that the sender server is trusted and the recipient exists.
func HandleIncomingFederatedMessage(w http.ResponseWriter, r *http.Request) {
	// Standard security check: only POST requests are allowed for message delivery.
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Decode the JSON body into our struct.
	var req FederatedMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 1. DATA VALIDATION:
	// Ensure no mandatory fields are empty. This prevents malformed data from entering our DB.
	if req.FromUserID == "" || req.ToUserID == "" || req.Content == "" || req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// 2. ORIGIN VERIFICATION:
	// Extract the domain portion of the sender's ID.
	parts := strings.Split(req.FromUserID, "@")
	if len(parts) != 2 {
		RespondWithError(w, http.StatusBadRequest, "invalid from_user_id format")
		return
	}
	senderServer := parts[1]

	// 3. SECURITY CHECK (Server Identity):
	// Ensure the server claiming to send the message matches the user ID of the sender.
	// This prevents one server from attempting to "spoof" users from another server.
	if senderServer != req.ServerID {
		RespondWithError(w, http.StatusBadRequest, "from_user_id does not match server_id")
		return
	}

	// 4. TRUST VERIFICATION:
	// Check if this server is in our whitelist or has completed a handshake.
	// If the server is unknown, EnsureServerTrusted might attempt to verify it via peering configs.
	_, _, err := EnsureServerTrusted(senderServer)
	if err != nil {
		log.Printf("Received message from untrusted server %s: %v", senderServer, err)
		RespondWithError(w, http.StatusForbidden, "server not trusted")
		return
	}

	// 5. RECIPIENT VALIDATION:
	// We must ensure the user exists on THIS instance before accepting the message.
	recipientParts := strings.Split(req.ToUserID, "@")
	recipientUsername := recipientParts[0]

	// Perform a database lookup. Note the use of LIKE with a suffix to match
	// internal ID formats (e.g., username@internal_server_name).
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

	// 6. PERSISTENCE:
	// Once all checks pass, we commit the message to our permanent storage.
	err = StoreIncomingFederatedMessage(req.FromUserID, req.ToUserID, req.Content, req.ServerID)
	if err != nil {
		log.Printf("Failed to store federated message: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store message")
		return
	}

	log.Printf("✅ Received federated message from %s to %s (origin: %s)",
		req.FromUserID, req.ToUserID, req.ServerID)

	// Acknowledge receipt to the sender server.
	RespondWithJSON(w, http.StatusOK, map[string]string{
		"status": "message_delivered",
	})
}

// ============================================================================
// STORAGE & UTILITY LOGIC
// ============================================================================

// StoreIncomingFederatedMessage handles the insertion of a remote message into the local database.
// It maps external "bob@server_b" IDs into internal database IDs for consistent querying.
func StoreIncomingFederatedMessage(fromUserID, toUserID, content, originServer string) error {
	// 1. ID TRANSFORMATION:
	// Users are stored internally with a server suffix that might differ from their
	// public-facing DNS name. ToInternalID performs this conversion.
	internalTo := ToInternalID(toUserID)

	// 2. DATABASE INSERTION:
	// We flag the message as 'is_federated' so the UI can display server origin badges.
	// 'origin_server' is stored to track the physical source of the data for moderation/auditing.
	_, err := db.Exec(`
        INSERT INTO messages (sender_id, recipient_id, content, created_at, is_federated, origin_server)
        VALUES ($1, $2, $3, NOW(), TRUE, $4)
    `, fromUserID, internalTo, content, originServer)

	return err
}

// StoreSentFederatedMessage saves a local copy of a message sent by a local user to a remote user.
// This ensures that the sender can see their own message history.
func StoreSentFederatedMessage(fromUserID, toUserID, content string) error {
	// Extract recipient server for auditing purposes.
	parts := strings.Split(toUserID, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid recipient format")
	}
	recipientServer := parts[1]

	// We insert the record with is_federated = TRUE.
	// Note: recipient_id here remains the full external ID (e.g., bob@server_b).
	_, err := db.Exec(`
        INSERT INTO messages (sender_id, recipient_id, content, created_at, is_federated, origin_server)
        VALUES ($1, $2, $3, NOW(), TRUE, $4)
    `, fromUserID, toUserID, content, recipientServer)

	return err
}

// ============================================================================
// CRYPTOGRAPHY (Ed25519)
// ============================================================================

// SignMessage generates a cryptographic signature for a string payload.
// It uses Ed25519, which is faster and more secure than older RSA algorithms.
func SignMessage(payload string) (string, error) {
	// Retrieve the private key from the environment.
	// Private keys should NEVER be hardcoded or checked into version control.
	privKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	if privKeyHex == "" {
		return "", fmt.Errorf("SERVER_IDENTITY_PRIVATE_KEY not configured")
	}

	// The key is stored as a Hex string; convert it to raw bytes.
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key format: %v", err)
	}

	// Generate the signature based on the payload bytes.
	privateKey := ed25519.PrivateKey(privKeyBytes)
	signature := ed25519.Sign(privateKey, []byte(payload))

	// Return as Hex for easy transmission in JSON.
	return hex.EncodeToString(signature), nil
}

// VerifyMessageSignature checks if a signature matches the payload and public key.
// This allows us to prove that a message hasn't been tampered with and truly came from the owner of the key.
func VerifyMessageSignature(payload, signatureHex, publicKeyHex string) bool {
	// 1. DECODE PUBLIC KEY:
	publicKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		log.Printf("Failed to decode public key: %v", err)
		return false
	}

	// 2. DECODE SIGNATURE:
	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		log.Printf("Failed to decode signature: %v", err)
		return false
	}

	// 3. CRYPTOGRAPHIC VERIFICATION:
	publicKey := ed25519.PublicKey(publicKeyBytes)
	return ed25519.Verify(publicKey, []byte(payload), signatureBytes)
}

// ============================================================================
// FEDERATION UTILITIES
// ============================================================================

// IsFederatedUser determines if a given UserID belongs to this server or a remote one.
// This is used by the logic to decide whether to route a message internally or
// trigger the federation delivery system.
func IsFederatedUser(userID string) (bool, string) {
	// Normalize to lower case to prevent "User@Server" and "user@server" from being seen as different.
	parts := strings.Split(strings.ToLower(userID), "@")
	if len(parts) != 2 {
		return false, ""
	}

	serverSuffix := parts[1]
	localServerName := strings.ToLower(getCurrentServerName())

	// Check 1: Direct match with local SERVER_ID env variable.
	if serverSuffix == localServerName {
		return false, ""
	}

	// Check 2: Match with the public-facing 'ServerName' defined in the DB config.
	// This is important because the internal SERVER_ID might be "node-1"
	// while the public name is "social.example.com".
	config, configErr := GetServerConfig()
	if configErr == nil && config.ServerName != "" {
		if serverSuffix == strings.ToLower(config.ServerName) {
			return false, ""
		}
	}

	// If no local match is found, the user is considered "Federated" (remote).
	return true, serverSuffix
}

// getCurrentServerName is a helper to fetch the identity of the current node.
func getCurrentServerName() string {
	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		// Defaulting to "server-a" provides a fallback for local development environments.
		serverID = "server-a"
	}
	return serverID
}