package identity

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// HandshakeRequest represents the initial handshake request
type HandshakeRequest struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Endpoint   string `json:"endpoint"`
	PublicKey  string `json:"public_key,omitempty"` // Optional in step 1, required in acknowledgment
}

// HandshakeResponse represents the handshake response
type HandshakeResponse struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	PublicKey  string `json:"public_key"`
	Status     string `json:"status"`
}

// InitiateHandshake sends a trust request to a remote server
func InitiateHandshake(serverEndpoint, serverName string) (*HandshakeResponse, error) {
	// Get current server's identity
	currentServerID := getServerID()
	currentServerName := getServerName()
	currentEndpoint := getServerEndpoint()

	// Step 1: Send handshake request without our public key
	reqBody := HandshakeRequest{
		ServerID:   currentServerID,
		ServerName: currentServerName,
		Endpoint:   currentEndpoint,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Send request to remote server
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/api/handshake", serverEndpoint),
		"application/json",
		bytes.NewBuffer(reqJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// Parse response
	var handshakeResp HandshakeResponse
	if err := json.NewDecoder(resp.Body).Decode(&handshakeResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	// Step 2: Send acknowledgment with our public key
	publicKeyHex := getServerPublicKey()
	ackBody := HandshakeRequest{
		ServerID:   currentServerID,
		ServerName: currentServerName,
		Endpoint:   currentEndpoint,
		PublicKey:  publicKeyHex,
	}

	ackJSON, err := json.Marshal(ackBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ack: %v", err)
	}

	ackResp, err := client.Post(
		fmt.Sprintf("%s/api/handshake/ack", serverEndpoint),
		"application/json",
		bytes.NewBuffer(ackJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send acknowledgment: %v", err)
	}
	defer ackResp.Body.Close()

	if ackResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acknowledgment failed with status %d", ackResp.StatusCode)
	}

	log.Printf("Handshake completed with %s", serverName)
	return &handshakeResp, nil
}

// HandleHandshakeRequest handles incoming handshake requests (Step 1)
func HandleHandshakeRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HandshakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	// Validate required fields
	if req.ServerID == "" || req.ServerName == "" || req.Endpoint == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing required fields",
		})
		return
	}

	log.Printf("Received handshake request from %s (%s)", req.ServerName, req.ServerID)

	// Get our server's public key
	publicKeyHex := getServerPublicKey()
	serverID := getServerID()
	serverName := getServerName()

	// Respond with our server info (but don't store their info yet)
	response := HandshakeResponse{
		ServerID:   serverID,
		ServerName: serverName,
		PublicKey:  publicKeyHex,
		Status:     "awaiting_acknowledgment",
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// HandleHandshakeAcknowledgment handles the acknowledgment with public key (Step 3)
func HandleHandshakeAcknowledgment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HandshakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	// Validate all required fields including public key
	if req.ServerID == "" || req.ServerName == "" || req.Endpoint == "" || req.PublicKey == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing required fields",
		})
		return
	}

	// Validate public key format
	if _, err := hex.DecodeString(req.PublicKey); err != nil || len(req.PublicKey) != 64 {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid public key format",
		})
		return
	}

	log.Printf("Received handshake acknowledgment from %s with public key", req.ServerName)

	// Store the remote server in trusted_servers
	err := storeTrustedServer(req.ServerID, req.ServerName, req.PublicKey, req.Endpoint)
	if err != nil {
		log.Printf("Failed to store trusted server: %v", err)
		RespondWithJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to store server",
		})
		return
	}

	log.Printf("Successfully added %s to trusted servers", req.ServerName)

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"status": "handshake_complete",
	})
}

// storeTrustedServer stores a trusted server in the database
func storeTrustedServer(serverID, serverName, publicKey, endpoint string) error {
	// Check if server already exists
	var existingID string
	err := db.QueryRow(`
		SELECT id FROM trusted_servers WHERE server_id = $1
	`, serverID).Scan(&existingID)

	if err == sql.ErrNoRows {
		// Insert new server
		_, err = db.Exec(`
			INSERT INTO trusted_servers (server_id, server_name, public_key, endpoint, trusted_at)
			VALUES ($1, $2, $3, $4, $5)
		`, serverID, serverName, publicKey, endpoint, time.Now())

		if err != nil {
			return fmt.Errorf("failed to insert server: %v", err)
		}
		log.Printf("Inserted new trusted server: %s", serverID)
	} else if err == nil {
		// Update existing server
		_, err = db.Exec(`
			UPDATE trusted_servers 
			SET server_name = $1, public_key = $2, endpoint = $3, trusted_at = $4
			WHERE server_id = $5
		`, serverName, publicKey, endpoint, time.Now(), serverID)

		if err != nil {
			return fmt.Errorf("failed to update server: %v", err)
		}
		log.Printf("Updated existing trusted server: %s", serverID)
	} else {
		return fmt.Errorf("database error: %v", err)
	}

	return nil
}

// Helper functions to get server identity
func getServerID() string {
	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server-a"
	}
	return serverID
}

func getServerName() string {
	// Could come from database or environment
	serverID := getServerID()
	// Format: "server-a" -> "server-a"
	return serverID
}

func getServerEndpoint() string {
	// Prefer SERVER_ENDPOINT (Docker-internal) for inter-server handshakes;
	// fall back to SERVER_URL (external) then localhost.
	endpoint := os.Getenv("SERVER_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("SERVER_URL")
	}
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}
	return endpoint
}

func getServerPublicKey() string {
	// Get public key from the identity private key
	privKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	if privKeyHex == "" {
		log.Fatal("SERVER_IDENTITY_PRIVATE_KEY not set")
	}

	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		log.Fatal("Invalid SERVER_IDENTITY_PRIVATE_KEY format")
	}

	privateKey := ed25519.PrivateKey(privKeyBytes)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return hex.EncodeToString(publicKey)
}
