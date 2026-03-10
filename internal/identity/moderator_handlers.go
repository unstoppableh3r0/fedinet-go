package identity

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Moderator JWT generation ───────────────────────────────────────────────────

func generateModeratorJWT(username string) (string, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", nil // caller will catch empty string
	}
	claims := AdminClaims{
		Username:    username,
		IsAdmin:     false,
		IsModerator: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-backend",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// ── ModeratorAuthMiddleware ────────────────────────────────────────────────────
// Accepts tokens issued to admins (IsAdmin=true) OR moderators (IsModerator=true).

func ModeratorAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			RespondWithError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			RespondWithError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		claims, err := ValidateJWT(parts[1])
		if err != nil {
			RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		if !claims.IsAdmin && !claims.IsModerator {
			RespondWithError(w, http.StatusForbidden, "moderator or admin access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ── ModeratorLoginHandler ──────────────────────────────────────────────────────
// POST /moderator/login
// Body: { "username": "...", "password": "..." }
// Validates credentials against identities table, then confirms moderator_roles membership.

func ModeratorLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		RespondWithError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	// Look up password hash from identities (user_id is "username@server")
	var passwordHash string
	var userID string
	err := db.QueryRow(`
		SELECT user_id, password_hash
		FROM identities
		WHERE user_id LIKE $1 || '@%'
		   OR user_id = $1
		LIMIT 1
	`, req.Username).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		log.Printf("ModeratorLoginHandler: db error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if !CheckPasswordHash(req.Password, passwordHash) {
		RespondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Confirm this user is in moderator_roles
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM moderator_roles WHERE user_id = $1`, userID).Scan(&count); err != nil {
		log.Printf("ModeratorLoginHandler: moderator check error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if count == 0 {
		RespondWithError(w, http.StatusForbidden, "user is not a moderator")
		return
	}

	token, err := generateModeratorJWT(req.Username)
	if err != nil || token == "" {
		log.Printf("ModeratorLoginHandler: JWT generation failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"token":   token,
		"message": "moderator login successful",
		"user_id": userID,
	})
}

// ── Moderator role management ──────────────────────────────────────────────────

// POST /admin/moderators/assign
// Body: { "username": "alice" }  (user_id is resolved automatically)
func AssignModeratorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		UserID   string `json:"user_id"` // optional override
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" {
		RespondWithError(w, http.StatusBadRequest, "username is required")
		return
	}

	// Resolve full user_id if not explicitly supplied
	userID := req.UserID
	if userID == "" {
		err := db.QueryRow(`
			SELECT user_id FROM identities
			WHERE user_id LIKE $1 || '@%'
			   OR user_id = $1
			LIMIT 1
		`, req.Username).Scan(&userID)
		if err == sql.ErrNoRows {
			RespondWithError(w, http.StatusNotFound, "user not found – they must register before being made a moderator")
			return
		}
		if err != nil {
			log.Printf("AssignModeratorHandler: user lookup: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "database error")
			return
		}
	}

	// Extract assigning admin from token
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	assignedBy := "admin"
	if len(parts) == 2 {
		if c, err := ValidateJWT(parts[1]); err == nil {
			assignedBy = c.Username
		}
	}

	_, err := db.Exec(`
		INSERT INTO moderator_roles (user_id, username, assigned_by, assigned_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE SET username = $2, assigned_by = $3, assigned_at = NOW()
	`, userID, req.Username, assignedBy)
	if err != nil {
		log.Printf("AssignModeratorHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to assign moderator role")
		return
	}

	// Upgrade badge to 'moderator' (don't downgrade an 'admin' badge)
	if current := GetUserBadge(userID); current != "admin" {
		if err := SetUserBadge(userID, "moderator"); err != nil {
			log.Printf("AssignModeratorHandler: SetUserBadge: %v", err)
		}
	}
	_ = CreateNotification(userID, "system", "BADGE_GRANTED", "You have been granted the moderator badge")

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "moderator role assigned",
		"user_id": userID,
	})
}

// POST /admin/moderators/remove
// Body: { "username": "alice" }  OR  { "user_id": "alice@server_a" }
func RemoveModeratorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" && req.Username == "" {
		RespondWithError(w, http.StatusBadRequest, "username or user_id is required")
		return
	}

	var result sql.Result
	var err error
	if req.UserID != "" {
		result, err = db.Exec(`DELETE FROM moderator_roles WHERE user_id = $1`, req.UserID)
	} else {
		result, err = db.Exec(`DELETE FROM moderator_roles WHERE username = $1`, req.Username)
	}
	if err != nil {
		log.Printf("RemoveModeratorHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to remove moderator role")
		return
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		RespondWithError(w, http.StatusNotFound, "user is not a moderator")
		return
	}

	// Revert badge to 'user' (only if it was 'moderator'; preserve 'admin' badge)
	targetID := req.UserID
	if targetID == "" {
		// Resolve from username
		_ = db.QueryRow(`SELECT user_id FROM identities WHERE user_id LIKE $1 || '@%' OR user_id = $1 LIMIT 1`, req.Username).Scan(&targetID)
	}
	if targetID != "" {
		if GetUserBadge(targetID) == "moderator" {
			if err := SetUserBadge(targetID, "user"); err != nil {
				log.Printf("RemoveModeratorHandler: SetUserBadge: %v", err)
			}
		}
		_ = CreateNotification(targetID, "system", "BADGE_GRANTED", "Your moderator badge has been removed. Your badge is now: user")
	}

	identifier := req.UserID
	if identifier == "" {
		identifier = req.Username
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "moderator role removed",
		"user_id": identifier,
	})
}

// GET /admin/moderators/list
func ListModeratorsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT user_id, username, assigned_by, assigned_at
		FROM moderator_roles
		ORDER BY assigned_at DESC
	`)
	if err != nil {
		log.Printf("ListModeratorsHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to list moderators")
		return
	}
	defer rows.Close()

	type ModeratorEntry struct {
		UserID     string    `json:"user_id"`
		Username   string    `json:"username"`
		AssignedBy string    `json:"assigned_by"`
		AssignedAt time.Time `json:"assigned_at"`
	}

	var moderators []ModeratorEntry
	for rows.Next() {
		var m ModeratorEntry
		if err := rows.Scan(&m.UserID, &m.Username, &m.AssignedBy, &m.AssignedAt); err != nil {
			log.Printf("ListModeratorsHandler scan: %v", err)
			continue
		}
		moderators = append(moderators, m)
	}

	if moderators == nil {
		moderators = []ModeratorEntry{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"moderators": moderators,
		"count":      len(moderators),
	})
}

// ── Stats snapshots ────────────────────────────────────────────────────────────

// GET /admin/snapshots?limit=30
func GetSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	limit := 30
	rows, err := db.Query(`
		SELECT total_users, total_posts, total_activities, total_follows, created_at
		FROM server_stats_snapshots
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		log.Printf("GetSnapshotsHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve snapshots")
		return
	}
	defer rows.Close()

	// Use field names that match the admin frontend's AdminSnapshot interface.
	type Snapshot struct {
		TS         int64 `json:"ts"`
		Users      int   `json:"users"`
		Posts      int   `json:"posts"`
		Activities int   `json:"activities"`
		Follows    int   `json:"follows"`
	}

	var snapshots []Snapshot
	for rows.Next() {
		var s Snapshot
		var createdAt time.Time
		if err := rows.Scan(&s.Users, &s.Posts, &s.Activities, &s.Follows, &createdAt); err != nil {
			log.Printf("GetSnapshotsHandler scan: %v", err)
			continue
		}
		s.TS = createdAt.UnixMilli()
		snapshots = append(snapshots, s)
	}

	if snapshots == nil {
		snapshots = []Snapshot{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"snapshots": snapshots,
		"count":     len(snapshots),
	})
}

// POST /admin/snapshots — save a stats snapshot.
// The request body may contain {ts, users, posts, activities, follows} (from the
// frontend seed). If the body is empty or unparseable the handler falls back to
// computing live counts from the database.
func SaveSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TS         int64 `json:"ts"`
		Users      int   `json:"users"`
		Posts      int   `json:"posts"`
		Activities int   `json:"activities"`
		Follows    int   `json:"follows"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // ignore parse errors — fall through to DB compute

	// If the body carried seed data (ts != 0), store it directly so the
	// trend chart has a meaningful history from first boot.
	if body.TS != 0 {
		snappedAt := time.UnixMilli(body.TS)
		var id int64
		if err := db.QueryRow(`
			INSERT INTO server_stats_snapshots
				(total_users, total_posts, total_activities, total_follows, created_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`, body.Users, body.Posts, body.Activities, body.Follows, snappedAt).Scan(&id); err != nil {
			log.Printf("SaveSnapshotHandler (seed): %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to save snapshot")
			return
		}
		RespondWithJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "message": "snapshot saved"})
		return
	}

	// No seed body — compute live from DB.
	stats, err := GetServerStats()
	if err != nil {
		log.Printf("SaveSnapshotHandler: GetServerStats: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to gather stats")
		return
	}

	var id int64
	err = db.QueryRow(`
		INSERT INTO server_stats_snapshots
			(total_users, total_posts, total_activities, total_follows, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id
	`, stats.TotalUsers, stats.TotalPosts, stats.TotalActivities, stats.TotalFollows).Scan(&id)
	if err != nil {
		log.Printf("SaveSnapshotHandler: insert: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to save snapshot")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      id,
		"message": "snapshot saved",
	})
}

// ── Account link graph ─────────────────────────────────────────────────────────

// GET /admin/account/links
// Returns up to 500 follow-relationship edges for visualising the social graph.
func GetAccountLinksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")

	var rows *sql.Rows
	var err error
	if userID != "" {
		rows, err = db.Query(`
			SELECT follower_user_id, followee_user_id, created_at
			FROM follows
			WHERE follower_user_id = $1 OR followee_user_id = $1
			ORDER BY created_at DESC
			LIMIT 500
		`, userID)
	} else {
		rows, err = db.Query(`
			SELECT follower_user_id, followee_user_id, created_at
			FROM follows
			ORDER BY created_at DESC
			LIMIT 500
		`)
	}
	if err != nil {
		log.Printf("GetAccountLinksHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve account links")
		return
	}
	defer rows.Close()

	type Link struct {
		ID          string    `json:"id"`
		RequesterID string    `json:"requester_id"`
		TargetID    string    `json:"target_id"`
		Status      string    `json:"status"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	var links []Link
	for rows.Next() {
		var l Link
		var createdAt time.Time
		if err := rows.Scan(&l.RequesterID, &l.TargetID, &createdAt); err != nil {
			log.Printf("GetAccountLinksHandler scan: %v", err)
			continue
		}
		l.ID = l.RequesterID + ":" + l.TargetID
		l.Status = "confirmed"
		l.CreatedAt = createdAt
		l.UpdatedAt = createdAt
		links = append(links, l)
	}

	if links == nil {
		links = []Link{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"links":   links,
		"count":   len(links),
	})
}

// ── Pending posts (moderator review queue) ────────────────────────────────────

// GET /moderation/pending
// Lists posts with visibility = 'PENDING_REVIEW' or 'HIDDEN' for moderator review.
func GetPendingPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT id, author, content, COALESCE(image_url, ''), visibility, created_at
		FROM posts
		WHERE visibility IN ('PENDING_REVIEW', 'HIDDEN')
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("GetPendingPostsHandler: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve pending posts")
		return
	}
	defer rows.Close()

	type PendingPost struct {
		ID         string    `json:"id"`
		Author     string    `json:"author"`
		Content    string    `json:"content"`
		ImageURL   string    `json:"image_url,omitempty"`
		Visibility string    `json:"visibility"`
		CreatedAt  time.Time `json:"created_at"`
	}

	var posts []PendingPost
	for rows.Next() {
		var p PendingPost
		if err := rows.Scan(&p.ID, &p.Author, &p.Content, &p.ImageURL, &p.Visibility, &p.CreatedAt); err != nil {
			log.Printf("GetPendingPostsHandler scan: %v", err)
			continue
		}
		posts = append(posts, p)
	}

	if posts == nil {
		posts = []PendingPost{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
		"count": len(posts),
	})
}
