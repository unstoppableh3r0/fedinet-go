package identity

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	fed "github.com/unstoppableh3r0/fedinet-go/internal/federation"
	fedcrypto "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// FederationSigHeader holds the three headers attached to every outbound
// federation request so the receiving server can verify authenticity.
type FederationSigHeader struct {
	ServerID  string
	Signature string
	Timestamp string
}

// GetServerURL returns the externally reachable base URL of this server.
// Read from SERVER_URL env var; falls back to http://localhost:8080.
func GetServerURL() string {
	if u := os.Getenv("SERVER_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// SetPostOrigin back-fills the origin_post column after a post is inserted.
func SetPostOrigin(postID, originPostID string) error {
	_, err := db.Exec(`UPDATE posts SET origin_post = $1 WHERE id = $2`, originPostID, postID)
	return err
}

// BuildFederationSignatureHeader signs a federation request using the server's
// Ed25519 private key. The signed message is "SERVER_ID:TIMESTAMP".
func BuildFederationSignatureHeader() FederationSigHeader {
	serverID, privKeyHex := getServerIDAndKey()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	message := serverID + ":" + ts

	sig, err := fedcrypto.SignData([]byte(message), privKeyHex)
	if err != nil {
		log.Printf("BuildFederationSignatureHeader: sign error: %v", err)
		sig = ""
	}
	return FederationSigHeader{ServerID: serverID, Signature: sig, Timestamp: ts}
}

// getServerIDAndKey reads the server's ID and private key from the database /
// environment. Falls back gracefully on errors.
func getServerIDAndKey() (string, string) {
	var serverID string
	_ = db.QueryRow(`SELECT server_id FROM server_identity WHERE id = 1`).Scan(&serverID)
	privKey := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	return serverID, privKey
}

// --- Capability cache ---------------------------------------------------

var (
	capCacheMu    sync.RWMutex
	capCache      = map[string]capEntry{}
)

type capEntry struct {
	linkedPosts bool
	fetchedAt   time.Time
}

const capCacheTTL = 2 * time.Hour

// RemoteServerSupportsLinkedPosts checks whether the remote server has
// advertised the linked_posts capability. Results are cached for 2 hours.
func RemoteServerSupportsLinkedPosts(serverURL string) bool {
	capCacheMu.RLock()
	entry, ok := capCache[serverURL]
	capCacheMu.RUnlock()

	if ok && time.Since(entry.fetchedAt) < capCacheTTL {
		return entry.linkedPosts
	}

	// Fetch capabilities from remote server
	caps, err := fed.FetchRemoteCapabilities(serverURL)
	if err != nil {
		log.Printf("RemoteServerSupportsLinkedPosts: failed to fetch caps from %s: %v", serverURL, err)
		return false
	}

	capCacheMu.Lock()
	capCache[serverURL] = capEntry{linkedPosts: caps.LinkedPosts, fetchedAt: time.Now()}
	capCacheMu.Unlock()

	return caps.LinkedPosts
}

// ---------------------------------------------------------------------------
// POST /federation/linked-post
//
// Receives a linked-post replication request from the origin server.
// The origin server sends this after creating a post so that this server
// can store a local replica with the same group_id for deduplication.
// ---------------------------------------------------------------------------

// HandleLinkedPostHandler receives a federated linked-post payload and stores
// a replica in the local posts table.
func HandleLinkedPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Verify the signature from the origin server
	serverID := r.Header.Get("X-Server-ID")
	sigHex := r.Header.Get("X-Signature")
	timestamp := r.Header.Get("X-Timestamp")

	if serverID == "" || sigHex == "" || timestamp == "" {
		RespondWithError(w, http.StatusUnauthorized, "missing federation signature headers")
		return
	}

	pubKey, err := GetTrustedServerPublicKey(serverID)
	if err != nil {
		log.Printf("HandleLinkedPostHandler: unknown server %s: %v", serverID, err)
		RespondWithError(w, http.StatusForbidden, "untrusted server")
		return
	}

	message := serverID + ":" + timestamp
	valid, err := fedcrypto.VerifySignature([]byte(message), sigHex, pubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusForbidden, "invalid federation signature")
		return
	}

	// Parse the linked-post payload
	var req struct {
		GroupID      string  `json:"group_id"`
		OriginPost   string  `json:"origin_post"`
		OriginServer string  `json:"origin_server"`
		Author       string  `json:"author"` // user_id on THIS server (e.g. may@serverB)
		Content      string  `json:"content"`
		Visibility   string  `json:"visibility"`
		ImageURL     *string `json:"image_url,omitempty"`
		ExpiresAt    *string `json:"expires_at,omitempty"`
		ContentWarn  *string `json:"content_warning,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.GroupID == "" || req.OriginPost == "" || req.OriginServer == "" || req.Author == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields: group_id, origin_post, origin_server, author")
		return
	}

	// Validate visibility
	switch req.Visibility {
	case "PUBLIC", "FOLLOWERS", "CLOSE_FRIENDS":
	default:
		req.Visibility = "PUBLIC"
	}

	// Parse optional expires_at
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		if ts, err := time.Parse(time.RFC3339, *req.ExpiresAt); err == nil {
			expiresAt = &ts
		}
	}

	internalAuthor := ToInternalID(req.Author)
	groupID := req.GroupID
	originPost := req.OriginPost
	originServer := req.OriginServer

	replicaID, err := CreatePost(
		internalAuthor,
		req.Content,
		req.ImageURL,
		req.Visibility,
		expiresAt,
		req.ContentWarn,
		&groupID,
		&originPost,
		&originServer,
	)
	if err != nil {
		log.Printf("HandleLinkedPostHandler: CreatePost error for %s: %v", internalAuthor, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store replica post")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"replica_id":    replicaID,
		"group_id":      groupID,
		"origin_post":   originPost,
		"origin_server": originServer,
	})
}

// ---------------------------------------------------------------------------
// POST /federation/propagate-edit
//
// Origin server broadcasts an edit to all replica servers for the same group_id.
// ---------------------------------------------------------------------------

// PropagateEditHandler receives an edit broadcast and updates the local replica.
func PropagateEditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Verify signature
	serverID := r.Header.Get("X-Server-ID")
	sigHex := r.Header.Get("X-Signature")
	timestamp := r.Header.Get("X-Timestamp")

	if serverID == "" || sigHex == "" || timestamp == "" {
		RespondWithError(w, http.StatusUnauthorized, "missing federation signature headers")
		return
	}

	pubKey, err := GetTrustedServerPublicKey(serverID)
	if err != nil {
		RespondWithError(w, http.StatusForbidden, "untrusted server")
		return
	}

	message := serverID + ":" + timestamp
	valid, err := fedcrypto.VerifySignature([]byte(message), sigHex, pubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusForbidden, "invalid federation signature")
		return
	}

	var req struct {
		GroupID  string `json:"group_id"`
		Version  int    `json:"version"`
		Content  string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.GroupID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id and content are required")
		return
	}

	// Update all local replica posts that share this group_id
	result, err := db.Exec(`
		UPDATE posts
		SET content = $1, updated_at = NOW()
		WHERE group_id = $2
		  AND (origin_server IS NULL OR origin_server != $3)
	`, req.Content, req.GroupID, GetServerURL())

	if err != nil {
		log.Printf("PropagateEditHandler: DB error for group %s: %v", req.GroupID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to apply edit")
		return
	}

	rows, _ := result.RowsAffected()
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"group_id":      req.GroupID,
		"rows_updated":  rows,
	})
}

// GetTrustedServerPublicKey looks up the Ed25519 public key for a trusted server
// by its server_id (X-Server-ID header value).
func GetTrustedServerPublicKey(serverID string) (string, error) {
	var pubKey string
	err := db.QueryRow(
		`SELECT public_key FROM trusted_servers WHERE server_id = $1`,
		serverID,
	).Scan(&pubKey)
	if err != nil {
		return "", fmt.Errorf("trusted server not found: %w", err)
	}
	return pubKey, nil
}
