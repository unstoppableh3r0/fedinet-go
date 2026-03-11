package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

func AdminAuthMiddleware(next http.Handler) http.Handler {
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

		tokenString := parts[1]

		claims, err := ValidateJWT(tokenString)
		if err != nil {
			RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		if !claims.IsAdmin {
			RespondWithError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req AdminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if !ValidateAdminCredentials(req.Username, req.Password) {
		RespondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := GenerateJWT(req.Username)
	if err != nil {
		log.Println("Failed to generate JWT:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"token":   token,
		"message": "login successful",
	})
}

func GetServerConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	config, err := GetServerConfig()
	if err != nil {
		log.Println("Failed to get server config:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	RespondWithJSON(w, http.StatusOK, config)
}

func UpdateServerConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ServerName string `json:"server_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.ServerName == "" {
		RespondWithError(w, http.StatusBadRequest, "server_name is required")
		return
	}

	authHeader := r.Header.Get("Authorization")
	tokenString := strings.Split(authHeader, " ")[1]
	claims, _ := ValidateJWT(tokenString)

	err := UpdateServerName(req.ServerName, claims.Username)
	if err != nil {
		log.Println("Failed to update server name:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to update server name")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message":     "server name updated successfully",
		"server_name": req.ServerName,
	})
}

// notifyAllUsersServerRenamed sends a SERVER_UPDATE notification to every local user.
func notifyAllUsersServerRenamed(newName string) {
	rows, err := db.Query(`SELECT user_id FROM identities`)
	if err != nil {
		log.Printf("notifyAllUsersServerRenamed: query error: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		// actor_id "system" is a sentinel – no real user
		if err := CreateNotification(uid, "system", "SYSTEM", "Server renamed to: "+newName); err != nil {
			log.Printf("notifyAllUsersServerRenamed: failed for %s: %v", uid, err)
		}
	}
}

func TestDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ConnectionString string `json:"connection_string"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.ConnectionString == "" {
		RespondWithError(w, http.StatusBadRequest, "connection_string is required")
		return
	}

	err := TestDatabaseConnection(req.ConnectionString)
	if err != nil {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{
			"status":  "failed",
			"message": err.Error(),
		})
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "database connection successful",
	})
}

func StartMigrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req MigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.NewConnectionString == "" {
		RespondWithError(w, http.StatusBadRequest, "new_connection_string is required")
		return
	}

	err := TestDatabaseConnection(req.NewConnectionString)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "cannot connect to new database: "+err.Error())
		return
	}

	migrationID, err := MigrateDatabase(req.NewConnectionString)
	if err != nil {
		log.Println("Failed to start migration:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to start migration")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"migration_id": migrationID,
		"status":       "started",
		"message":      "database migration started",
	})
}

func GetMigrationStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	migrationID := r.URL.Query().Get("migration_id")
	if migrationID == "" {
		RespondWithError(w, http.StatusBadRequest, "migration_id parameter required")
		return
	}

	status, err := GetMigrationStatus(migrationID)
	if err != nil {
		log.Println("Failed to get migration status:", err)
		RespondWithError(w, http.StatusNotFound, "migration not found")
		return
	}

	RespondWithJSON(w, http.StatusOK, status)
}

func GetAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	users, err := GetAllUsers()
	if err != nil {
		log.Println("Failed to get users:", err)
		// Return empty list instead of 500 so the dashboard still loads
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"users": []interface{}{},
			"count": 0,
		})
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"count": len(users),
	})
}

func GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := GetServerStats()
	if err != nil {
		log.Println("Failed to get stats:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve statistics")
		return
	}

	RespondWithJSON(w, http.StatusOK, stats)
}

// ── Invite admin handlers ──────────────────────────────────────────────────

// GET /admin/invites/list
func ListInvitesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	invites, err := ListInvites()
	if err != nil {
		log.Println("Failed to list invites:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve invites")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"invites": invites,
	})
}

// POST /admin/invites/generate
func GenerateInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req GenerateInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Extract creator username from JWT
	createdBy := "admin"
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) == 2 {
		if claims, err := ValidateJWT(parts[1]); err == nil {
			createdBy = claims.Username
		}
	}

	invite, err := GenerateInvite(req, createdBy)
	if err != nil {
		log.Println("Failed to generate invite:", err)
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, invite)
}

// POST /admin/invites/revoke   body: {"invite_code":"..."}
func RevokeInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.InviteCode == "" {
		RespondWithError(w, http.StatusBadRequest, "invite_code is required")
		return
	}

	if err := RevokeInvite(req.InviteCode); err != nil {
		log.Println("Failed to revoke invite:", err)
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "invite revoked"})
}

// GET /admin/invites/qr?code=<invite_code>
// Returns a PNG QR image.
func GetInviteQRHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		RespondWithError(w, http.StatusBadRequest, "code query parameter is required")
		return
	}

	png, err := GenerateInviteQR(code)
	if err != nil {
		log.Println("Failed to generate invite QR:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate QR code")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// ── Admin dashboard snapshots ─────────────────────────────────────────────────

// GetSnapshotsHandler returns historical server stat snapshots for trend charts.
// GET /admin/snapshots
func GetSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT id, total_users, total_posts, total_follows, captured_at
		FROM server_snapshots
		ORDER BY captured_at DESC
		LIMIT 30
	`)
	if err != nil {
		log.Printf("GetSnapshotsHandler: query error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch snapshots")
		return
	}
	defer rows.Close()

	type Snapshot struct {
		ID           string `json:"id"`
		TotalUsers   int    `json:"total_users"`
		TotalPosts   int    `json:"total_posts"`
		TotalFollows int    `json:"total_follows"`
		CapturedAt   string `json:"captured_at"`
	}

	var snapshots []Snapshot
	for rows.Next() {
		var s Snapshot
		if err := rows.Scan(&s.ID, &s.TotalUsers, &s.TotalPosts, &s.TotalFollows, &s.CapturedAt); err != nil {
			log.Printf("GetSnapshotsHandler: scan error: %v", err)
			continue
		}
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

// SaveSnapshotHandler captures a point-in-time stat snapshot.
// POST /admin/snapshots
func SaveSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var totalUsers, totalPosts, totalFollows int
	db.QueryRow(`SELECT COUNT(*) FROM identities`).Scan(&totalUsers)
	db.QueryRow(`SELECT COUNT(*) FROM posts WHERE visibility = 'PUBLIC'`).Scan(&totalPosts)
	db.QueryRow(`SELECT COUNT(*) FROM follows`).Scan(&totalFollows)

	var id string
	err := db.QueryRow(`
		INSERT INTO server_snapshots (total_users, total_posts, total_follows)
		VALUES ($1, $2, $3)
		RETURNING id
	`, totalUsers, totalPosts, totalFollows).Scan(&id)
	if err != nil {
		log.Printf("SaveSnapshotHandler: insert error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to save snapshot")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":            id,
		"total_users":   totalUsers,
		"total_posts":   totalPosts,
		"total_follows": totalFollows,
	})
}

// ── Admin account graph ───────────────────────────────────────────────────────

// GetAccountLinksHandler returns follower/following graph edges for the admin view.
// GET /admin/account/links
func GetAccountLinksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT follower_user_id, followee_user_id
		FROM follows
		LIMIT 500
	`)
	if err != nil {
		log.Printf("GetAccountLinksHandler: query error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch account links")
		return
	}
	defer rows.Close()

	type Link struct {
		From string `json:"from"`
		To   string `json:"to"`
	}

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.From, &l.To); err != nil {
			continue
		}
		l.From = ToExternalID(l.From)
		l.To = ToExternalID(l.To)
		links = append(links, l)
	}

	if links == nil {
		links = []Link{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"links": links,
		"count": len(links),
	})
}
