package identity

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ── Badge helpers ──────────────────────────────────────────────────────────────

// GetUserBadge returns the badge for a user, defaulting to "user".
func GetUserBadge(userID string) string {
	var badge string
	err := db.QueryRow(`SELECT COALESCE(badge, 'user') FROM identities WHERE user_id = $1`, userID).Scan(&badge)
	if err != nil {
		return "user"
	}
	if badge == "" {
		return "user"
	}
	return badge
}

// SetUserBadge sets the badge column for a user.
func SetUserBadge(userID, badge string) error {
	_, err := db.Exec(`UPDATE identities SET badge = $1 WHERE user_id = $2`, badge, userID)
	return err
}

// IsModerator returns true if the user is in the moderator_roles table.
func IsModerator(userID string) bool {
	var exists bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM moderator_roles WHERE user_id = $1)`, userID).Scan(&exists)
	return exists
}

// ── Moderation-feature toggle helpers ─────────────────────────────────────────

// IsModerationEnabled reads the moderation_enabled flag from server_config.
// Returns true if the key is absent (safe default) or set to 'true'.
func IsModerationEnabled() bool {
	var val string
	err := db.QueryRow(`SELECT value FROM server_config WHERE key = 'moderation_enabled'`).Scan(&val)
	if err == sql.ErrNoRows {
		return true // default: moderation on
	}
	if err != nil {
		log.Printf("IsModerationEnabled: db error: %v", err)
		return true
	}
	return val == "true"
}

// setModerationEnabled persists the moderation toggle to server_config.
func setModerationEnabled(enabled bool, updatedBy string) error {
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := db.Exec(`
		INSERT INTO server_config (key, value, updated_by, updated_at)
		VALUES ('moderation_enabled', $1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_by = $2, updated_at = NOW()
	`, val, updatedBy)
	return err
}

// ── HTTP Handlers ──────────────────────────────────────────────────────────────

// POST /admin/users/assign-badge
// Body: { "user_id": "alice@server_a", "badge": "moderator" }
// Admin-only. Also syncs the moderator_roles table for badge == "moderator".
func AssignBadgeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Identify the admin performing the action.
	authHeader := r.Header.Get("Authorization")
	authParts := strings.Split(authHeader, " ")
	adminActorID := "system"
	if len(authParts) == 2 && authParts[0] == "Bearer" {
		if adminClaims, err := ValidateUserToken(authParts[1]); err == nil {
			adminActorID = ToInternalID(adminClaims.UserID)
		}
	}

	var req struct {
		UserID string `json:"user_id"`
		Badge  string `json:"badge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" || req.Badge == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and badge are required")
		return
	}

	internalUserID := ToInternalID(req.UserID)

	// Verify the user exists
	var username string
	err := db.QueryRow(`
		SELECT user_id FROM identities WHERE user_id = $1
	`, internalUserID).Scan(&username)
	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	if err := SetUserBadge(internalUserID, req.Badge); err != nil {
		log.Printf("AssignBadgeHandler: SetUserBadge: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to set badge")
		return
	}

	// Notify the user about their new badge — actor is the admin who granted it.
	notifMsg := "Your account badge has been updated to: " + req.Badge
	_ = CreateNotification(internalUserID, adminActorID, "BADGE_GRANTED", notifMsg)

	go LogAdminAction(ToExternalID(adminActorID), "BADGE_ASSIGNED", ToExternalID(internalUserID), "badge="+req.Badge)

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "badge assigned",
		"user_id": ToExternalID(internalUserID),
		"badge":   req.Badge,
	})
}

// GET /admin/moderation/status  (admin-only)
func GetModerationStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	enabled := IsModerationEnabled()
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"moderation_enabled": enabled,
	})
}

// POST /admin/moderation/toggle  (admin-only)
// Body: { "enabled": true|false }
// Disabling preserves all moderator_roles rows – they are NOT deleted.
// Re-enabling immediately restores the full moderation pipeline.
func ToggleModerationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	by := "admin"
	if len(parts) == 2 {
		if c, err := ValidateJWT(parts[1]); err == nil {
			by = c.Username
		}
	}

	if err := setModerationEnabled(req.Enabled, by); err != nil {
		log.Printf("ToggleModerationHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to update moderation status")
		return
	}

	status := "disabled"
	if req.Enabled {
		status = "enabled"
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message":            "moderation feature " + status,
		"moderation_enabled": status,
	})
}

// ── Server permission helpers ──────────────────────────────────────────────────

// knownPermissions lists all toggleable feature flags stored in server_config.
var knownPermissions = []string{
	"allow_ephemeral_posts",
	"allow_image_uploads",
	"allow_direct_messages",
	"allow_reposts",
	"allow_replies",
}

// GetPermission reads a boolean feature flag from server_config. Defaults to true when absent.
func GetPermission(key string) bool {
	var val string
	err := db.QueryRow(`SELECT value FROM server_config WHERE key = $1`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		log.Printf("GetPermission(%s): db error: %v", key, err)
		return true
	}
	return val != "false"
}

func setPermission(key string, enabled bool, updatedBy string) error {
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := db.Exec(`
		INSERT INTO server_config (key, value, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_by = $3, updated_at = NOW()
	`, key, val, updatedBy)
	return err
}

// GET /admin/settings/permissions
func GetPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result := map[string]bool{}
	for _, k := range knownPermissions {
		result[k] = GetPermission(k)
	}
	RespondWithJSON(w, http.StatusOK, result)
}

// POST /admin/settings/permissions
// Body: { "allow_ephemeral_posts": true, "allow_image_uploads": false, ... }
func UpdatePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req map[string]bool
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	by := "admin"
	if len(parts) == 2 {
		if c, err := ValidateJWT(parts[1]); err == nil {
			by = c.Username
		}
	}

	for _, k := range knownPermissions {
		if val, ok := req[k]; ok {
			if err := setPermission(k, val, by); err != nil {
				log.Printf("UpdatePermissionsHandler: error setting %s: %v", k, err)
				RespondWithError(w, http.StatusInternalServerError, "failed to update permissions")
				return
			}
		}
	}

	// Return the full updated state
	result := map[string]bool{}
	for _, k := range knownPermissions {
		result[k] = GetPermission(k)
	}
	RespondWithJSON(w, http.StatusOK, result)
}

// ── Admin activity logging ─────────────────────────────────────────────────────

// LogAdminAction inserts a record into the admin_logs table.
// Called fire-and-forget (errors are only logged, not propagated).
func LogAdminAction(actor, action, targetID, detail string) {
	_, err := db.Exec(
		`INSERT INTO admin_logs (actor, action, target_id, detail) VALUES ($1, $2, $3, $4)`,
		actor, action, targetID, detail,
	)
	if err != nil {
		log.Printf("LogAdminAction: %v", err)
	}
}

// ── Badge revoke ───────────────────────────────────────────────────────────────

// POST /admin/users/revoke-badge
// Body: { "user_id": "alice@server_a" }
// Resets the user's badge back to the default "user" value.
func RevokeBadgeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	authHeader := r.Header.Get("Authorization")
	authParts := strings.Split(authHeader, " ")
	adminActorID := "admin"
	if len(authParts) == 2 && authParts[0] == "Bearer" {
		if claims, err := ValidateUserToken(authParts[1]); err == nil {
			adminActorID = ToInternalID(claims.UserID)
		}
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	internalUserID := ToInternalID(req.UserID)

	var existing string
	err := db.QueryRow(`SELECT user_id FROM identities WHERE user_id = $1`, internalUserID).Scan(&existing)
	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	if err := SetUserBadge(internalUserID, "user"); err != nil {
		log.Printf("RevokeBadgeHandler: SetUserBadge: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to revoke badge")
		return
	}

	notifMsg := "Your account badge has been revoked."
	_ = CreateNotification(internalUserID, adminActorID, "BADGE_GRANTED", notifMsg)

	go LogAdminAction(ToExternalID(adminActorID), "BADGE_REVOKED", ToExternalID(internalUserID), "badge revoked → user")

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "badge revoked",
		"user_id": ToExternalID(internalUserID),
	})
}

// ── Admin logs endpoint ────────────────────────────────────────────────────────

// AdminLogEntry is the JSON shape returned to the frontend.
type AdminLogEntry struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	TargetID  string    `json:"target_id"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// GET /admin/logs
// Query params: limit (default 50, max 200), offset, actor (optional filter).
func GetAdminLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()

	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}

	offset := 0
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	actor := q.Get("actor")

	var (
		rows *sql.Rows
		err  error
	)
	if actor != "" {
		rows, err = db.Query(
			`SELECT id, actor, action, target_id, detail, created_at
			 FROM admin_logs WHERE actor ILIKE $1
			 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			"%"+actor+"%", limit, offset,
		)
	} else {
		rows, err = db.Query(
			`SELECT id, actor, action, target_id, detail, created_at
			 FROM admin_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			limit, offset,
		)
	}
	if err != nil {
		log.Printf("GetAdminLogsHandler: query: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	entries := []AdminLogEntry{}
	for rows.Next() {
		var e AdminLogEntry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.TargetID, &e.Detail, &e.CreatedAt); err != nil {
			log.Printf("GetAdminLogsHandler: scan: %v", err)
			continue
		}
		entries = append(entries, e)
	}

	var total int
	if actor != "" {
		db.QueryRow(`SELECT COUNT(*) FROM admin_logs WHERE actor ILIKE $1`, "%"+actor+"%").Scan(&total)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM admin_logs`).Scan(&total)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  entries,
		"count": total,
	})
}
