package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type PrivacySettings struct {
	UserID                  string    `json:"user_id"`
	SearchLocal             string    `json:"search_local"`
	SearchFederated         string    `json:"search_federated"`
	PostsVisibility         string    `json:"posts_visibility"`
	LikesVisibility         string    `json:"likes_visibility"`
	RepliesVisibility       string    `json:"replies_visibility"`
	FollowingListVisibility string    `json:"following_list_visibility"`
	FollowersListVisibility string    `json:"followers_list_visibility"`
	DisableResharing        bool      `json:"disable_resharing"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func defaultPrivacySettings(userID string) PrivacySettings {
	now := time.Now()
	return PrivacySettings{
		UserID:                  userID,
		SearchLocal:             "everyone",
		SearchFederated:         "everyone",
		PostsVisibility:         "public",
		LikesVisibility:         "public",
		RepliesVisibility:       "public",
		FollowingListVisibility: "public",
		FollowersListVisibility: "public",
		DisableResharing:        false,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
}

// getPrivacySettingsForUser loads privacy settings for any user by ID,
// returning defaults if no row exists yet.
func getPrivacySettingsForUser(userID string) PrivacySettings {
	var s PrivacySettings
	err := db.QueryRow(`
		SELECT user_id, search_local, search_federated,
		       posts_visibility, likes_visibility, replies_visibility,
		       following_list_visibility, followers_list_visibility,
		       COALESCE(disable_resharing, false),
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`, userID).Scan(
		&s.UserID, &s.SearchLocal, &s.SearchFederated,
		&s.PostsVisibility, &s.LikesVisibility, &s.RepliesVisibility,
		&s.FollowingListVisibility, &s.FollowersListVisibility,
		&s.DisableResharing,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return defaultPrivacySettings(userID)
	}
	return s
}

// canViewContent returns true if viewerID is allowed to see content governed
// by the given visibility setting that belongs to targetID.
// visibility values: "public" / "everyone" / "followers" / "private"
func canViewContent(visibility, viewerID, targetID string) bool {
	if viewerID == targetID {
		return true
	}
	switch visibility {
	case "public", "everyone":
		return true
	case "followers":
		var count int
		db.QueryRow(
			`SELECT COUNT(*) FROM follows WHERE follower_user_id = $1 AND followee_user_id = $2`,
			viewerID, targetID,
		).Scan(&count)
		return count > 0
	case "private":
		return false
	default:
		return true
	}
}

// PrivacySettingsHandler handles GET and POST /privacy/settings
// Requires a valid user JWT in Authorization: Bearer <token>
func PrivacySettingsHandler(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		RespondWithError(w, http.StatusUnauthorized, "missing or invalid authorization header")
		return
	}
	claims, err := ValidateUserToken(parts[1])
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	userID := claims.UserID

	switch r.Method {
	case http.MethodGet:
		var s PrivacySettings
		err := db.QueryRow(`
			SELECT user_id, search_local, search_federated,
			       posts_visibility, likes_visibility, replies_visibility,
			       following_list_visibility, followers_list_visibility,
			       COALESCE(disable_resharing, false),
			       created_at, updated_at
			FROM privacy_settings
			WHERE user_id = $1
		`, userID).Scan(
			&s.UserID, &s.SearchLocal, &s.SearchFederated,
			&s.PostsVisibility, &s.LikesVisibility, &s.RepliesVisibility,
			&s.FollowingListVisibility, &s.FollowersListVisibility,
			&s.DisableResharing,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			// No row yet — return defaults
			RespondWithJSON(w, http.StatusOK, defaultPrivacySettings(userID))
			return
		}
		RespondWithJSON(w, http.StatusOK, s)

	case http.MethodPost:
		var req PrivacySettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondWithError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		// Ensure caller can only update their own settings
		req.UserID = userID

		_, err := db.Exec(`
			INSERT INTO privacy_settings (
				user_id, search_local, search_federated,
				posts_visibility, likes_visibility, replies_visibility,
				following_list_visibility, followers_list_visibility,
				disable_resharing,
				created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
			ON CONFLICT (user_id) DO UPDATE SET
				search_local              = EXCLUDED.search_local,
				search_federated          = EXCLUDED.search_federated,
				posts_visibility          = EXCLUDED.posts_visibility,
				likes_visibility          = EXCLUDED.likes_visibility,
				replies_visibility        = EXCLUDED.replies_visibility,
				following_list_visibility = EXCLUDED.following_list_visibility,
				followers_list_visibility = EXCLUDED.followers_list_visibility,
				disable_resharing         = EXCLUDED.disable_resharing,
				updated_at                = NOW()
		`,
			req.UserID, req.SearchLocal, req.SearchFederated,
			req.PostsVisibility, req.LikesVisibility, req.RepliesVisibility,
			req.FollowingListVisibility, req.FollowersListVisibility,
			req.DisableResharing,
		)
		if err != nil {
			log.Printf("PrivacySettingsHandler save: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to save privacy settings")
			return
		}

		// Audit log — record that the user changed their privacy settings.
		detail, _ := json.Marshal(req)
		LogPrivacyEvent("SETTINGS_CHANGED", userID, "", string(detail), clientIP(r))

		// Return the saved settings
		var s PrivacySettings
		db.QueryRow(`
			SELECT user_id, search_local, search_federated,
			       posts_visibility, likes_visibility, replies_visibility,
			       following_list_visibility, followers_list_visibility,
			       COALESCE(disable_resharing, false),
			       created_at, updated_at
			FROM privacy_settings WHERE user_id = $1
		`, userID).Scan(
			&s.UserID, &s.SearchLocal, &s.SearchFederated,
			&s.PostsVisibility, &s.LikesVisibility, &s.RepliesVisibility,
			&s.FollowingListVisibility, &s.FollowersListVisibility,
			&s.DisableResharing,
			&s.CreatedAt, &s.UpdatedAt,
		)
		RespondWithJSON(w, http.StatusOK, s)

	default:
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ── Privacy audit log ─────────────────────────────────────────────────────────

// LogPrivacyEvent persists a privacy-related audit event.
// eventType: e.g. "SETTINGS_CHANGED", "USER_BLOCKED", "USER_UNBLOCKED",
//            "PROFILE_EXPORTED", "PROFILE_IMPORTED"
// detail:    JSON-encoded context (what changed, reason, etc.)
// ipAddr:    caller's IP from clientIP(r)
func LogPrivacyEvent(eventType, actorID, targetID, detail, ipAddr string) {
	if detail == "" {
		detail = "{}"
	}
	_, err := db.Exec(`
		INSERT INTO privacy_audit_logs (event_type, actor_id, target_id, detail, ip_addr)
		VALUES ($1, $2, $3, $4, $5)
	`, eventType, actorID, targetID, detail, ipAddr)
	if err != nil {
		log.Printf("LogPrivacyEvent(%s): %v", eventType, err)
	}
}

// PrivacyAuditLogEntry is the shape returned to the admin.
type PrivacyAuditLogEntry struct {
	ID        string    `json:"id"`
	EventType string    `json:"event_type"`
	ActorID   string    `json:"actor_id"`
	TargetID  string    `json:"target_id"`
	Detail    string    `json:"detail"`
	IPAddr    string    `json:"ip_addr"`
	CreatedAt time.Time `json:"created_at"`
}

// GetPrivacyLogsHandler handles GET /admin/privacy/logs
// Query params: limit (default 50, max 200), offset (default 0), actor_id (optional filter)
// Requires admin JWT.
func GetPrivacyLogsHandler(w http.ResponseWriter, r *http.Request) {
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

	baseQuery := `
		SELECT id, event_type, actor_id, target_id, detail, ip_addr, created_at
		FROM privacy_audit_logs`

	actorFilter := q.Get("actor_id")
	var args []interface{}
	if actorFilter != "" {
		baseQuery += ` WHERE actor_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{ToInternalID(actorFilter), limit, offset}
	} else {
		baseQuery += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	var entries []PrivacyAuditLogEntry
	for rows.Next() {
		var e PrivacyAuditLogEntry
		if err := rows.Scan(&e.ID, &e.EventType, &e.ActorID, &e.TargetID, &e.Detail, &e.IPAddr, &e.CreatedAt); err != nil {
			RespondWithError(w, http.StatusInternalServerError, "scan error")
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "rows error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   entries,
		"limit":  limit,
		"offset": offset,
		"count":  len(entries),
	})
}

