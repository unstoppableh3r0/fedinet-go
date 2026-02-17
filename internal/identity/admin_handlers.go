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





