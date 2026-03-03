package identity

import (
	"database/sql"
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
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve users")
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

// AdminSnapshot is a single growth-trend data point persisted in Postgres.
type AdminSnapshot struct {
	TS         int64 `json:"ts"`
	Users      int   `json:"users"`
	Posts      int   `json:"posts"`
	Activities int   `json:"activities"`
	Follows    int   `json:"follows"`
}

const maxAdminSnapshots = 60

// GetAdminSnapshotsHandler returns the stored admin dashboard trend snapshots.
//
//	GET /admin/snapshots
func GetAdminSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT ts, users, posts, activities, follows
		FROM admin_snapshots
		ORDER BY ts ASC
		LIMIT $1
	`, maxAdminSnapshots)
	if err != nil {
		log.Printf("GetAdminSnapshotsHandler: query error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch snapshots")
		return
	}
	defer rows.Close()

	snaps := []AdminSnapshot{}
	for rows.Next() {
		var s AdminSnapshot
		if err := rows.Scan(&s.TS, &s.Users, &s.Posts, &s.Activities, &s.Follows); err != nil {
			continue
		}
		snaps = append(snaps, s)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"snapshots": snaps})
}

// SaveAdminSnapshotHandler upserts a single snapshot into the DB.
// If a snapshot taken within the last 5 minutes already exists it is updated in place;
// otherwise a new row is appended. Rows beyond maxAdminSnapshots are pruned.
//
//	POST /admin/snapshots
func SaveAdminSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var snap AdminSnapshot
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if snap.TS == 0 {
		RespondWithError(w, http.StatusBadRequest, "ts is required")
		return
	}

	fiveMinMs := int64(5 * 60 * 1000)
	// Check for a recent snapshot we should update instead of inserting
	var existingID int64
	err := db.QueryRow(`
		SELECT id FROM admin_snapshots
		WHERE $1 - ts < $2
		ORDER BY ts DESC LIMIT 1
	`, snap.TS, fiveMinMs).Scan(&existingID)

	if err == nil {
		// Update the recent snapshot
		_, err = db.Exec(`
			UPDATE admin_snapshots
			SET ts=$1, users=$2, posts=$3, activities=$4, follows=$5
			WHERE id=$6
		`, snap.TS, snap.Users, snap.Posts, snap.Activities, snap.Follows, existingID)
	} else {
		// Insert a new row
		_, err = db.Exec(`
			INSERT INTO admin_snapshots (ts, users, posts, activities, follows)
			VALUES ($1, $2, $3, $4, $5)
		`, snap.TS, snap.Users, snap.Posts, snap.Activities, snap.Follows)
	}

	if err != nil {
		log.Printf("SaveAdminSnapshotHandler: write error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to save snapshot")
		return
	}

	// Prune oldest rows if we exceed the cap
	db.Exec(`
		DELETE FROM admin_snapshots
		WHERE id NOT IN (
			SELECT id FROM admin_snapshots ORDER BY ts DESC LIMIT $1
		)
	`, maxAdminSnapshots)

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Admin: account link graph ─────────────────────────────────────

// GET /admin/account/links?user_id=alice@server_a
// Returns the full link graph for a given user_id (admin-only).
// If user_id is omitted, returns ALL account links on the server.
func AdminGetAccountLinksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rawUserID := r.URL.Query().Get("user_id")

	var rows *sql.Rows
	var err error

	if rawUserID == "" {
		rows, err = db.Query(`
			SELECT id, requester_id, target_id, status, created_at, updated_at
			FROM account_links
			ORDER BY updated_at DESC
		`)
	} else {
		userID := ToInternalID(rawUserID)
		rows, err = db.Query(`
			SELECT id, requester_id, target_id, status, created_at, updated_at
			FROM account_links
			WHERE requester_id=$1 OR target_id=$1
			ORDER BY updated_at DESC
		`, userID)
	}
	if err != nil {
		log.Printf("AdminGetAccountLinks: query error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to query links")
		return
	}
	defer rows.Close()

	type linkRow struct {
		ID          string `json:"id"`
		RequesterID string `json:"requester_id"`
		TargetID    string `json:"target_id"`
		Status      string `json:"status"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	var links []linkRow

	for rows.Next() {
		var l AccountLink
		if err := rows.Scan(&l.ID, &l.RequesterID, &l.TargetID, &l.Status, &l.CreatedAt, &l.UpdatedAt); err != nil {
			continue
		}
		links = append(links, linkRow{
			ID:          l.ID,
			RequesterID: ToExternalID(l.RequesterID),
			TargetID:    ToExternalID(l.TargetID),
			Status:      l.Status,
			CreatedAt:   l.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   l.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	if links == nil {
		links = []linkRow{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"user_id": rawUserID,
		"links":   links,
		"count":   len(links),
	})
}
