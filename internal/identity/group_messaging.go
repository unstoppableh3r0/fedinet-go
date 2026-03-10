package identity

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// groupMasterKey returns the SERVER_MASTER_KEY used to wrap group keys at rest.
func groupMasterKey() string {
	k := os.Getenv("SERVER_MASTER_KEY")
	if k == "" {
		// 32-byte zero key — same fallback used everywhere else in the codebase.
		k = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	return k
}

// generateGroupKey creates a random 256-bit symmetric key (AES-GCM) and returns
// it as a 64-character lowercase hex string.
func generateGroupKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// fetchGroupKey retrieves and decrypts the AES key for groupID.
func fetchGroupKey(groupID string) (string, error) {
	var encKey string
	if err := db.QueryRow(
		`SELECT encrypted_group_key FROM group_chats WHERE id = $1`, groupID,
	).Scan(&encKey); err != nil {
		return "", err
	}
	return crypto.Decrypt(encKey, groupMasterKey())
}

// isGroupMember reports whether userID is a member (any role) of groupID.
func isGroupMember(groupID, userID string) (bool, error) {
	var ok bool
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id=$1 AND user_id=$2)`,
		groupID, userID,
	).Scan(&ok)
	return ok, err
}

// isGroupAdmin reports whether userID holds the 'admin' role in groupID.
func isGroupAdmin(groupID, userID string) (bool, error) {
	var role string
	err := db.QueryRow(
		`SELECT role FROM group_members WHERE group_id=$1 AND user_id=$2`,
		groupID, userID,
	).Scan(&role)
	if err != nil {
		return false, err
	}
	return role == "admin", nil
}

// requireGroupAuth extracts the Bearer JWT and returns the validated internal
// user ID.  Returns ("", false) and writes a 401 response if auth fails.
func requireGroupAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		RespondWithError(w, http.StatusUnauthorized, "missing or invalid authorization header")
		return "", false
	}
	claims, err := ValidateUserToken(parts[1])
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
		return "", false
	}
	return claims.UserID, true
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// CreateGroupHandler creates a new encrypted group chat and adds the creator
// plus any requested initial members.
//
// POST /groups/create
// Headers: Authorization: Bearer <token>
// Body:    {"name":"dev team","members":["alice@server_a","bob@server_a"]}
func CreateGroupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		RespondWithError(w, http.StatusBadRequest, "group name is required")
		return
	}

	// Generate a unique per-group AES-256 key and wrap it with the server key.
	groupKeyHex, err := generateGroupKey()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to generate group key")
		return
	}
	encryptedKey, err := crypto.Encrypt(groupKeyHex, groupMasterKey())
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to encrypt group key")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	var groupID string
	if err = tx.QueryRow(
		`INSERT INTO group_chats (name, created_by, encrypted_group_key)
		 VALUES ($1, $2, $3) RETURNING id`,
		req.Name, userID, encryptedKey,
	).Scan(&groupID); err != nil {
		log.Printf("CreateGroup: insert group failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create group")
		return
	}

	// Creator is always an admin.
	if _, err = tx.Exec(
		`INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'admin')`,
		groupID, userID,
	); err != nil {
		log.Printf("CreateGroup: add creator failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to add creator")
		return
	}

	// Add requested initial members (skip creator if listed, skip silently on error).
	for _, m := range req.Members {
		memberID := ToInternalID(m)
		if memberID == userID {
			continue
		}
		if _, err = tx.Exec(
			`INSERT INTO group_members (group_id, user_id, role)
			 VALUES ($1, $2, 'member') ON CONFLICT DO NOTHING`,
			groupID, memberID,
		); err != nil {
			log.Printf("CreateGroup: add member %s failed: %v", memberID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"group_id": groupID,
		"name":     req.Name,
	})
}

// ListGroupsHandler returns all groups the authenticated user belongs to,
// including member counts and the timestamp of the last message.
//
// GET /groups
// Headers: Authorization: Bearer <token>
func ListGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	rows, err := db.Query(`
		SELECT gc.id, gc.name, gc.created_by, gm.role, gc.created_at,
		       (SELECT COUNT(*) FROM group_members  WHERE group_id = gc.id) AS member_count,
		       (SELECT MAX(created_at) FROM group_messages WHERE group_id = gc.id) AS last_message_at
		FROM group_chats gc
		JOIN group_members gm ON gm.group_id = gc.id AND gm.user_id = $1
		ORDER BY gc.created_at DESC
	`, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type groupEntry struct {
		ID            string     `json:"id"`
		Name          string     `json:"name"`
		CreatedBy     string     `json:"created_by"`
		Role          string     `json:"role"`
		CreatedAt     time.Time  `json:"created_at"`
		MemberCount   int        `json:"member_count"`
		LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	}

	var groups []groupEntry
	for rows.Next() {
		var g groupEntry
		var lastMsg sql.NullTime
		if err := rows.Scan(
			&g.ID, &g.Name, &g.CreatedBy, &g.Role, &g.CreatedAt,
			&g.MemberCount, &lastMsg,
		); err != nil {
			log.Printf("ListGroups: scan error: %v", err)
			continue
		}
		if lastMsg.Valid {
			g.LastMessageAt = &lastMsg.Time
		}
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []groupEntry{}
	}

	RespondWithJSON(w, http.StatusOK, groups)
}

// GetGroupMembersHandler lists members of a group (members only).
//
// GET /groups/members?group_id=<id>
// Headers: Authorization: Bearer <token>
func GetGroupMembersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id required")
		return
	}

	if member, err := isGroupMember(groupID, userID); err != nil || !member {
		RespondWithError(w, http.StatusForbidden, "not a member of this group")
		return
	}

	rows, err := db.Query(
		`SELECT user_id, role, joined_at FROM group_members WHERE group_id = $1 ORDER BY joined_at`,
		groupID,
	)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type memberEntry struct {
		UserID   string    `json:"user_id"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	var members []memberEntry
	for rows.Next() {
		var m memberEntry
		if err := rows.Scan(&m.UserID, &m.Role, &m.JoinedAt); err != nil {
			continue
		}
		members = append(members, m)
	}
	if members == nil {
		members = []memberEntry{}
	}

	RespondWithJSON(w, http.StatusOK, members)
}

// AddGroupMemberHandler adds a user to a group (admin only).
//
// POST /groups/members/add
// Headers: Authorization: Bearer <token>
// Body:    {"group_id":"...","user_id":"charlie@server_a"}
func AddGroupMemberHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	var req struct {
		GroupID string `json:"group_id"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id and user_id required")
		return
	}

	if admin, err := isGroupAdmin(req.GroupID, userID); err != nil || !admin {
		RespondWithError(w, http.StatusForbidden, "only group admins can add members")
		return
	}

	newMember := ToInternalID(req.UserID)
	if _, err := db.Exec(
		`INSERT INTO group_members (group_id, user_id, role)
		 VALUES ($1, $2, 'member') ON CONFLICT DO NOTHING`,
		req.GroupID, newMember,
	); err != nil {
		log.Printf("AddGroupMember: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "member added"})
}

// RemoveGroupMemberHandler removes a user from a group (admin only).
// Prevents removing the last remaining admin.
//
// POST /groups/members/remove
// Headers: Authorization: Bearer <token>
// Body:    {"group_id":"...","user_id":"charlie@server_a"}
func RemoveGroupMemberHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	var req struct {
		GroupID string `json:"group_id"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id and user_id required")
		return
	}

	if admin, err := isGroupAdmin(req.GroupID, userID); err != nil || !admin {
		RespondWithError(w, http.StatusForbidden, "only group admins can remove members")
		return
	}

	target := ToInternalID(req.UserID)

	// Refuse to remove the last admin.
	var adminCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND role='admin'`, req.GroupID,
	).Scan(&adminCount); err == nil && adminCount == 1 {
		var targetRole string
		_ = db.QueryRow(
			`SELECT role FROM group_members WHERE group_id=$1 AND user_id=$2`, req.GroupID, target,
		).Scan(&targetRole)
		if targetRole == "admin" {
			RespondWithError(w, http.StatusConflict,
				"cannot remove the only admin; promote another member first")
			return
		}
	}

	if _, err := db.Exec(
		`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`, req.GroupID, target,
	); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "member removed"})
}

// SendGroupMessageHandler encrypts a message with the group's AES key and
// stores the ciphertext.  Plaintext never touches the database.
//
// POST /groups/message
// Headers: Authorization: Bearer <token>
// Body:    {"group_id":"...","content":"Hello team!"}
func SendGroupMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	var req struct {
		GroupID string `json:"group_id"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.GroupID == "" || strings.TrimSpace(req.Content) == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id and content required")
		return
	}

	if member, err := isGroupMember(req.GroupID, userID); err != nil || !member {
		RespondWithError(w, http.StatusForbidden, "not a member of this group")
		return
	}

	groupKey, err := fetchGroupKey(req.GroupID)
	if err != nil {
		log.Printf("SendGroupMessage: key fetch failed for group %s: %v", req.GroupID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve group key")
		return
	}

	encrypted, err := crypto.Encrypt(req.Content, groupKey)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	var msgID string
	if err = db.QueryRow(
		`INSERT INTO group_messages (group_id, sender_id, encrypted_content)
		 VALUES ($1, $2, $3) RETURNING id`,
		req.GroupID, userID, encrypted,
	).Scan(&msgID); err != nil {
		log.Printf("SendGroupMessage: insert failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store message")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{"message_id": msgID})
}

// GetGroupMessagesHandler fetches and decrypts messages for a group.
// Supports pagination via the optional `before` (RFC3339) and `limit` query params.
//
// GET /groups/messages?group_id=<id>[&limit=50][&before=2026-01-01T00:00:00Z]
// Headers: Authorization: Bearer <token>
func GetGroupMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, ok := requireGroupAuth(w, r)
	if !ok {
		return
	}

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		RespondWithError(w, http.StatusBadRequest, "group_id required")
		return
	}

	if member, err := isGroupMember(groupID, userID); err != nil || !member {
		RespondWithError(w, http.StatusForbidden, "not a member of this group")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}

	var (
		query string
		args  []interface{}
	)
	base := `SELECT id, sender_id, encrypted_content, created_at FROM group_messages WHERE group_id = $1`
	if before := r.URL.Query().Get("before"); before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			RespondWithError(w, http.StatusBadRequest, "invalid 'before' timestamp; use RFC3339")
			return
		}
		query = base + " AND created_at < $2 ORDER BY created_at DESC LIMIT $3"
		args = []interface{}{groupID, t, limit}
	} else {
		query = base + " ORDER BY created_at DESC LIMIT $2"
		args = []interface{}{groupID, limit}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	groupKey, err := fetchGroupKey(groupID)
	if err != nil {
		log.Printf("GetGroupMessages: key fetch failed for group %s: %v", groupID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve group key")
		return
	}

	type msgEntry struct {
		ID        string    `json:"id"`
		SenderID  string    `json:"sender_id"`
		Content   string    `json:"content"`
		CreatedAt time.Time `json:"created_at"`
	}

	var messages []msgEntry
	for rows.Next() {
		var m msgEntry
		var encContent string
		if err := rows.Scan(&m.ID, &m.SenderID, &encContent, &m.CreatedAt); err != nil {
			log.Printf("GetGroupMessages: scan error: %v", err)
			continue
		}
		plaintext, err := crypto.Decrypt(encContent, groupKey)
		if err != nil {
			log.Printf("GetGroupMessages: decrypt failed for msg %s: %v", m.ID, err)
			m.Content = "[encrypted]"
		} else {
			m.Content = plaintext
		}
		messages = append(messages, m)
	}
	if messages == nil {
		messages = []msgEntry{}
	}

	RespondWithJSON(w, http.StatusOK, messages)
}
