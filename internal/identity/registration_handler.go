package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// CompleteRegistrationRequest represents the request to complete registration after OTP verification
type CompleteRegistrationRequest struct {
	SessionID  string `json:"session_id"`
	OTP        string `json:"otp"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

// CompleteRegistrationHandler handles account creation after OTP verification
func CompleteRegistrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CompleteRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate all required fields
	if req.SessionID == "" || req.OTP == "" || req.Email == "" || req.Username == "" || req.Password == "" || req.InviteCode == "" {
		RespondWithError(w, http.StatusBadRequest, "all fields required")
		return
	}

	// Verify OTP
	otp, err := VerifyOTP(req.Email, req.OTP, req.SessionID)
	if err != nil {
		log.Printf("OTP verification failed: %v", err)
		RespondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Verify purpose is registration
	if otp.Purpose != "registration" {
		RespondWithError(w, http.StatusBadRequest, "invalid OTP purpose")
		return
	}

	// Validate invite code (again, for security)
	invite, err := ValidateInvite(req.InviteCode)
	if err != nil {
		RespondWithError(w, http.StatusForbidden, "invalid or expired invite: "+err.Error())
		return
	}
	if invite.InviteType != "user" {
		RespondWithError(w, http.StatusForbidden, "invalid invite type")
		return
	}

	// Validate Username Format
	if !ValidateUsername(req.Username) {
		RespondWithError(w, http.StatusBadRequest, "invalid username format (alphanumeric, 3-30 chars)")
		return
	}

	// Normalize
	req.Username = strings.ToLower(req.Username)
	req.Email = strings.ToLower(req.Email)

	// Create account
	federatedUserID := req.Username + "@" + InternalServerName
	homeServer := "http://localhost:8082"

	// Create account with email
	recoveryKey, err := CreateAccountWithEmail(federatedUserID, homeServer, req.Email, req.Password)
	if err != nil {
		log.Println("Registration failed:", err)
		if err.Error() == "user already exists" {
			RespondWithError(w, http.StatusConflict, "username taken")
			return
		}
		RespondWithError(w, http.StatusInternalServerError, "internal registration error")
		return
	}

	// Mark invite as used
	identity, _ := GetIdentityByUserID(federatedUserID)
	var userID string
	if identity != nil {
		userID = identity.ID.String()
	}

	if err := UseInvite(req.InviteCode, userID, r.RemoteAddr, r.UserAgent()); err != nil {
		log.Printf("Error marking invite used: %v", err)
	}

	// Generate tokens for the new user
	accessToken, err := GenerateAccessToken(federatedUserID)
	if err != nil {
		log.Printf("Failed to generate access token: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	refreshToken, err := GenerateRefreshToken(federatedUserID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		log.Printf("Failed to generate refresh token: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Return success with tokens and recovery key
	RespondWithJSON(w, http.StatusCreated, map[string]string{
		"message":       "registration successful",
		"user_id":       ToExternalID(federatedUserID),
		"home_server":   homeServer,
		"recovery_key":  recoveryKey,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}
