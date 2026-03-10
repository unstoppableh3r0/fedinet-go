package identity

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// Vouch is a cryptographically-signed attestation that a trusted server has
// verified a user's identity.
type Vouch struct {
	ID                 string     `json:"id"`
	VouchedUserID      string     `json:"vouched_user_id"`
	VouchingServerID   string     `json:"vouching_server_id"`
	VouchingServerName string     `json:"vouching_server_name"`
	Signature          string     `json:"signature"`
	IssuedAt           time.Time  `json:"issued_at"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

// vouchPayload returns the canonical string that is signed/verified.
// Format: "<vouched_user_id>|<vouching_server_id>|<issued_at_rfc3339>"
func vouchPayload(vouchedUserID, vouchingServerID, issuedAtRFC3339 string) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s", vouchedUserID, vouchingServerID, issuedAtRFC3339))
}

// ---------------------------------------------------------------------------
// Core DB helpers
// ---------------------------------------------------------------------------

// signVouch creates an Ed25519 signature over the canonical vouch payload using
// the server's identity private key (SERVER_IDENTITY_PRIVATE_KEY env var).
func signVouch(vouchedUserID, vouchingServerID string, issuedAt time.Time) (string, error) {
	privKeyHex := os.Getenv("SERVER_IDENTITY_PRIVATE_KEY")
	if privKeyHex == "" {
		return "", fmt.Errorf("SERVER_IDENTITY_PRIVATE_KEY not set")
	}
	privBytes, err := hex.DecodeString(privKeyHex)
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid SERVER_IDENTITY_PRIVATE_KEY")
	}
	payload := vouchPayload(vouchedUserID, vouchingServerID, issuedAt.UTC().Format(time.RFC3339))
	sig := ed25519.Sign(ed25519.PrivateKey(privBytes), payload)
	return hex.EncodeToString(sig), nil
}

// verifyVouch verifies a vouch signature against the given Ed25519 public key (hex).
func verifyVouch(vouchedUserID, vouchingServerID, issuedAtRFC3339, signatureHex, pubKeyHex string) error {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key")
	}
	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature encoding")
	}
	payload := vouchPayload(vouchedUserID, vouchingServerID, issuedAtRFC3339)
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), payload, sigBytes) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func storeVouch(v Vouch) error {
	_, err := db.Exec(`
		INSERT INTO identity_vouches
			(id, vouched_user_id, vouching_server_id, vouching_server_name,
			 signature, issued_at, expires_at, revoked_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULL,NOW())
		ON CONFLICT (vouched_user_id, vouching_server_id) DO UPDATE
		  SET signature             = EXCLUDED.signature,
		      vouching_server_name  = EXCLUDED.vouching_server_name,
		      issued_at             = EXCLUDED.issued_at,
		      expires_at            = EXCLUDED.expires_at,
		      revoked_at            = NULL
	`, v.ID, v.VouchedUserID, v.VouchingServerID, v.VouchingServerName,
		v.Signature, v.IssuedAt, v.ExpiresAt)
	return err
}

// GetVouches returns all non-revoked, non-expired vouches for a user.
func GetVouches(userID string) ([]Vouch, error) {
	rows, err := db.Query(`
		SELECT id, vouched_user_id, vouching_server_id, vouching_server_name,
		       signature, issued_at, expires_at, revoked_at
		FROM identity_vouches
		WHERE vouched_user_id = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY issued_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vouches []Vouch
	for rows.Next() {
		var v Vouch
		var expiresAt, revokedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.VouchedUserID, &v.VouchingServerID, &v.VouchingServerName,
			&v.Signature, &v.IssuedAt, &expiresAt, &revokedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			v.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			v.RevokedAt = &revokedAt.Time
		}
		vouches = append(vouches, v)
	}
	return vouches, rows.Err()
}

// ---------------------------------------------------------------------------
// Admin: issue a vouch for a local user
// ---------------------------------------------------------------------------

// IssueVouchHandler handles POST /admin/vouch
// Body: { "user_id": "alice@server_a", "expires_in": "30d" }  (expires_in optional)
func IssueVouchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID    string  `json:"user_id"`
		ExpiresIn *string `json:"expires_in"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server_a"
	}

	// Fetch this server's display name from server_config
	var serverName string
	if err := db.QueryRow(`SELECT COALESCE(value,'') FROM server_config WHERE key='server_name'`).Scan(&serverName); err != nil {
		serverName = serverID
	}

	issuedAt := time.Now().UTC()

	sig, err := signVouch(req.UserID, serverID, issuedAt)
	if err != nil {
		log.Printf("IssueVouch: sign error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to sign vouch")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn != "" {
		if d, err := parseExpiryShorthand(*req.ExpiresIn); err == nil {
			t := issuedAt.Add(d)
			expiresAt = &t
		}
	}

	v := Vouch{
		ID:                 uuid.New().String(),
		VouchedUserID:      req.UserID,
		VouchingServerID:   serverID,
		VouchingServerName: serverName,
		Signature:          sig,
		IssuedAt:           issuedAt,
		ExpiresAt:          expiresAt,
	}

	if err := storeVouch(v); err != nil {
		log.Printf("IssueVouch: db error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store vouch")
		return
	}

	// Propagate this vouch to all trusted peers
	go propagateVouch(v)

	RespondWithJSON(w, http.StatusCreated, v)
}



// ---------------------------------------------------------------------------
// Admin: revoke a vouch
// ---------------------------------------------------------------------------

// RevokeVouchHandler handles POST /admin/vouch/revoke
// Body: { "user_id": "alice@server_a" }
func RevokeVouchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server_a"
	}

	res, err := db.Exec(`
		UPDATE identity_vouches
		SET revoked_at = NOW()
		WHERE vouched_user_id = $1 AND vouching_server_id = $2 AND revoked_at IS NULL
	`, req.UserID, serverID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	n, _ := res.RowsAffected()
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"revoked": n > 0})
}

// ---------------------------------------------------------------------------
// Public: get vouches for any user
// ---------------------------------------------------------------------------

// GetVouchesHandler handles GET /api/vouches?user_id=alice@server_a
func GetVouchesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	// If the user is on a remote server, proxy the request there so callers
	// always get a complete picture (including vouches issued by the home server).
	if parts := splitUserID(userID); parts[1] != "" {
		myServerID := os.Getenv("SERVER_ID")
		if myServerID == "" {
			myServerID = "server_a"
		}
		if parts[1] != myServerID {
			proxyVouchQuery(w, userID, parts[1])
			return
		}
	}

	vouches, err := GetVouches(userID)
	if err != nil {
		log.Printf("GetVouchesHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	if vouches == nil {
		vouches = []Vouch{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": vouches})
}

// splitUserID returns [username, serverID] from "alice@server_a".
func splitUserID(id string) [2]string {
	for i := 0; i < len(id); i++ {
		if id[i] == '@' {
			return [2]string{id[:i], id[i+1:]}
		}
	}
	return [2]string{id, ""}
}

// proxyVouchQuery forwards the /api/vouches?user_id=... request to the user's
// home server and relays the response.
func proxyVouchQuery(w http.ResponseWriter, userID, serverID string) {
	server, err := GetTrustedServer(serverID)
	if err != nil {
		// Server not trusted — return local vouches only (may be empty)
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": []Vouch{}})
		return
	}

	url := fmt.Sprintf("%s/api/vouches?user_id=%s", server.Endpoint, userID)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": []Vouch{}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// ---------------------------------------------------------------------------
// Federation: receive an incoming vouch from a peer server
// ---------------------------------------------------------------------------

// HandleIncomingVouch handles POST /federation/vouch
// Body: Vouch JSON sent by a peer server after it issues a vouch.
func HandleIncomingVouch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var v Vouch
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid vouch payload")
		return
	}

	if v.VouchedUserID == "" || v.VouchingServerID == "" || v.Signature == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// Look up the vouching server's public key — it must already be trusted.
	server, err := GetTrustedServer(v.VouchingServerID)
	if err != nil {
		log.Printf("HandleIncomingVouch: untrusted server %s: %v", v.VouchingServerID, err)
		RespondWithError(w, http.StatusForbidden, "vouching server is not trusted")
		return
	}

	// Verify the signature.
	issuedAtStr := v.IssuedAt.UTC().Format(time.RFC3339)
	if err := verifyVouch(v.VouchedUserID, v.VouchingServerID, issuedAtStr, v.Signature, server.PublicKey); err != nil {
		log.Printf("HandleIncomingVouch: invalid signature from %s: %v", v.VouchingServerID, err)
		RespondWithError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	// Persist — use the vouching server's name if we have it, otherwise keep existing.
	if v.VouchingServerName == "" {
		v.VouchingServerName = server.ServerName
	}
	if v.ID == "" {
		v.ID = uuid.New().String()
	}

	if err := storeVouch(v); err != nil {
		log.Printf("HandleIncomingVouch: db error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store vouch")
		return
	}

	log.Printf("✅ Stored vouch: %s vouched by %s", v.VouchedUserID, v.VouchingServerID)
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// ---------------------------------------------------------------------------
// Federation: propagate a newly-issued vouch to all trusted peers
// ---------------------------------------------------------------------------

func propagateVouch(v Vouch) {
	rows, err := db.Query(`SELECT endpoint FROM trusted_servers WHERE endpoint != ''`)
	if err != nil {
		log.Printf("propagateVouch: failed to list trusted servers: %v", err)
		return
	}
	defer rows.Close()

	body, _ := json.Marshal(v)

	for rows.Next() {
		var endpoint string
		if err := rows.Scan(&endpoint); err != nil {
			continue
		}
		url := endpoint + "/federation/vouch"
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("propagateVouch: failed to deliver to %s: %v", endpoint, err)
			continue
		}
		resp.Body.Close()
		log.Printf("propagateVouch: delivered to %s → %s", endpoint, resp.Status)
	}
}

// ---------------------------------------------------------------------------
// Federation: fetch vouches from all trusted peers for a user (aggregated view)
// ---------------------------------------------------------------------------

// FetchRemoteVouches queries every trusted server for vouches on userID and
// merges them with locally-stored ones.  Used for the profile page aggregation.
func FetchRemoteVouches(userID string) []Vouch {
	local, _ := GetVouches(userID)

	rows, err := db.Query(`SELECT server_id, endpoint FROM trusted_servers WHERE endpoint != ''`)
	if err != nil {
		return local
	}
	defer rows.Close()

	myServerID := os.Getenv("SERVER_ID")
	if myServerID == "" {
		myServerID = "server_a"
	}

	seen := map[string]bool{}
	for _, v := range local {
		seen[v.VouchingServerID] = true
	}

	client := &http.Client{Timeout: 4 * time.Second}

	for rows.Next() {
		var sid, endpoint string
		if err := rows.Scan(&sid, &endpoint); err != nil {
			continue
		}
		if seen[sid] {
			continue
		}
		url := fmt.Sprintf("%s/api/vouches?user_id=%s", endpoint, userID)
		resp, err := client.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		var body struct {
			Vouches []Vouch `json:"vouches"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			local = append(local, body.Vouches...)
		}
		resp.Body.Close()
	}

	return local
}

// GetAggregatedVouchesHandler handles GET /api/vouches/aggregate?user_id=...
// Returns vouches collected from this server AND all trusted peers.
func GetAggregatedVouchesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	vouches := FetchRemoteVouches(userID)
	if err := db.QueryRow(`SELECT 1 FROM pg_tables WHERE tablename='identity_vouches'`).Scan(new(int)); err != nil {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": []Vouch{}})
		return
	}

	if vouches == nil {
		vouches = []Vouch{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": vouches})
}

// ---------------------------------------------------------------------------
// Admin: list all vouches issued by this server
// ---------------------------------------------------------------------------

// ListIssuedVouchesHandler handles GET /admin/vouches
func ListIssuedVouchesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverID := os.Getenv("SERVER_ID")
	if serverID == "" {
		serverID = "server_a"
	}

	rows, err := db.Query(`
		SELECT id, vouched_user_id, vouching_server_id, vouching_server_name,
		       signature, issued_at, expires_at, revoked_at
		FROM identity_vouches
		WHERE vouching_server_id = $1
		ORDER BY issued_at DESC
	`, serverID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var vouches []Vouch
	for rows.Next() {
		var v Vouch
		var expiresAt, revokedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.VouchedUserID, &v.VouchingServerID, &v.VouchingServerName,
			&v.Signature, &v.IssuedAt, &expiresAt, &revokedAt); err != nil {
			continue
		}
		if expiresAt.Valid {
			v.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			v.RevokedAt = &revokedAt.Time
		}
		vouches = append(vouches, v)
	}
	if vouches == nil {
		vouches = []Vouch{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"vouches": vouches})
}
