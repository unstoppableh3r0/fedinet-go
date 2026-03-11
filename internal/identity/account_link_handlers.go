package identity

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// accountLinkRow is the DB shape for a single account_links row plus profile data.
type accountLinkRow struct {
	ID            string
	RequesterID   string
	TargetID      string
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	RequesterName string
	RequesterAvat string
	TargetName    string
	TargetAvat    string
}

// accountLinkJSON is the JSON shape returned to the frontend.
type accountLinkJSON struct {
	ID              string `json:"id"`
	RequesterID     string `json:"requester_id"`
	TargetID        string `json:"target_id"`
	Status          string `json:"status"`
	CanRemove       bool   `json:"can_remove"`
	IsInbound       bool   `json:"is_inbound"`
	RequesterName   string `json:"requester_name,omitempty"`
	RequesterAvatar string `json:"requester_avatar,omitempty"`
	TargetName      string `json:"target_name,omitempty"`
	TargetAvatar    string `json:"target_avatar,omitempty"`
	CreatedAt       string `json:"created_at"`
	// PeerServer is the external (browser-accessible) home_server URL for the
	// peer account.  The frontend uses this to correctly route API calls after
	// an account switch.
	PeerServer string `json:"peer_server_url,omitempty"`
}

// getPeerURL resolves a federated server's identity endpoint URL from the
// FEDERATION_PEERS env var (format: "server_b=http://host:port,server_c=http://...")
func getPeerURL(serverID string) string {
	peers := os.Getenv("FEDERATION_PEERS")
	if peers == "" {
		return ""
	}
	for _, pair := range strings.Split(peers, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == serverID {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}

// getPeerExternalURL resolves a peer server's browser-accessible (external) URL.
// Reads FEDERATION_PEERS_EXTERNAL env var (same format as FEDERATION_PEERS).
func getPeerExternalURL(serverID string) string {
	peers := os.Getenv("FEDERATION_PEERS_EXTERNAL")
	if peers == "" {
		return ""
	}
	for _, pair := range strings.Split(peers, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == serverID {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}

// forwardLinkRequest replicates a link request to the target server so both
// servers hold a copy of the row (same UUID), enabling cross-server accept/reject.
func forwardLinkRequest(peerURL, requesterID, targetID, linkID string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"requester_id": requesterID,
		"target_id":    targetID,
		"link_id":      linkID,
		"forwarded":    true,
	})
	resp, err := http.Post(peerURL+"/account/link/request", "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("forwardLinkRequest to %s: %v", peerURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("forwardLinkRequest to %s: status=%s", peerURL, resp.Status)
}

// syncLinkStatus propagates an accept/reject decision to the peer server that
// holds the other copy of the link row.
func syncLinkStatus(peerURL, linkID, newStatus string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"link_id": linkID,
		"status":  newStatus,
	})
	resp, err := http.Post(peerURL+"/account/link/sync", "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("syncLinkStatus to %s: %v", peerURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("syncLinkStatus to %s: status=%s", peerURL, resp.Status)
}

// GET /account/links?user_id=...
func GetAccountLinksUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	internalUserID := ToInternalID(userID)

	rows, err := db.Query(`
		SELECT
			al.id,
			al.requester_id,
			al.target_id,
			al.status,
			al.created_at,
			al.updated_at,
			COALESCE(rp.display_name, ''),
			COALESCE(rp.avatar_url, ''),
			COALESCE(tp.display_name, ''),
			COALESCE(tp.avatar_url, '')
		FROM account_links al
		LEFT JOIN profiles rp ON rp.user_id = al.requester_id
		LEFT JOIN profiles tp ON tp.user_id = al.target_id
		WHERE (al.requester_id = $1 OR al.target_id = $1)
		  AND al.status != 'rejected'
		ORDER BY al.created_at DESC
	`, internalUserID)
	if err != nil {
		log.Printf("GetAccountLinksUserHandler query: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to load links")
		return
	}
	defer rows.Close()

	var links []accountLinkJSON
	for rows.Next() {
		var row accountLinkRow
		if err := rows.Scan(
			&row.ID, &row.RequesterID, &row.TargetID, &row.Status,
			&row.CreatedAt, &row.UpdatedAt,
			&row.RequesterName, &row.RequesterAvat,
			&row.TargetName, &row.TargetAvat,
		); err != nil {
			log.Printf("GetAccountLinksUserHandler scan: %v", err)
			continue
		}

		isInbound := row.RequesterID != internalUserID
		canRemove := row.Status == "confirmed" || row.RequesterID == internalUserID

		// Compute the peer's external (browser-facing) home_server URL.
		peerIDInternal := row.TargetID
		if isInbound {
			peerIDInternal = row.RequesterID
		}
		peerServerID := ""
		if parts := strings.SplitN(peerIDInternal, "@", 2); len(parts) == 2 {
			peerServerID = parts[1]
		}
		myServerID := os.Getenv("SERVER_ID")
		if myServerID == "" {
			myServerID = "localhost"
		}
		peerServerURL := ""
		if peerServerID == "" || peerServerID == myServerID {
			peerServerURL = os.Getenv("SERVER_URL")
			if peerServerURL == "" {
				peerServerURL = "http://localhost:8080"
			}
		} else {
			peerServerURL = getPeerExternalURL(peerServerID)
		}

		links = append(links, accountLinkJSON{
			ID:              row.ID,
			RequesterID:     ToExternalID(row.RequesterID),
			TargetID:        ToExternalID(row.TargetID),
			Status:          row.Status,
			CanRemove:       canRemove,
			IsInbound:       isInbound,
			RequesterName:   row.RequesterName,
			RequesterAvatar: row.RequesterAvat,
			TargetName:      row.TargetName,
			TargetAvatar:    row.TargetAvat,
			CreatedAt:       row.CreatedAt.Format(time.RFC3339),
			PeerServer:      peerServerURL,
		})
	}

	if links == nil {
		links = []accountLinkJSON{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"links": links,
		"count": len(links),
	})
}

// POST /account/link/request
// Body: {"requester_id": "...", "target_id": "..."}
func RequestAccountLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		RequesterID string `json:"requester_id"`
		TargetID    string `json:"target_id"`
		// Forwarded is set by the peer server when replicating; prevents infinite loops.
		Forwarded bool   `json:"forwarded"`
		LinkID    string `json:"link_id"` // provided by originating server when forwarded
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.RequesterID == "" || body.TargetID == "" {
		RespondWithError(w, http.StatusBadRequest, "requester_id and target_id required")
		return
	}

	requester := ToInternalID(body.RequesterID)
	target := ToInternalID(body.TargetID)

	if requester == target {
		RespondWithError(w, http.StatusBadRequest, "cannot link to yourself")
		return
	}

	var linkID string
	var err error
	if body.Forwarded && body.LinkID != "" {
		// Use the UUID provided by the originating server so both DBs share the same row ID.
		err = db.QueryRow(`
			INSERT INTO account_links (id, requester_id, target_id, status)
			VALUES ($1::uuid, $2, $3, 'pending')
			ON CONFLICT (requester_id, target_id) DO UPDATE
				SET status = CASE
					WHEN account_links.status = 'rejected' THEN 'pending'
					ELSE account_links.status
				END,
				updated_at = NOW()
			RETURNING id
		`, body.LinkID, requester, target).Scan(&linkID)
	} else {
		err = db.QueryRow(`
			INSERT INTO account_links (requester_id, target_id, status)
			VALUES ($1, $2, 'pending')
			ON CONFLICT (requester_id, target_id) DO UPDATE
				SET status = CASE
					WHEN account_links.status = 'rejected' THEN 'pending'
					ELSE account_links.status
				END,
				updated_at = NOW()
			RETURNING id
		`, requester, target).Scan(&linkID)
	}
	if err != nil {
		log.Printf("RequestAccountLinkHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create link request")
		return
	}

	// Notify the target so they see the request immediately
	go CreateNotification(target, requester, "LINK_REQUEST", linkID)

	// If the target is on a different server, replicate the link row there so
	// the target user can see (and action) the incoming request on their server.
	if !body.Forwarded {
		if isFederated, targetServerID := IsFederatedUser(target); isFederated {
			if peerURL := getPeerURL(targetServerID); peerURL != "" {
				go forwardLinkRequest(peerURL, requester, target, linkID)
			}
		}
	}

	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"link_id": linkID,
		"status":  "pending",
		"message": "Link request sent",
	})
}

// POST /account/link/accept
// Body: {"user_id": "...", "link_id": "..."}
func AcceptAccountLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		UserID string `json:"user_id"`
		LinkID string `json:"link_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.UserID == "" || body.LinkID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and link_id required")
		return
	}

	internalUserID := ToInternalID(body.UserID)

	res, err := db.Exec(`
		UPDATE account_links
		SET status = 'confirmed', updated_at = NOW()
		WHERE id = $1 AND target_id = $2 AND status = 'pending'
	`, body.LinkID, internalUserID)
	if err != nil {
		log.Printf("AcceptAccountLinkHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to accept link")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		RespondWithError(w, http.StatusNotFound, "link not found or not pending")
		return
	}

	// Sync the confirmed status to the peer server that holds the other copy.
	go func() {
		var requesterID string
		if err := db.QueryRow(`SELECT requester_id FROM account_links WHERE id = $1`, body.LinkID).Scan(&requesterID); err == nil {
			if isFederated, peerServerID := IsFederatedUser(requesterID); isFederated {
				if peerURL := getPeerURL(peerServerID); peerURL != "" {
					syncLinkStatus(peerURL, body.LinkID, "confirmed")
				}
			}
		}
	}()

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Link accepted"})
}

// POST /account/link/reject
// Body: {"user_id": "...", "link_id": "..."}
func RejectAccountLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		UserID string `json:"user_id"`
		LinkID string `json:"link_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.UserID == "" || body.LinkID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and link_id required")
		return
	}

	internalUserID := ToInternalID(body.UserID)

	res, err := db.Exec(`
		UPDATE account_links
		SET status = 'rejected', updated_at = NOW()
		WHERE id = $1 AND target_id = $2 AND status = 'pending'
	`, body.LinkID, internalUserID)
	if err != nil {
		log.Printf("RejectAccountLinkHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to reject link")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		RespondWithError(w, http.StatusNotFound, "link not found or not pending")
		return
	}

	// Sync the rejected status to the peer server that holds the other copy.
	go func() {
		var requesterID string
		if err := db.QueryRow(`SELECT requester_id FROM account_links WHERE id = $1`, body.LinkID).Scan(&requesterID); err == nil {
			if isFederated, peerServerID := IsFederatedUser(requesterID); isFederated {
				if peerURL := getPeerURL(peerServerID); peerURL != "" {
					syncLinkStatus(peerURL, body.LinkID, "rejected")
				}
			}
		}
	}()

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Link rejected"})
}

// POST /account/link/switch
// Headers: Authorization: Bearer <current_user_access_token>
// Body: {"to_user_id": "..."}
// Verifies a confirmed link exists between the token owner and to_user_id,
// then issues a new token pair for to_user_id (passwordless account switch).
func SwitchAccountLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authenticate the current user via Bearer JWT
	authHeader := r.Header.Get("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		RespondWithError(w, http.StatusUnauthorized, "missing or invalid authorization header")
		return
	}
	claims, err := ValidateUserToken(parts[1])
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	var body struct {
		ToUserID string `json:"to_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ToUserID == "" {
		RespondWithError(w, http.StatusBadRequest, "to_user_id required")
		return
	}

	fromUserID := claims.UserID // internal format from JWT
	toUserID := ToInternalID(body.ToUserID)

	if fromUserID == toUserID {
		RespondWithError(w, http.StatusBadRequest, "cannot switch to the same account")
		return
	}

	// Verify a confirmed link exists between the two accounts (in either direction)
	var linkCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM account_links
		WHERE status = 'confirmed'
		  AND (
			(requester_id = $1 AND target_id = $2) OR
			(requester_id = $2 AND target_id = $1)
		  )
	`, fromUserID, toUserID).Scan(&linkCount)
	if err != nil {
		log.Printf("SwitchAccountLinkHandler link check: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to verify link")
		return
	}
	if linkCount == 0 {
		RespondWithError(w, http.StatusForbidden, "no confirmed link between accounts")
		return
	}

	// Determine if this is a cross-server switch.
	// toUserID is in internal format: "username@server_id"
	targetServerID := ""
	if parts := strings.SplitN(toUserID, "@", 2); len(parts) == 2 {
		targetServerID = parts[1]
	}
	myServerID := os.Getenv("SERVER_ID")
	if myServerID == "" {
		myServerID = "localhost"
	}

	if targetServerID != "" && targetServerID != myServerID {
		// Cross-server switch: this server cannot issue tokens valid on the
		// target server.  Tell the frontend to fall back to password login on
		// the target server (which it will fetch from peer_server_url in the
		// linked-accounts list).
		RespondWithError(w, http.StatusUnprocessableEntity, "cross-server switch: please log in with your password on the target server")
		return
	}

	// Same-server switch — issue tokens as normal.
	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}

	accessToken, refreshToken, err := GenerateTokenPair(toUserID, externalURL)
	if err != nil {
		log.Printf("SwitchAccountLinkHandler token generation: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"user_id":       ToExternalID(toUserID),
		"home_server":   externalURL,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

// POST /account/link/remove
// Body: {"user_id": "...", "link_id": "..."}
func RemoveAccountLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		UserID string `json:"user_id"`
		LinkID string `json:"link_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.UserID == "" || body.LinkID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and link_id required")
		return
	}

	internalUserID := ToInternalID(body.UserID)

	res, err := db.Exec(`
		DELETE FROM account_links
		WHERE id = $1 AND (requester_id = $2 OR target_id = $2)
	`, body.LinkID, internalUserID)
	if err != nil {
		log.Printf("RemoveAccountLinkHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to remove link")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		RespondWithError(w, http.StatusNotFound, "link not found")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Link removed"})
}

// POST /account/link/sync  (server-to-server only, not exposed to end-users)
// Body: {"link_id": "...", "status": "confirmed"|"rejected"}
// Called by a peer server to replicate an accept/reject decision.
func SyncAccountLinkStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		LinkID string `json:"link_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LinkID == "" {
		RespondWithError(w, http.StatusBadRequest, "link_id and status required")
		return
	}
	if body.Status != "confirmed" && body.Status != "rejected" {
		RespondWithError(w, http.StatusBadRequest, "status must be confirmed or rejected")
		return
	}

	_, err := db.Exec(`
		UPDATE account_links SET status = $1, updated_at = NOW() WHERE id = $2
	`, body.Status, body.LinkID)
	if err != nil {
		log.Printf("SyncAccountLinkStatusHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "sync failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "synced"})
}
