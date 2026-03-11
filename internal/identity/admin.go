package identity

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================================
// DATA STRUCTURES & MODELS
// ============================================================================

// AdminClaims represents the structure of the JWT (JSON Web Token) payload.
// It includes custom fields for identity management and the standard registered claims.
// This is used to verify that a request is coming from a legitimate administrator.
type AdminClaims struct {
	Username             string `json:"username"` // The unique identifier for the administrator.
	IsAdmin              bool   `json:"is_admin"` // A boolean flag used for secondary authorization checks.
	jwt.RegisteredClaims        // Standard fields like 'exp', 'iat', and 'iss'.
}

// AdminLoginRequest is the schema for incoming authentication requests.
// It maps directly to the JSON body sent by the frontend during login.
type AdminLoginRequest struct {
	Username string `json:"username"` // Plaintext username provided by the user.
	Password string `json:"password"` // Plaintext password provided by the user.
}

// ServerConfig stores metadata about the instance itself.
// This is useful for multi-server environments where identifying the node is necessary.
type ServerConfig struct {
	ServerName string    `json:"server_name"` // Human-readable name (e.g., "North America Node").
	UpdatedAt  time.Time `json:"updated_at"`  // The last time the configuration was modified.
	UpdatedBy  string    `json:"updated_by"`  // The identity of the admin who made the change.
}

// MigrationRequest is a simple wrapper for database migration instructions.
// It contains the target DSN (Data Source Name) for the new database instance.
type MigrationRequest struct {
	NewConnectionString string `json:"new_connection_string"`
}

// MigrationStatus tracks the lifecycle of a database migration event.
// Because migrations are asynchronous, this allows the UI to poll for progress updates.
type MigrationStatus struct {
	ID             string                 `json:"id"`              // Unique UUID for this migration task.
	FromDB         string                 `json:"from_db"`         // The source database connection string.
	ToDB           string                 `json:"to_db"`           // The destination database connection string.
	Status         string                 `json:"status"`          // Current state: 'pending', 'in_progress', 'completed', 'failed'.
	TablesMigrated map[string]interface{} `json:"tables_migrated"` // A map tracking success/failure for individual tables.
	ErrorMessage   *string                `json:"error_message"`   // Detailed error description if the migration fails.
	StartedAt      time.Time              `json:"started_at"`      // Timestamp when the migration was initialized.
	CompletedAt    *time.Time             `json:"completed_at"`    // Timestamp when the migration reached a terminal state.
}

// ServerStats aggregates high-level metrics for the administrator dashboard.
// This provides a "birds-eye view" of the health and activity of the server.
type ServerStats struct {
	TotalUsers      int    `json:"total_users"`      // Total count of identities in the system.
	TotalPosts      int    `json:"total_posts"`      // Cumulative number of posts across all users.
	TotalActivities int    `json:"total_activities"` // Count of federation activities (likes, follows, etc.).
	TotalFollows    int    `json:"total_follows"`    // Number of social connections established.
	ServerName      string `json:"server_name"`      // The name of the server being monitored.
	DatabaseStatus  string `json:"database_status"`  // Indicates if the database is currently reachable (connected/disconnected).
	Uptime          string `json:"uptime"`           // Duration since the service was last restarted.
}

// Notification represents a system-generated alert for an end user.
// In this package, it is primarily used for administrative broadcasts (e.g., maintenance alerts).
type Notification struct {
	ID        string    `json:"id"`         // Unique identifier for the notification record.
	UserID    string    `json:"user_id"`    // The recipient of the notification.
	Title     string    `json:"title"`      // Short summary of the notification (e.g., "Update").
	Message   string    `json:"message"`    // The full body content of the notification.
	Type      string    `json:"type"`       // Categorization (e.g., "system", "social", "alert").
	IsRead    bool      `json:"is_read"`    // Status flag to track if the user has seen the message.
	CreatedAt time.Time `json:"created_at"` // Timestamp of notification creation.
}

// ============================================================================
// AUTHENTICATION & SECURITY LOGIC
// ============================================================================

// ValidateAdminCredentials handles the multi-layered authentication logic for admins.
// It prioritizes environment variables for container-native security (Docker/K8s)
// but falls back to the database for dynamically managed administrative accounts.
func ValidateAdminCredentials(username, password string) bool {
	// Stage 1: Check Environment Variables (Hardcoded/Infrastructure level credentials).
	// This allows system admins to regain access if the database is corrupted or locked.
	adminUser := os.Getenv("ADMIN_USERNAME")
	adminPass := os.Getenv("ADMIN_PASSWORD")

	if adminUser != "" && adminPass != "" {
		// Strict equality check for infrastructure-level credentials.
		if username == adminUser && password == adminPass {
			return true
		}
		// Security Logic: If env-vars are present but do not match the input, we stop here.
		// This prevents "shadowing" where an attacker might try to use a DB account
		// that conflicts with a higher-priority environment-based account.
		log.Println("Admin login: env credentials set but don't match")
	}

	// Stage 2: Database Lookup (Dynamically created admin accounts).
	// We look for a bcrypt-hashed password in the 'admins' table.
	var passwordHash string
	err := db.QueryRow(
		"SELECT password_hash FROM admins WHERE username = $1 LIMIT 1",
		username,
	).Scan(&passwordHash)

	// If the user doesn't exist or the query fails, authentication fails.
	if err != nil {
		log.Printf("ValidateAdminCredentials: DB lookup failed for %q: %v", username, err)
		return false
	}

	// Stage 3: Hash Comparison.
	// Use Bcrypt to compare the plaintext input with the stored hash to prevent timing attacks.
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		log.Printf("ValidateAdminCredentials: wrong password for %q", username)
		return false
	}

	// All checks passed.
	return true
}

// GenerateJWT creates a new authentication token for an administrator.
// It signs the token using a secret key defined in the system's environment variables.
func GenerateJWT(username string) (string, error) {
	// Retrieve the secret key. If missing, we cannot sign tokens, which is a fatal error.
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", fmt.Errorf("JWT_SECRET not set in environment")
	}

	// Configure the token payload.
	// Tokens are set to expire in 24 hours to balance convenience and security.
	claims := AdminClaims{
		Username: username,
		IsAdmin:  true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-backend",
		},
	}

	// Initialize the token object using the HS256 algorithm (HMAC with SHA-256).
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign the token string using the provided secret key.
	return token.SignedString([]byte(jwtSecret))
}

// ValidateJWT parses an incoming token string and verifies its cryptographic signature.
// It also checks for expiration and proper claim structure.
func ValidateJWT(tokenString string) (*AdminClaims, error) {
	// Secret key is required to verify the signature.
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET not set in environment")
	}

	// Parse the token with the custom AdminClaims structure.
	token, err := jwt.ParseWithClaims(tokenString, &AdminClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Security check: Ensure the algorithm used to sign the token is HMAC.
		// This prevents "alg: none" attacks or RSA-to-HMAC confusion attacks.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	// If parsing fails (signature mismatch, expired, malformed), return the error.
	if err != nil {
		return nil, err
	}

	// Validate that the claims can be correctly cast to our struct and that the token is logically valid.
	if claims, ok := token.Claims.(*AdminClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ============================================================================
// SERVER CONFIGURATION & IDENTITY
// ============================================================================

// GetServerConfig fetches the server's identity settings from the database.
// It handles initialization logic by falling back to environment variables if no DB record exists.
func GetServerConfig() (*ServerConfig, error) {
	var config ServerConfig

	// Query for the specific 'server_name' key in the configuration table.
	err := db.QueryRow(`
        SELECT value, updated_at, COALESCE(updated_by, 'system')
        FROM server_config
        WHERE key = 'server_name'
    `).Scan(&config.ServerName, &config.UpdatedAt, &config.UpdatedBy)

	// Case: First run or missing configuration.
	if err == sql.ErrNoRows {
		// Use environment variables or a default value for the initial state.
		config.ServerName = os.Getenv("SERVER_NAME")
		if config.ServerName == "" {
			config.ServerName = "localhost"
		}
		config.UpdatedAt = time.Now()
		config.UpdatedBy = "system"
		return &config, nil
	}

	if err != nil {
		return nil, err
	}

	return &config, nil
}

// SeedServerConfig is an idempotent function called during application startup.
// It ensures that essential server identity records exist and match the current environment.
// This bridges the gap between static environment variables and persistent database state.
func SeedServerConfig() {
	// Determine the preferred server name from multiple potential environment sources.
	serverName := os.Getenv("SERVER_NAME")
	if serverName == "" {
		serverName = os.Getenv("SERVER_ID") // Fallback to ID if name is missing.
	}
	if serverName == "" {
		serverName = "FediNet Server" // Ultimate fallback.
	}

	// Step 1: Seed the general server configuration.
	// Uses an 'UPSERT' pattern: if the key exists, update it only if it was previously empty or generic.
	// This preserves custom admin changes while ensuring new servers are correctly named.
	_, err := db.Exec(`
        INSERT INTO server_config (key, value, updated_by, updated_at)
        VALUES ('server_name', $1, 'system', NOW())
        ON CONFLICT (key) DO UPDATE
        SET value = CASE
            WHEN server_config.value = '' OR server_config.value = 'FediNet Server'
            THEN EXCLUDED.value
            ELSE server_config.value
        END,
        updated_at = NOW()
    `, serverName)

	if err != nil {
		log.Printf("SeedServerConfig: failed to seed server_config: %v", err)
	} else {
		log.Printf("SeedServerConfig: server_name set to %q", serverName)
	}

	// Step 2: Seed the server_identity table.
	// This table is vital for federation and QR code discovery.
	serverID := os.Getenv("SERVER_ID")
	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Similar UPSERT logic for the identity record.
	_, err = db.Exec(`
        INSERT INTO server_identity (id, server_id, server_name, public_key, endpoint)
        VALUES (1, $1, $2, '', $3)
        ON CONFLICT (id) DO UPDATE
        SET server_id   = CASE WHEN server_identity.server_id   = '' THEN EXCLUDED.server_id   ELSE server_identity.server_id END,
            server_name = CASE WHEN server_identity.server_name = '' OR server_identity.server_name = 'FediNet Server'
                               THEN EXCLUDED.server_name ELSE server_identity.server_name END,
            endpoint    = CASE WHEN server_identity.endpoint    = '' THEN EXCLUDED.endpoint    ELSE server_identity.endpoint END,
            updated_at  = NOW()
    `, serverID, serverName, serverURL)

	if err != nil {
		log.Printf("SeedServerConfig: failed to seed server_identity: %v", err)
	}
}

// UpdateServerName allows an administrator to rename the server instance.
// This triggers a global notification to inform all users of the change.
func UpdateServerName(newName, updatedBy string) error {
	// Perform the database update.
	_, err := db.Exec(`
        INSERT INTO server_config (key, value, updated_by, updated_at)
        VALUES ('server_name', $1, $2, NOW())
        ON CONFLICT (key) DO UPDATE
        SET value = $1, updated_by = $2, updated_at = NOW()
    `, newName, updatedBy)

	if err != nil {
		return fmt.Errorf("failed to update server config: %v", err)
	}

	// UX Optimization: Notify users asynchronously.
	// We launch a goroutine so the API response isn't blocked by the notification broadcast.
	go func() {
		if err := NotifyAllUsers(
			"Server Name Updated",
			fmt.Sprintf("Server name changed to: %s", newName),
			"server_change",
		); err != nil {
			// Notification failures are logged but don't cause the primary action to fail.
			log.Printf("UpdateServerName: notification broadcast failed (non-fatal): %v", err)
		}
	}()

	return nil
}

// ============================================================================
// NOTIFICATION BROADCAST SYSTEM
// ============================================================================

// NotifyAllUsers wraps the notification logic in a single atomic transaction.
// This ensures that either every user gets the notification, or none do (if a failure occurs).
func NotifyAllUsers(title, message, notifType string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Defer a rollback in case of error; Commit will nullify the rollback if successful.
	defer tx.Rollback()

	err = NotifyAllUsersInTx(tx, title, message, notifType)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// NotifyAllUsersInTx performs a batch insertion into the notifications table.
// It selects all user IDs from the 'identities' table and generates a corresponding record.
func NotifyAllUsersInTx(tx *sql.Tx, title, message, notifType string) error {
	// The query uses a SELECT within an INSERT to handle bulk processing efficiently at the DB level.
	// recipient_id: target user.
	// actor_id: 'admin' (the source).
	// type/entity_id/activity_stream: details for rendering on the client side.
	_, err := tx.Exec(`
        INSERT INTO notifications (recipient_id, actor_id, type, entity_id, activity_stream, created_at)
        SELECT user_id, 'admin', $1, $2, $3, NOW()
        FROM identities
        ON CONFLICT DO NOTHING
    `, notifType, title, []byte(message))

	return err
}

// ============================================================================
// SYSTEM METRICS & USER DISCOVERY
// ============================================================================

// GetServerStats collects usage data from across the system.
// It is designed to be "resilient," meaning it will continue to return partial data
// even if certain tables or features are currently unavailable.
func GetServerStats() (*ServerStats, error) {
	stats := &ServerStats{}

	// Individual count queries. Errors are ignored to prevent a single missing table
	// from crashing the entire Dashboard view.
	db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&stats.TotalUsers)
	db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&stats.TotalPosts)
	db.QueryRow("SELECT COUNT(*) FROM follows").Scan(&stats.TotalFollows)

	// Legacy Support: Check for 'activity_log' first, fallback to 'outbox_activities'.
	// This maintains compatibility during schema migrations.
	if err := db.QueryRow("SELECT COUNT(*) FROM activity_log").Scan(&stats.TotalActivities); err != nil {
		db.QueryRow("SELECT COUNT(*) FROM outbox_activities").Scan(&stats.TotalActivities)
	}

	// Retrieve server name for the UI.
	config, err := GetServerConfig()
	if err != nil {
		stats.ServerName = os.Getenv("SERVER_NAME")
		if stats.ServerName == "" {
			stats.ServerName = "FediNet Server"
		}
	} else {
		stats.ServerName = config.ServerName
	}

	// Health Check: Verify if the database pool is active.
	if err := db.Ping(); err != nil {
		stats.DatabaseStatus = "disconnected"
	} else {
		stats.DatabaseStatus = "connected"
	}

	// Placeholder for uptime logic (to be implemented).
	stats.Uptime = "N/A"

	return stats, nil
}

// GetAllUsers retrieves the complete set of registered users and their profile data.
// It performs a LEFT JOIN between identities and profiles to ensure users without a profile are still listed.
func GetAllUsers() ([]models.UserDocument, error) {
	rows, err := db.Query(`
        SELECT
            i.id, i.user_id, i.home_server, i.public_key, i.allow_discovery, i.created_at, i.updated_at,
            p.user_id, p.display_name, p.avatar_url, p.banner_url, p.bio, p.portfolio_url,
            p.birth_date, p.location, p.followers_visibility, p.following_visibility,
            p.created_at, p.updated_at
        FROM identities i
        LEFT JOIN profiles p ON i.user_id = p.user_id
        ORDER BY i.created_at DESC
    `)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.UserDocument
	for rows.Next() {
		var doc models.UserDocument
		// Scan the flat SQL row into the structured UserDocument object.
		err := rows.Scan(
			&doc.Identity.ID, &doc.Identity.UserID, &doc.Identity.HomeServer,
			&doc.Identity.PublicKey, &doc.Identity.AllowDiscovery,
			&doc.Identity.CreatedAt, &doc.Identity.UpdatedAt,
			&doc.Profile.UserID, &doc.Profile.DisplayName,
			&doc.Profile.AvatarURL, &doc.Profile.BannerURL, &doc.Profile.Bio,
			&doc.Profile.PortfolioURL, &doc.Profile.BirthDate, &doc.Profile.Location,
			&doc.Profile.FollowersVisibility, &doc.Profile.FollowingVisibility,
			&doc.Profile.CreatedAt, &doc.Profile.UpdatedAt,
		)
		if err != nil {
			log.Println("Error scanning user:", err)
			continue // Skip corrupted rows to continue processing the rest.
		}
		users = append(users, doc)
	}

	return users, nil
}

// GetSuggestedUsers finds public users for discovery features (e.g., "People to Follow").
// It filters for users who have explicitly opted into the discovery service.
func GetSuggestedUsers(limit int) ([]models.UserDocument, error) {
	rows, err := db.Query(`
        SELECT
            i.id, i.user_id, i.home_server, i.public_key, i.allow_discovery, i.created_at, i.updated_at,
            p.user_id, p.display_name, p.avatar_url, p.banner_url, p.bio, p.portfolio_url,
            p.birth_date, p.location, p.followers_visibility, p.following_visibility,
            p.created_at, p.updated_at
        FROM identities i
        LEFT JOIN profiles p ON i.user_id = p.user_id
        WHERE i.allow_discovery = true
        ORDER BY i.created_at DESC
        LIMIT $1
    `, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.UserDocument
	for rows.Next() {
		var doc models.UserDocument
		err := rows.Scan(
			&doc.Identity.ID, &doc.Identity.UserID, &doc.Identity.HomeServer,
			&doc.Identity.PublicKey, &doc.Identity.AllowDiscovery,
			&doc.Identity.CreatedAt, &doc.Identity.UpdatedAt,
			&doc.Profile.UserID, &doc.Profile.DisplayName,
			&doc.Profile.AvatarURL, &doc.Profile.BannerURL, &doc.Profile.Bio,
			&doc.Profile.PortfolioURL, &doc.Profile.BirthDate, &doc.Profile.Location,
			&doc.Profile.FollowersVisibility, &doc.Profile.FollowingVisibility,
			&doc.Profile.CreatedAt, &doc.Profile.UpdatedAt,
		)
		if err != nil {
			log.Println("Error scanning user:", err)
			continue
		}
		users = append(users, doc)
	}

	return users, nil
}

// ============================================================================
// DATABASE MIGRATION ENGINE
// ============================================================================

// TestDatabaseConnection verifies that a potential migration target is valid and reachable.
// This is used as a pre-flight check before starting expensive data movement.
func TestDatabaseConnection(connectionString string) error {
	testDB, err := sql.Open("postgres", connectionString)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer testDB.Close()

	// Ping is required because sql.Open does not actually establish a connection immediately.
	err = testDB.Ping()
	if err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// CreateSchemaOnNewDB initializes the structure of the new database instance.
// It creates all necessary tables, extensions, and triggers required for operation.
func CreateSchemaOnNewDB(newDB *sql.DB) error {
	schema := `
        -- Pgcrypto is used for generating UUIDs.
        CREATE EXTENSION IF NOT EXISTS pgcrypto;

        -- Central function for updating 'updated_at' columns on row changes.
        CREATE OR REPLACE FUNCTION set_updated_at()
        RETURNS TRIGGER AS $$
        BEGIN
          NEW.updated_at = now();
          RETURN NEW;
        END;
        $$ LANGUAGE plpgsql;

        -- Core table for user accounts.
        CREATE TABLE identities (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          did TEXT,
          user_id TEXT NOT NULL UNIQUE,
          home_server TEXT NOT NULL,
          public_key TEXT NOT NULL,
          allow_discovery BOOLEAN DEFAULT true,
          created_at TIMESTAMP DEFAULT now(),
          updated_at TIMESTAMP DEFAULT now()
        );

        -- Personal information associated with an identity.
        CREATE TABLE profiles (
          user_id TEXT PRIMARY KEY,
          display_name TEXT NOT NULL,
          avatar_url TEXT,
          banner_url TEXT,
          bio TEXT,
          portfolio_url TEXT,
          birth_date DATE,
          location TEXT,
          followers_visibility TEXT DEFAULT 'public',
          following_visibility TEXT DEFAULT 'public',
          created_at TIMESTAMP DEFAULT now(),
          updated_at TIMESTAMP DEFAULT now(),
          CONSTRAINT profiles_user_id_fkey
            FOREIGN KEY (user_id)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- User-generated content.
        CREATE TABLE posts (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          author TEXT NOT NULL,
          content TEXT NOT NULL,
          created_at TIMESTAMP DEFAULT now(),
          updated_at TIMESTAMP DEFAULT now(),
          CONSTRAINT posts_author_fkey
            FOREIGN KEY (author)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- Log of federated actions (e.g., likes, boosts).
        CREATE TABLE activities (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          actor_id TEXT NOT NULL,
          verb TEXT NOT NULL,
          object_type TEXT,
          object_id TEXT,
          target_id TEXT,
          payload JSONB,
          created_at TIMESTAMP DEFAULT now(),
          CONSTRAINT activities_actor_id_fkey
            FOREIGN KEY (actor_id)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- Social graph tracking.
        CREATE TABLE follows (
          follower_user_id TEXT NOT NULL,
          follower_home_server TEXT NOT NULL,
          followee_user_id TEXT NOT NULL,
          followee_home_server TEXT NOT NULL,
          created_at TIMESTAMP DEFAULT now(),
          updated_at TIMESTAMP DEFAULT now(),
          PRIMARY KEY (follower_user_id, followee_user_id),
          CONSTRAINT follows_follower_user_id_fkey
            FOREIGN KEY (follower_user_id)
            REFERENCES identities(user_id)
            ON DELETE CASCADE,
          CONSTRAINT follows_followee_user_id_fkey
            FOREIGN KEY (followee_user_id)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- Internal private communication.
        CREATE TABLE messages (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          sender TEXT NOT NULL,
          receiver TEXT NOT NULL,
          content TEXT NOT NULL,
          created_at TIMESTAMP DEFAULT now(),
          CONSTRAINT messages_sender_fkey
            FOREIGN KEY (sender)
            REFERENCES identities(user_id)
            ON DELETE CASCADE,
          CONSTRAINT messages_receiver_fkey
            FOREIGN KEY (receiver)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- Key-value store for server parameters.
        CREATE TABLE server_config (
          key TEXT PRIMARY KEY,
          value TEXT NOT NULL,
          updated_at TIMESTAMP DEFAULT NOW(),
          updated_by TEXT
        );

        -- User alerts and notifications.
        CREATE TABLE notifications (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          user_id TEXT NOT NULL,
          title TEXT NOT NULL,
          message TEXT NOT NULL,
          type TEXT NOT NULL,
          is_read BOOLEAN DEFAULT FALSE,
          created_at TIMESTAMP DEFAULT NOW(),
          CONSTRAINT notifications_user_id_fkey
            FOREIGN KEY (user_id)
            REFERENCES identities(user_id)
            ON DELETE CASCADE
        );

        -- Record to track migrations on the target DB.
        CREATE TABLE migration_status (
          id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
          from_db TEXT NOT NULL,
          to_db TEXT NOT NULL,
          status TEXT NOT NULL,
          tables_migrated JSONB,
          error_message TEXT,
          started_at TIMESTAMP DEFAULT NOW(),
          completed_at TIMESTAMP
        );

        -- Wire up the triggers for timestamp maintenance.
        CREATE TRIGGER profiles_updated_at_trigger
        BEFORE UPDATE ON profiles
        FOR EACH ROW
        EXECUTE FUNCTION set_updated_at();

        CREATE TRIGGER posts_updated_at_trigger
        BEFORE UPDATE ON posts
        FOR EACH ROW
        EXECUTE FUNCTION set_updated_at();

        CREATE TRIGGER follows_updated_at_trigger
        BEFORE UPDATE ON follows
        FOR EACH ROW
        EXECUTE FUNCTION set_updated_at();

        -- Optimization: Indexes for fast notification retrieval.
        CREATE INDEX idx_notifications_user_id ON notifications(user_id);
        CREATE INDEX idx_notifications_is_read ON notifications(is_read);
    `

	_, err := newDB.Exec(schema)
	return err
}

// MigrateDatabase is the entry point for the migration process.
// It logs the request and hands off the actual work to a background worker.
func MigrateDatabase(newConnectionString string) (string, error) {
	// Generate a unique ID to identify this specific migration task.
	migrationID := uuid.New().String()
	currentDB := os.Getenv("DATABASE_URL")

	// Record the start of the process in the current database.
	_, err := db.Exec(`
        INSERT INTO migration_status (id, from_db, to_db, status)
        VALUES ($1, $2, $3, 'pending')
    `, migrationID, currentDB, newConnectionString)

	if err != nil {
		return "", fmt.Errorf("failed to create migration record: %w", err)
	}

	// Execution Logic: Database migrations are slow and can time out HTTP requests.
	// We launch it in a separate goroutine so the admin gets an instant confirmation.
	go performMigration(migrationID, currentDB, newConnectionString)

	return migrationID, nil
}

// performMigration is the background worker that manages the data copy process.
// It connects to both databases and transfers data table by table.
func performMigration(migrationID, fromDB, toDB string) {
	// Mark as in-progress.
	db.Exec(`UPDATE migration_status SET status = 'in_progress' WHERE id = $1`, migrationID)

	// Open connection to the destination.
	newDB, err := sql.Open("postgres", toDB)
	if err != nil {
		recordMigrationError(migrationID, fmt.Sprintf("Failed to connect to new database: %v", err))
		return
	}
	defer newDB.Close()

	// Initialize the destination schema.
	err = CreateSchemaOnNewDB(newDB)
	if err != nil {
		recordMigrationError(migrationID, fmt.Sprintf("Failed to create schema: %v", err))
		return
	}

	// List of tables to migrate. ORDER MATTERS due to Foreign Key constraints.
	// Identities must come before Profiles, for example.
	tables := []string{"identities", "profiles", "posts", "activities", "follows", "messages", "server_config", "notifications"}
	tableStatus := make(map[string]interface{})

	for _, table := range tables {
		// Execute the copy for each table.
		err = copyTableData(db, newDB, table)
		if err != nil {
			tableStatus[table] = "failed"
			recordMigrationError(migrationID, fmt.Sprintf("Failed to migrate table %s: %v", table, err))
			return
		}
		tableStatus[table] = "success"

		// Update progress in the current database.
		statusJSON, _ := json.Marshal(tableStatus)
		db.Exec(`UPDATE migration_status SET tables_migrated = $1 WHERE id = $2`, statusJSON, migrationID)
	}

	// Mark migration as successful.
	statusJSON, _ := json.Marshal(tableStatus)
	db.Exec(`
        UPDATE migration_status
        SET status = 'completed', tables_migrated = $1, completed_at = NOW()
        WHERE id = $2
    `, statusJSON, migrationID)

	log.Printf("Migration %s completed successfully", migrationID)
}

// copyTableData implements a generic "Select All -> Insert Into" logic.
// It dynamically discovers columns to avoid hardcoding fields, making it resilient to small schema changes.
func copyTableData(fromDB, toDB *sql.DB, tableName string) error {
	// Read all data from source table.
	rows, err := fromDB.Query(fmt.Sprintf("SELECT * FROM %s", tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	// Introspection: Get column names dynamically.
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Build the SQL placeholders (e.g., "$1, $2, $3").
	placeholders := ""
	for i := range columns {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
	}

	// Build the bulk insertion string.
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		joinColumns(columns),
		placeholders)

	// Row-by-row transfer.
	for rows.Next() {
		// Create a slice of interfaces to hold any type of column data.
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the source data into our interface slice.
		err = rows.Scan(valuePtrs...)
		if err != nil {
			return err
		}

		// Execute the insert on the target database.
		_, err = toDB.Exec(insertSQL, values...)
		if err != nil {
			return err
		}
	}

	return rows.Err()
}

// joinColumns is a utility to format column names for SQL strings.
func joinColumns(columns []string) string {
	result := ""
	for i, col := range columns {
		if i > 0 {
			result += ", "
		}
		result += col
	}
	return result
}

// recordMigrationError persists the error message so admins can debug why a migration failed.
func recordMigrationError(migrationID, errorMsg string) {
	db.Exec(`
        UPDATE migration_status
        SET status = 'failed', error_message = $1, completed_at = NOW()
        WHERE id = $2
    `, errorMsg, migrationID)
	log.Printf("Migration %s failed: %s", migrationID, errorMsg)
}

// GetMigrationStatus retrieves the state of a specific migration.
// It handles JSON deserialization for the 'tables_migrated' field.
func GetMigrationStatus(migrationID string) (*MigrationStatus, error) {
	var status MigrationStatus
	var tablesJSON []byte

	err := db.QueryRow(`
        SELECT id, from_db, to_db, status,
               COALESCE(tables_migrated, '{}'::jsonb),
               error_message, started_at, completed_at
        FROM migration_status
        WHERE id = $1
    `, migrationID).Scan(
		&status.ID, &status.FromDB, &status.ToDB, &status.Status,
		&tablesJSON, &status.ErrorMessage, &status.StartedAt, &status.CompletedAt,
	)

	if err != nil {
		return nil, err
	}

	// Convert the database JSONB back into a Go map.
	if len(tablesJSON) > 0 {
		json.Unmarshal(tablesJSON, &status.TablesMigrated)
	}

	return &status, nil
}
