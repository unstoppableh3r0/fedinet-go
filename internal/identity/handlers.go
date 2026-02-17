package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		InviteCode      string `json:"invite_code"`
		ClientPublicKey string `json:"client_public_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// 🔥 Invite check FIRST (for integration test requirement)
	if req.InviteCode == "" {
		RespondWithError(w, http.StatusForbidden, "invite code required")
		return
	}

	if req.Username == "" {
		RespondWithError(w, http.StatusBadRequest, "username required")
		return
	}

	if req.Password == "" {
		RespondWithError(w, http.StatusBadRequest, "password required")
		return
	}

	// Normalize username
	req.Username = strings.ToLower(req.Username)

	federatedUserID := req.Username + "@" + InternalServerName

	// Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Determine Home Server URL
	homeServer := os.Getenv("SERVER_URL")
	if homeServer == "" {
		homeServer = "http://localhost:8082"
	}

	// Create Account
	recoveryKey, err := CreateAccountWithClientKey(
		federatedUserID,
		homeServer,
		hashedPassword,
		req.ClientPublicKey,
	)
	if err != nil {
		log.Println("CreateAccount error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	// Mark invite as used
	if err := UseInvite(req.InviteCode, federatedUserID, r.RemoteAddr, r.UserAgent()); err != nil {
		log.Printf("Failed to mark invite %s as used: %v", req.InviteCode, err)
	}

	// Generate session key (optional)
	sessionKey, err := GenerateSessionKey(federatedUserID)
	if err != nil {
		log.Printf("Failed to generate session key for %s: %v", federatedUserID, err)
	}

	// Generate access + refresh tokens
	accessToken, refreshToken, err := GenerateTokenPair(federatedUserID, homeServer)
	if err != nil {
		log.Println("Token generation failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	// Success response
	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":       ToExternalID(federatedUserID),
		"home_server":   homeServer,
		"recovery_key":  recoveryKey,
		"session_key":   sessionKey,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}
