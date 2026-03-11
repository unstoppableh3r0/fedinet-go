package identity

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

// ============================================================================
// DATA STRUCTURES & IDENTITY PARSING
// ============================================================================

// FederatedIdentity represents a parsed federated user identifier.
// It separates the local handle from the domain to facilitate routing.
type FederatedIdentity struct {
    Username    string // The local part (e.g., "alice")
    ServerName  string // The domain part (e.g., "server_b")
    IsFederated bool   // Flag indicating if the user is remote
}

// ParseFederatedIdentity parses a query string and extracts username and server.
// Supports formats: "username", "username@server", "username@server-a".
// This is the primary parser for search bars and mentions.
func ParseFederatedIdentity(query string) FederatedIdentity {
    query = strings.TrimSpace(query)

    // Case 1: Simple username without domain (Local context)
    if !strings.Contains(query, "@") {
        return FederatedIdentity{
            Username:    query,
            ServerName:  "",
            IsFederated: false,
        }
    }

    // Case 2: Federated format (username@server)
    parts := strings.Split(query, "@")
    if len(parts) != 2 {
        // Fallback for malformed strings like "@@" or "a@b@c"
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

// ============================================================================
// TRUST & DISCOVERY LOGIC
// ============================================================================

// GetTrustedServer retrieves server configuration from the trusted_servers table.
// This is the "Whitelist" check used for all inbound/outbound federated traffic.
func GetTrustedServer(serverName string) (*TrustedServer, error) {
    var server TrustedServer

    // serverName here is the server_id suffix from user IDs (e.g. "server_b"), not the display name.
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
// This allows static peer definitions without a database migration.
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

// CheckServerHealth returns true if the remote server's /health endpoint is reachable.
// Used as a pre-flight check before attempting a handshake or profile search.
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
// It implements a "Just-In-Time Discovery" pattern:
// 1. Check local trust DB.
// 2. If missing, check environment peers.
// 3. Health check the endpoint.
// 4. Perform an 'InitiateHandshake' (P2P Key Exchange).
// 5. Persist the newly discovered server.
func EnsureServerTrusted(serverID string) (*TrustedServer, string, error) {
    // 1. Check if trust relationship already exists
    server, err := GetTrustedServer(serverID)
    if err == nil {
        return server, "already_trusted", nil
    }

    // 2. Check for static configuration in environment variables
    endpoint, found := GetPeerEndpoint(serverID)
    if !found {
        return nil, "not_found", fmt.Errorf("server '%s' is not trusted and not in FEDERATION_PEERS", serverID)
    }

    // 3. Connectivity verification
    log.Printf("Auto-discovery: health-checking %s at %s", serverID, endpoint)
    if !CheckServerHealth(endpoint) {
        return nil, "unhealthy", fmt.Errorf("server '%s' at %s is not reachable or unhealthy", serverID, endpoint)
    }

    // 4. Cryptographic Handshake (Key Exchange)
    log.Printf("Auto-discovery: initiating handshake with %s", serverID)
    handshakeResp, err := InitiateHandshake(endpoint, serverID)
    if err != nil {
        return nil, "unhealthy", fmt.Errorf("handshake with '%s' failed: %v", serverID, err)
    }

    // 5. Persistence
    displayName := handshakeResp.ServerName
    if displayName == "" {
        displayName = serverID
    }
    storeErr := storeTrustedServer(serverID, displayName, handshakeResp.PublicKey, endpoint)
    if storeErr != nil {
        log.Printf("Warning: could not store auto-discovered server %s: %v", serverID, storeErr)
    }

    // Re-sync with database state
    server, err = GetTrustedServer(serverID)
    if err != nil {
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

// ============================================================================
// SECURITY & REQUEST SIGNING
// ============================================================================

// SignFederatedRequest signs a request with the server's Ed25519 private key.
// The signature payload follows the structure: METHOD|PATH|TIMESTAMP.
// This proves the request originated from the claimed ServerID and hasn't been tampered with.
func SignFederatedRequest(method, path, timestamp string) (string, error) {
    serverPrivKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
    if serverPrivKeyHex == "" {
        return "", fmt.Errorf("server private key not configured")
    }

    privKeyBytes, err := hex.DecodeString(serverPrivKeyHex)
    if err != nil {
        return "", fmt.Errorf("invalid private key format: %v", err)
    }

    // Canonical payload for signing
    payload := fmt.Sprintf("%s|%s|%s", method, path, timestamp)

    // Ed25519 Signing
    signature := ed25519.Sign(privKeyBytes, []byte(payload))

    return hex.EncodeToString(signature), nil
}



// ============================================================================
// FEDERATED SEARCH & USER API
// ============================================================================

// FederatedUserProfile represents the public-facing user profile shared across the network.
type FederatedUserProfile struct {
    UserID      string `json:"user_id"`      // Globally unique ID (username@domain)
    Username    string `json:"username"`     // Local handle
    DisplayName string `json:"display_name"` // Readable name
    AvatarURL   string `json:"avatar_url"`   // Public image resource
    Bio         string `json:"bio"`          // Short biography
    HomeServer  string `json:"home_server"`  // Originating server ID
}

// SearchFederatedUser queries a remote server for user information.
// It performs a secure GET request to the remote user profile endpoint.
func SearchFederatedUser(username, serverName string) (*FederatedUserProfile, string, error) {
    // 1. Server Whitelist/Discovery
    server, discoveryStatus, err := EnsureServerTrusted(serverName)
    if err != nil {
        return nil, discoveryStatus, err
    }

    // 2. Request Preparation
    path := fmt.Sprintf("/api/users/%s", username)
    fullURL := fmt.Sprintf("%s%s", server.Endpoint, path)
    timestamp := time.Now().UTC().Format(time.RFC3339)

    // 3. Cryptographic Proof
    signature, err := SignFederatedRequest("GET", path, timestamp)
    if err != nil {
        return nil, discoveryStatus, fmt.Errorf("failed to sign request: %v", err)
    }

    req, err := http.NewRequest("GET", fullURL, nil)
    if err != nil {
        return nil, discoveryStatus, fmt.Errorf("failed to create request: %v", err)
    }

    // 4. Header Injection
    serverID := os.Getenv("SERVER_ID")
    if serverID == "" {
        serverID = "server_a"
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Server-ID", serverID)
    req.Header.Set("X-Timestamp", timestamp)
    req.Header.Set("X-Signature", signature)

    // 5. Execution
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, discoveryStatus, fmt.Errorf("failed to contact remote server: %v", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, discoveryStatus, fmt.Errorf("failed to read response: %v", err)
    }

    if resp.StatusCode != http.StatusOK {
        return nil, discoveryStatus, fmt.Errorf("remote server returned error: %s", string(body))
    }

    var profile FederatedUserProfile
    if err := json.Unmarshal(body, &profile); err != nil {
        return nil, discoveryStatus, fmt.Errorf("failed to parse response: %v", err)
    }

    return &profile, discoveryStatus, nil
}

// FederatedSearchHandler handles federated user search requests from the UI.
// It detects if the query is local or remote and routes accordingly.
func FederatedSearchHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    query := r.URL.Query().Get("q")
    if query == "" {
        RespondWithJSON(w, http.StatusBadRequest, map[string]string{
            "error": "search query 'q' is required",
        })
        return
    }

    identity := ParseFederatedIdentity(query)

    // Route to Local search if no '@' is present
    if !identity.IsFederated {
        RespondWithJSON(w, http.StatusOK, map[string]interface{}{
            "found":     false,
            "federated": false,
            "message":   "Use local search endpoint for non-federated queries",
        })
        return
    }

    log.Printf("Federated search: username=%s, server=%s", identity.Username, identity.ServerName)

    // Execute remote search
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

    RespondWithJSON(w, http.StatusOK, map[string]interface{}{
        "found":            true,
        "federated":        true,
        "discovery_status": discoveryStatus,
        "user":             profile,
    })
}

// ============================================================================
// PUBLIC FEDERATION ENDPOINTS (INBOUND)
// ============================================================================

// GetPublicUserHandler returns public user information for federation.
// This is the public endpoint other servers call to resolve our users.
func GetPublicUserHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Monitoring requester server
    serverID := r.Header.Get("X-Server-ID")
    if serverID != "" {
        log.Printf("Public user API: requested by federated server '%s'", serverID)
    }

    // Resolve path variables
    path := r.URL.Path
    parts := strings.Split(strings.TrimRight(path, "/"), "/")
    if len(parts) < 4 {
        RespondWithJSON(w, http.StatusBadRequest, map[string]string{
            "error": "invalid user path",
        })
        return
    }

    // Sub-path routing (e.g., /posts)
    if parts[len(parts)-1] == "posts" {
        GetPublicUserPostsHandler(w, r)
        return
    }

    username := parts[len(parts)-1]
    log.Printf("Public user API: Looking for username '%s'", username)

    // Database Lookup
    // We match by full ID (alice@server) OR by prefix handle (alice) for flexibility.
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
    if len(parts) < 5 || parts[len(parts)-1] != "posts" {
        RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
        return
    }
    username := parts[len(parts)-2]
    viewerID := r.URL.Query().Get("viewer_id")

    // Internal ID resolution
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

    // Fetch posts using local business logic
    posts, err := GetUserPosts(internalUserID, internalViewer, 20, 0)
    if err != nil {
        RespondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch posts"})
        return
    }

    // Sanitize Author IDs for external consumption
    for i := range posts {
        posts[i].Author = ToExternalID(posts[i].Author)
    }
    RespondWithJSON(w, http.StatusOK, map[string]interface{}{"posts": posts})
}

// FederatedUserPostsHandler proxies a posts request to a remote server.
// URL: GET /api/posts/federated?user_id=alice@server_b&viewer_id=bob@server_a
// This serves as the Client-to-Server (C2S) proxy for reading remote timelines.
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

    // Self-routing: If the user belongs to us, don't federate.
    if serverID == getServerID() {
        r.URL.Path = "/api/users/" + username + "/posts"
        GetPublicUserPostsHandler(w, r)
        return
    }

    // Outbound Federation
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