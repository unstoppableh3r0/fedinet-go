package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// ============ Admin Authentication Middleware ============

// AdminAuthMiddleware validates JWT token for admin routes
func AdminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			RespondWithError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Extract token from "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			RespondWithError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		tokenString := parts[1]

		// Validate token
		claims, err := ValidateJWT(tokenString)
		if err != nil {
			RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		if !claims.IsAdmin {
			RespondWithError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		// Token is valid, proceed to handler
		next.ServeHTTP(w, r)
	})
}

// ============ Admin Handlers ============

// AdminLoginHandler handles admin login
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

	// Validate credentials
	if !ValidateAdminCredentials(req.Username, req.Password) {
		RespondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Generate JWT token
	token, err := GenerateJWT(req.Username)
	if err != nil {
		log.Println("Failed to generate JWT:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return token
	RespondWithJSON(w, http.StatusOK, map[string]string{
		"token":   token,
		"message": "login successful",
	})
}

// GetServerConfigHandler retrieves current server configuration
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

// UpdateServerConfigHandler updates server configuration
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

	// Extract admin username from JWT (already validated by middleware)
	authHeader := r.Header.Get("Authorization")
	tokenString := strings.Split(authHeader, " ")[1]
	claims, _ := ValidateJWT(tokenString)

	// Update server name
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

// TestDatabaseHandler tests a database connection
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

	// Test connection
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

// StartMigrationHandler initiates database migration
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

	// Test connection first
	err := TestDatabaseConnection(req.NewConnectionString)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "cannot connect to new database: "+err.Error())
		return
	}

	// Start migration
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

// GetMigrationStatusHandler retrieves migration status
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

// GetAllUsersHandler retrieves all users
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

// GetStatsHandler retrieves server statistics
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

// ============ User Notification Handlers ============

// GetUserNotificationsHandler retrieves notifications for a user
func GetUserNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id parameter required")
		return
	}

	notifications, err := GetUserNotifications(userID, 50)
	if err != nil {
		log.Println("Failed to get notifications:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to retrieve notifications")
		return
	}

	// Get unread count
	unreadCount, err := GetUnreadNotificationCount(userID)
	if err != nil {
		log.Println("Failed to get unread count:", err)
		unreadCount = 0
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": notifications,
		"unread_count":  unreadCount,
	})
}

// MarkNotificationReadHandler marks a notification as read
func MarkNotificationReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		NotificationID string `json:"notification_id"`
		UserID         string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.NotificationID == "" || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "notification_id and user_id are required")
		return
	}

	err := MarkNotificationAsRead(req.NotificationID, req.UserID)
	if err != nil {
		log.Println("Failed to mark notification as read:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to update notification")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "notification marked as read",
	})
}
