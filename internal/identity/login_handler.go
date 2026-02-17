package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

func LoginHandler(w http.ResponseWriter, r *http.Request) {
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
		RespondWithError(w, http.StatusBadRequest, "username and password required")
		return
	}

	// Normalize username
	req.Username = strings.ToLower(req.Username)
	federatedUserID := req.Username
	if !strings.Contains(req.Username, "@") {
		federatedUserID = req.Username + "@" + InternalServerName
	}

	// Fetch user
	var passwordHash string
	var homeServer string

	err := db.QueryRow(`
		SELECT password_hash, home_server 
		FROM identities 
		WHERE user_id = $1
	`, federatedUserID).Scan(&passwordHash, &homeServer)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		log.Println("Login error:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Verify password
	if !CheckPasswordHash(req.Password, passwordHash) {
		RespondWithError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// ✅ Generate JWT (valid for 24 hours)
	token, err := GenerateUserJWT(federatedUserID, homeServer)
	if err != nil {
		log.Println("JWT generation error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// ✅ Return token + user info
	RespondWithJSON(w, http.StatusOK, map[string]string{
		"token":       token,
		"user_id":     ToExternalID(federatedUserID),
		"home_server": homeServer,
	})
}
