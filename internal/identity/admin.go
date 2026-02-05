package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Admin JWT Claims
type AdminClaims struct {
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// Admin login credentials
type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Server configuration
type ServerConfig struct {
	ServerName string    `json:"server_name"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
}

// Database migration request
type MigrationRequest struct {
	NewConnectionString string `json:"new_connection_string"`
}

// Migration status
type MigrationStatus struct {
	ID             string                 `json:"id"`
	FromDB         string                 `json:"from_db"`
	ToDB           string                 `json:"to_db"`
	Status         string                 `json:"status"`
	TablesMigrated map[string]interface{} `json:"tables_migrated"`
	ErrorMessage   *string                `json:"error_message"`
	StartedAt      time.Time              `json:"started_at"`
	CompletedAt    *time.Time             `json:"completed_at"`
}

// Server statistics
type ServerStats struct {
	TotalUsers      int    `json:"total_users"`
	TotalPosts      int    `json:"total_posts"`
	TotalActivities int    `json:"total_activities"`
	TotalFollows    int    `json:"total_follows"`
	ServerName      string `json:"server_name"`
	DatabaseStatus  string `json:"database_status"`
	Uptime          string `json:"uptime"`
}

// Notification model
type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// ============ Authentication Functions ============

// ValidateAdminCredentials checks if the provided credentials are valid
func ValidateAdminCredentials(username, password string) bool {
	adminUser := os.Getenv("ADMIN_USERNAME")
	adminPass := os.Getenv("ADMIN_PASSWORD")

	if adminUser == "" || adminPass == "" {
		log.Println("Warning: Admin credentials not set in environment")
		return false
	}

	return username == adminUser && password == adminPass
}

// GenerateJWT creates a new JWT token for admin
func GenerateJWT(username string) (string, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", fmt.Errorf("JWT_SECRET not set in environment")
	}

	claims := AdminClaims{
		Username: username,
		IsAdmin:  true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "federated-backend",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// ValidateJWT validates a JWT token and returns the claims
func ValidateJWT(tokenString string) (*AdminClaims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET not set in environment")
	}

	token, err := jwt.ParseWithClaims(tokenString, &AdminClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*AdminClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// ============ Server Configuration Functions ============

// GetServerConfig retrieves the current server configuration
func GetServerConfig() (*ServerConfig, error) {
	var config ServerConfig

	err := db.QueryRow(`
		SELECT value, updated_at, COALESCE(updated_by, 'system') 
		FROM server_config 
		WHERE key = 'server_name'
	`).Scan(&config.ServerName, &config.UpdatedAt, &config.UpdatedBy)

	if err == sql.ErrNoRows {
		// Return default from environment
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

// UpdateServerName updates the server name and notifies all users
// UpdateServerName updates the server name and notifies all users
func UpdateServerName(newName, updatedBy string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update server config
	_, err = tx.Exec(`
		INSERT INTO server_config (key, value, updated_by, updated_at)
		VALUES ('server_name', $1, $2, NOW())
		ON CONFLICT (key) DO UPDATE 
		SET value = $1, updated_by = $2, updated_at = NOW()
	`, newName, updatedBy)

	if err != nil {
		return fmt.Errorf("failed to update server config: %v", err)
	}

	// Notify all users about server name change
	err = NotifyAllUsersInTx(tx, "Server Name Updated",
		fmt.Sprintf("The server name has been changed to: %s. Your username is now username@%s", newName, newName),
		"server_change")

	if err != nil {
		return err
	}

	return tx.Commit()
}

// NotifyAllUsers creates a notification for all users
func NotifyAllUsers(title, message, notifType string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = NotifyAllUsersInTx(tx, title, message, notifType)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// NotifyAllUsersInTx creates notifications within a transaction
func NotifyAllUsersInTx(tx *sql.Tx, title, message, notifType string) error {
	_, err := tx.Exec(`
		INSERT INTO notifications (user_id, title, message, type, is_read, created_at)
		SELECT user_id, $1, $2, $3, false, NOW()
		FROM identities
	`, title, message, notifType)

	return err
}

// GetServerStats retrieves server statistics
func GetServerStats() (*ServerStats, error) {
	stats := &ServerStats{}

	// Get user count
	err := db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&stats.TotalUsers)
	if err != nil {
		return nil, err
	}

	// Get post count
	err = db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&stats.TotalPosts)
	if err != nil {
		return nil, err
	}

	// Get activity count
	err = db.QueryRow("SELECT COUNT(*) FROM activities").Scan(&stats.TotalActivities)
	if err != nil {
		return nil, err
	}

	// Get follows count
	err = db.QueryRow("SELECT COUNT(*) FROM follows").Scan(&stats.TotalFollows)
	if err != nil {
		return nil, err
	}

	// Get server name
	config, err := GetServerConfig()
	if err != nil {
		stats.ServerName = "unknown"
	} else {
		stats.ServerName = config.ServerName
	}

	// Test database connection
	err = db.Ping()
	if err != nil {
		stats.DatabaseStatus = "disconnected"
	} else {
		stats.DatabaseStatus = "connected"
	}

	stats.Uptime = "N/A" // TODO: Implement uptime tracking

	return stats, nil
}

// GetAllUsers retrieves all users with basic info
func GetAllUsers() ([]UserDocument, error) {
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

	var users []UserDocument
	for rows.Next() {
		var doc UserDocument
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

// ============ Database Migration Functions ============

// TestDatabaseConnection tests if a database connection string is valid
func TestDatabaseConnection(connectionString string) error {
	testDB, err := sql.Open("postgres", connectionString)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer testDB.Close()

	err = testDB.Ping()
	if err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// CreateSchemaOnNewDB creates all necessary tables on a new database
func CreateSchemaOnNewDB(newDB *sql.DB) error {
	schema := `
		-- Enable extensions
		CREATE EXTENSION IF NOT EXISTS pgcrypto;
		
		-- Helper function
		CREATE OR REPLACE FUNCTION set_updated_at()
		RETURNS TRIGGER AS $$
		BEGIN
		  NEW.updated_at = now();
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		
		-- Identities table
		CREATE TABLE identities (
		  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		  user_id TEXT NOT NULL UNIQUE,
		  home_server TEXT NOT NULL,
		  public_key TEXT NOT NULL,
		  allow_discovery BOOLEAN DEFAULT true,
		  created_at TIMESTAMP DEFAULT now(),
		  updated_at TIMESTAMP DEFAULT now()
		);
		
		-- Profiles table
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
		
		-- Posts table
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
		
		-- Activities table
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
		
		-- Follows table
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
		
		-- Messages table
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
		
		-- Server config table
		CREATE TABLE server_config (
		  key TEXT PRIMARY KEY,
		  value TEXT NOT NULL,
		  updated_at TIMESTAMP DEFAULT NOW(),
		  updated_by TEXT
		);
		
		-- Notifications table
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
		
		-- Migration status table
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
		
		-- Triggers
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
		
		-- Indexes
		CREATE INDEX idx_notifications_user_id ON notifications(user_id);
		CREATE INDEX idx_notifications_is_read ON notifications(is_read);
	`

	_, err := newDB.Exec(schema)
	return err
}

// MigrateDatabase performs the complete database migration
func MigrateDatabase(newConnectionString string) (string, error) {
	// Create migration record
	migrationID := uuid.New().String()
	currentDB := os.Getenv("DATABASE_URL")

	_, err := db.Exec(`
		INSERT INTO migration_status (id, from_db, to_db, status)
		VALUES ($1, $2, $3, 'pending')
	`, migrationID, currentDB, newConnectionString)

	if err != nil {
		return "", fmt.Errorf("failed to create migration record: %w", err)
	}

	// This should be run as a background job in production
	go performMigration(migrationID, currentDB, newConnectionString)

	return migrationID, nil
}

// performMigration executes the migration process
func performMigration(migrationID, fromDB, toDB string) {
	// Update status to in_progress
	db.Exec(`UPDATE migration_status SET status = 'in_progress' WHERE id = $1`, migrationID)

	// Open connection to new database
	newDB, err := sql.Open("postgres", toDB)
	if err != nil {
		recordMigrationError(migrationID, fmt.Sprintf("Failed to connect to new database: %v", err))
		return
	}
	defer newDB.Close()

	// Create schema on new database
	err = CreateSchemaOnNewDB(newDB)
	if err != nil {
		recordMigrationError(migrationID, fmt.Sprintf("Failed to create schema: %v", err))
		return
	}

	// Migrate data from each table
	tables := []string{"identities", "profiles", "posts", "activities", "follows", "messages", "server_config", "notifications"}
	tableStatus := make(map[string]interface{})

	for _, table := range tables {
		err = copyTableData(db, newDB, table)
		if err != nil {
			tableStatus[table] = "failed"
			recordMigrationError(migrationID, fmt.Sprintf("Failed to migrate table %s: %v", table, err))
			return
		}
		tableStatus[table] = "success"

		// Update progress
		statusJSON, _ := json.Marshal(tableStatus)
		db.Exec(`UPDATE migration_status SET tables_migrated = $1 WHERE id = $2`, statusJSON, migrationID)
	}

	// Mark migration as completed
	statusJSON, _ := json.Marshal(tableStatus)
	db.Exec(`
		UPDATE migration_status 
		SET status = 'completed', tables_migrated = $1, completed_at = NOW() 
		WHERE id = $2
	`, statusJSON, migrationID)

	log.Printf("Migration %s completed successfully", migrationID)
}

// copyTableData copies all data from one table to another
func copyTableData(fromDB, toDB *sql.DB, tableName string) error {
	// Get all data from source table
	rows, err := fromDB.Query(fmt.Sprintf("SELECT * FROM %s", tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Prepare insert statement
	placeholders := ""
	for i := range columns {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		joinColumns(columns),
		placeholders)

	// Copy each row
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		err = rows.Scan(valuePtrs...)
		if err != nil {
			return err
		}

		_, err = toDB.Exec(insertSQL, values...)
		if err != nil {
			return err
		}
	}

	return rows.Err()
}

// joinColumns joins column names with commas
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

// recordMigrationError records an error in the migration status
func recordMigrationError(migrationID, errorMsg string) {
	db.Exec(`
		UPDATE migration_status 
		SET status = 'failed', error_message = $1, completed_at = NOW() 
		WHERE id = $2
	`, errorMsg, migrationID)
	log.Printf("Migration %s failed: %s", migrationID, errorMsg)
}

// GetMigrationStatus retrieves the status of a migration
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

	// Parse tables migrated JSON
	if len(tablesJSON) > 0 {
		json.Unmarshal(tablesJSON, &status.TablesMigrated)
	}

	return &status, nil
}

// ============ Notification Functions ============

// GetUserNotifications retrieves notifications for a specific user
func GetUserNotifications(userID string, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.Query(`
		SELECT id, user_id, title, message, type, is_read, created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifications []Notification
	for rows.Next() {
		var n Notification
		err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Message, &n.Type, &n.IsRead, &n.CreatedAt)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, n)
	}

	return notifications, nil
}

// MarkNotificationAsRead marks a notification as read
func MarkNotificationAsRead(notificationID, userID string) error {
	_, err := db.Exec(`
		UPDATE notifications 
		SET is_read = true 
		WHERE id = $1 AND user_id = $2
	`, notificationID, userID)

	return err
}

// GetUnreadNotificationCount gets the count of unread notifications
func GetUnreadNotificationCount(userID string) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM notifications 
		WHERE user_id = $1 AND is_read = false
	`, userID).Scan(&count)

	return count, err
}
