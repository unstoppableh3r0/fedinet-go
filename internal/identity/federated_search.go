package main

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// FederatedIdentity represents a parsed federated user identifier
type FederatedIdentity struct {
	Username    string
	ServerName  string
	IsFederated bool
}

// ParseFederatedIdentity parses a query string and extracts username and server
// Supports formats: "username", "username@server", "username@server-a"
func ParseFederatedIdentity(query string) FederatedIdentity {
	query = strings.TrimSpace(query)

	if !strings.Contains(query, "@") {
		return FederatedIdentity{
			Username:    query,
			ServerName:  "",
			IsFederated: false,
		}
	}

	parts := strings.Split(query, "@")
	if len(parts) != 2 {
		return FederatedIdentity{
			Username:    query,
			ServerName:  "",
			IsFederated: false,
		}
	}

	return FederatedIdentity{
		Username:    parts[0],
		ServerName:  parts[1],
		IsFederated: true,
	}
}

// GetTrustedServer retrieves server configuration from trusted_servers table
func GetTrustedServer(serverName string) (*TrustedServer, error) {
	var server TrustedServer

	err := db.QueryRow(`
		SELECT id, server_id, server_name, public_key, endpoint
		FROM trusted_servers
		WHERE server_name = $1
	`, serverName).Scan(
		&server.ID,
		&server.ServerID,
		&server.ServerName,
		&server.PublicKey,
		&server.Endpoint,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("server '%s' is not in trusted servers list", serverName)
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}

	return &server, nil
}

// SignFederatedRequest signs a request with the server's private key
func SignFederatedRequest(method, path, timestamp string) (string, error) {
	// Get server's private key from environment or database
	serverPrivKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	if serverPrivKeyHex == "" {
		return "", fmt.Errorf("server private key not configured")
	}

	privKeyBytes, err := hex.DecodeString(serverPrivKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key format: %v", err)
	}

	// Create signature payload: METHOD|PATH|TIMESTAMP
	payload := fmt.Sprintf("%s|%s|%s", method, path, timestamp)

	// Sign with Ed25519
	signature := ed25519.Sign(privKeyBytes, []byte(payload))

	return hex.EncodeToString(signature), nil
}

// FederatedUserProfile represents a user profile from remote server
type FederatedUserProfile struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Bio         string `json:"bio"`
	HomeServer  string `json:"home_server"`
}

// SearchFederatedUser queries a remote server for user information
func SearchFederatedUser(username, serverName string) (*FederatedUserProfile, error) {
	// Get trusted server configuration
	server, err := GetTrustedServer(serverName)
	if err != nil {
		return nil, err
	}

	// Create request to remote server's public user API
	path := fmt.Sprintf("/api/users/%s", username)
	fullURL := fmt.Sprintf("%s%s", server.Endpoint, path)

	// Create timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Sign the request
	signature, err := SignFederatedRequest("GET", path, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add federation headers
	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server-a"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Server-ID", serverID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)

	// Make the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to contact remote server: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote server returned error: %s", string(body))
	}

	// Parse response
	var profile FederatedUserProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &profile, nil
}

// FederatedSearchHandler handles federated user search requests
func FederatedSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get search query
	query := r.URL.Query().Get("q")
	if query == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "search query 'q' is required",
		})
		return
	}

	// Parse the identity
	identity := ParseFederatedIdentity(query)

	// If not federated, use existing local search
	if !identity.IsFederated {
		// Forward to existing UserSearchHandler logic
		// For now, just return not found
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"found":     false,
			"federated": false,
			"message":   "Use local search endpoint for non-federated queries",
		})
		return
	}

	// Perform federated search
	log.Printf("Federated search: username=%s, server=%s", identity.Username, identity.ServerName)

	profile, err := SearchFederatedUser(identity.Username, identity.ServerName)
	if err != nil {
		log.Printf("Federated search error: %v", err)
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"found":     false,
			"federated": true,
			"error":     err.Error(),
		})
		return
	}

	// Return the federated user profile
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"found":     true,
		"federated": true,
		"user":      profile,
	})
}

// GetPublicUserHandler returns public user information for federation
// This endpoint is called by other servers to lookup users
func GetPublicUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify federated request signature
	serverID := r.Header.Get("X-Server-ID")
	timestamp := r.Header.Get("X-Timestamp")
	signature := r.Header.Get("X-Signature")

	if serverID == "" || timestamp == "" || signature == "" {
		RespondWithJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "missing federation headers",
		})
		return
	}

	// Get username from URL path
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid user path",
		})
		return
	}
	username := parts[len(parts)-1]

	// TODO: Verify signature using trusted_servers public key

	log.Printf("Public user API: Looking for username '%s'", username)

	// Query local database for user
	// user_id format is "username@server", so we need to match the username part
	var userID, displayName, avatarURL, bio, homeServer string
	err := db.QueryRow(`
		SELECT i.user_id, p.display_name, p.avatar_url, p.bio, i.home_server
		FROM identities i
		JOIN profiles p ON i.user_id = p.user_id
		WHERE i.user_id LIKE $1 OR SPLIT_PART(i.user_id, '@', 1) = $2
	`, username+"@%", username).Scan(&userID, &displayName, &avatarURL, &bio, &homeServer)

	if err == sql.ErrNoRows {
		log.Printf("User '%s' not found in database", username)
		RespondWithJSON(w, http.StatusNotFound, map[string]string{
			"error": "user not found",
		})
		return
	}
	if err != nil {
		log.Printf("Database error: %v", err)
		RespondWithJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal server error",
		})
		return
	}

	// Return public profile
	RespondWithJSON(w, http.StatusOK, FederatedUserProfile{
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
		Bio:         bio,
		HomeServer:  homeServer,
	})
}
