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
	"net/url"
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

	// serverName here is the server_id suffix from user IDs (e.g. "server_b"), not the display name
	err := db.QueryRow(`
		SELECT id, server_id, server_name, public_key, endpoint
		FROM trusted_servers
		WHERE server_id = $1
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

// GetPeerEndpoint looks up a server's endpoint from the FEDERATION_PEERS env var.
// Format: "server_b=http://server_b:8082,server_c=http://server_c:8082"
func GetPeerEndpoint(serverID string) (string, bool) {
	peers := os.Getenv("FEDERATION_PEERS")
	if peers == "" {
		return "", false
	}
	for _, entry := range strings.Split(peers, ",") {
		entry = strings.TrimSpace(entry)
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == serverID {
			return strings.TrimSpace(kv[1]), true
		}
	}
	return "", false
}

// CheckServerHealth returns true if the remote server's /health endpoint is reachable and returns 200.
func CheckServerHealth(endpoint string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint + "/health")
	if err != nil {
		log.Printf("Health check failed for %s: %v", endpoint, err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureServerTrusted ensures the given serverID is in trusted_servers.
// If it isn't, it tries to discover the endpoint from FEDERATION_PEERS,
// health-checks it, and auto-initiates a handshake to add it.
// Returns (trustedServer, discoveryStatus, error).
// discoveryStatus is one of: "already_trusted", "auto_handshake", "not_found", "unhealthy".
func EnsureServerTrusted(serverID string) (*TrustedServer, string, error) {
	// 1. Already trusted?
	server, err := GetTrustedServer(serverID)
	if err == nil {
		return server, "already_trusted", nil
	}

	// 2. Look up endpoint from FEDERATION_PEERS
	endpoint, found := GetPeerEndpoint(serverID)
	if !found {
		return nil, "not_found", fmt.Errorf("server '%s' is not trusted and not in FEDERATION_PEERS", serverID)
	}

	// 3. Health check
	log.Printf("Auto-discovery: health-checking %s at %s", serverID, endpoint)
	if !CheckServerHealth(endpoint) {
		return nil, "unhealthy", fmt.Errorf("server '%s' at %s is not reachable or unhealthy", serverID, endpoint)
	}

	// 4. Auto-handshake: fetch remote server info (server_name + public_key)
	log.Printf("Auto-discovery: initiating handshake with %s", serverID)
	handshakeResp, err := InitiateHandshake(endpoint, serverID)
	if err != nil {
		return nil, "unhealthy", fmt.Errorf("handshake with '%s' failed: %v", serverID, err)
	}

	// The handshake already stored the server (via HandleHandshakeAcknowledgment on remote).
	// Our side is stored inside AddTrustedServerHandler, but here we called InitiateHandshake directly
	// which doesn't persist — so store it now.
	displayName := handshakeResp.ServerName
	if displayName == "" {
		displayName = serverID
	}
	storeErr := storeTrustedServer(serverID, displayName, handshakeResp.PublicKey, endpoint)
	if storeErr != nil {
		log.Printf("Warning: could not store auto-discovered server %s: %v", serverID, storeErr)
		// Non-fatal: continue with an in-memory struct so this search still succeeds
	}

	// 5. Re-fetch from DB (or build from what we have)
	server, err = GetTrustedServer(serverID)
	if err != nil {
		// Build from handshake response (DB write may have been nil-op if already existed in race)
		server = &TrustedServer{
			ServerID:   serverID,
			ServerName: displayName,
			PublicKey:  handshakeResp.PublicKey,
			Endpoint:   endpoint,
		}
	}

	log.Printf("✅ Auto-discovery complete: %s (%s) is now trusted", serverID, endpoint)
	return server, "auto_handshake", nil
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

// SearchFederatedUser queries a remote server for user information.
// Returns (profile, discoveryStatus, error).
func SearchFederatedUser(username, serverName string) (*FederatedUserProfile, string, error) {
	// Ensure the server is trusted (auto-handshake if needed)
	server, discoveryStatus, err := EnsureServerTrusted(serverName)
	if err != nil {
		return nil, discoveryStatus, err
	}

	// Create request to remote server's public user API
	path := fmt.Sprintf("/api/users/%s", username)
	fullURL := fmt.Sprintf("%s%s", server.Endpoint, path)

	// Create timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Sign the request
	signature, err := SignFederatedRequest("GET", path, timestamp)
	if err != nil {
		return nil, discoveryStatus, fmt.Errorf("failed to sign request: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, discoveryStatus, fmt.Errorf("failed to create request: %v", err)
	}

	// Add federation headers
	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server_a"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Server-ID", serverID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)

	// Make the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, discoveryStatus, fmt.Errorf("failed to contact remote server: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, discoveryStatus, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, discoveryStatus, fmt.Errorf("remote server returned error: %s", string(body))
	}

	// Parse response
	var profile FederatedUserProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, discoveryStatus, fmt.Errorf("failed to parse response: %v", err)
	}

	return &profile, discoveryStatus, nil
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

	// Perform federated search — auto-discovers and handshakes if needed
	log.Printf("Federated search: username=%s, server=%s", identity.Username, identity.ServerName)

	profile, discoveryStatus, err := SearchFederatedUser(identity.Username, identity.ServerName)
	if err != nil {
		log.Printf("Federated search error (status=%s): %v", discoveryStatus, err)
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"found":            false,
			"federated":        true,
			"discovery_status": discoveryStatus,
			"error":            err.Error(),
		})
		return
	}

	// Return the federated user profile
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"found":            true,
		"federated":        true,
		"discovery_status": discoveryStatus,
		"user":             profile,
	})
}

// GetPublicUserHandler returns public user information for federation
// This endpoint is called by other servers to lookup users
func GetPublicUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check federation headers — they are optional for public profile reads.
	// If present we log the requester; full signature verification can be added later.
	serverID := r.Header.Get("X-Server-ID")
	if serverID != "" {
		log.Printf("Public user API: requested by federated server '%s'", serverID)
	}

	// Get username from URL path
	path := r.URL.Path
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) < 4 {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid user path",
		})
		return
	}

	// Dispatch to posts sub-handler: /api/users/{username}/posts
	if parts[len(parts)-1] == "posts" {
		GetPublicUserPostsHandler(w, r)
		return
	}

	username := parts[len(parts)-1]

	// TODO: Verify signature using trusted_servers public key

	log.Printf("Public user API: Looking for username '%s'", username)

	// Query local database for user
	// user_id format is "username@server", so we need to match the username part
	var userID, displayName, avatarURL, bio, homeServer string
	err := db.QueryRow(`
		SELECT i.user_id,
		       COALESCE(p.display_name, ''),
		       COALESCE(p.avatar_url, ''),
		       COALESCE(p.bio, ''),
		       COALESCE(i.home_server, '')
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

// GetPublicUserPostsHandler returns the recent public posts for a federated user ID.
// URL: GET /api/users/{username}/posts
func GetPublicUserPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	// path: /api/users/{username}/posts  → parts: ["","api","users","{username}","posts"]
	if len(parts) < 5 || parts[len(parts)-1] != "posts" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	username := parts[len(parts)-2]

	viewerID := r.URL.Query().Get("viewer_id")

	// Resolve full internal user_id
	var internalUserID string
	err := db.QueryRow(
		`SELECT i.user_id FROM identities i
		 WHERE i.user_id LIKE $1 OR SPLIT_PART(i.user_id,'@',1)=$2
		 LIMIT 1`,
		username+"@%", username,
	).Scan(&internalUserID)
	if err == sql.ErrNoRows {
		RespondWithJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		RespondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	internalViewer := ""
	if viewerID != "" {
		internalViewer = ToInternalID(viewerID)
	}

	posts, err := GetUserPosts(internalUserID, internalViewer, 20, 0)
	if err != nil {
		RespondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch posts"})
		return
	}
	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"posts": posts})
}

// FederatedUserPostsHandler proxies a posts request to a remote server.
// URL: GET /api/posts/federated?user_id=alice@server_b&viewer_id=bob@server_a
func FederatedUserPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}

	parts := strings.Split(userID, "@")
	if len(parts) != 2 {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id must be username@server"})
		return
	}
	username, serverID := parts[0], parts[1]

	// Check if it's actually a local user — if so serve directly.
	if serverID == getServerID() {
		r.URL.Path = "/api/users/" + username + "/posts"
		GetPublicUserPostsHandler(w, r)
		return
	}

	// Ensure remote server is trusted.
	remote, _, err := EnsureServerTrusted(serverID)
	if err != nil {
		RespondWithJSON(w, http.StatusBadGateway, map[string]string{"error": "cannot reach server: " + err.Error()})
		return
	}

	viewerID := r.URL.Query().Get("viewer_id")
	remoteURL := fmt.Sprintf("%s/api/users/%s/posts", remote.Endpoint, username)
	if viewerID != "" {
		remoteURL += "?viewer_id=" + url.QueryEscape(viewerID)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(remoteURL)
	if err != nil {
		RespondWithJSON(w, http.StatusBadGateway, map[string]string{"error": "remote request failed"})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		RespondWithJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid response from remote"})
		return
	}
	RespondWithJSON(w, resp.StatusCode, result)
}
