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

	// Normalize username to internal ID format
	req.Username = strings.ToLower(req.Username)
	federatedUserID := req.Username
	if !strings.Contains(req.Username, "@") {
		federatedUserID = req.Username + "@" + InternalServerName
	}

	// Fetch user from database
	var passwordHash string
	var email string
	err := db.QueryRow(`
		SELECT password_hash, COALESCE(email, '') 
		FROM identities 
		WHERE user_id = $1
	`, federatedUserID).Scan(&passwordHash, &email)

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

	// Check if user has email configured
	if email == "" {
		RespondWithError(w, http.StatusBadRequest, "email not configured for this account - please contact admin")
		return
	}

	// Check OTP rate limit
	if err := CheckOTPRateLimit(email); err != nil {
		RespondWithError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	// Generate OTP
	otpCode, err := GenerateOTP()
	if err != nil {
		log.Printf("Failed to generate OTP: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate OTP")
		return
	}

	// Store OTP
	sessionID, err := StoreOTP(email, otpCode, "login")
	if err != nil {
		log.Printf("Failed to store OTP: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store OTP")
		return
	}

	// Send OTP email
	if err := SendOTPEmail(email, otpCode, "login"); err != nil {
		log.Printf("Failed to send OTP email: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to send OTP email")
		return
	}

	// Return session ID and masked email for OTP verification
	maskedEmail := maskEmail(email)
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "OTP sent to your email",
		"session_id":   sessionID,
		"email_hint":   maskedEmail,
		"expires_in":   int(OTPExpiry.Seconds()),
		"requires_otp": true,
	})
}

// maskEmail masks the email address for display
func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***@***"
	}
	local := parts[0]
	domain := parts[1]

	if len(local) <= 2 {
		return "***@" + domain
	}

	return string(local[0]) + "***" + string(local[len(local)-1]) + "@" + domain
}
