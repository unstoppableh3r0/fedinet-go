package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// SendOTPRequest represents the request body for sending OTP
type SendOTPRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"` // "login", "registration", "password_reset"
}

// SendOTPResponse represents the response after sending OTP
type SendOTPResponse struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// VerifyOTPRequest represents the request body for verifying OTP
type VerifyOTPRequest struct {
	Email     string `json:"email"`
	OTP       string `json:"otp"`
	SessionID string `json:"session_id"`
}

// VerifyOTPResponse represents the response after OTP verification
type VerifyOTPResponse struct {
	Message      string `json:"message"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id,omitempty"`
	HomeServer   string `json:"home_server,omitempty"`
	RecoveryKey  string `json:"recovery_key,omitempty"` // Only for registration
}

// SendOTPHandler handles requests to send OTP codes
func SendOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SendOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate request
	if req.Email == "" {
		RespondWithError(w, http.StatusBadRequest, "email required")
		return
	}

	if req.Purpose == "" {
		req.Purpose = "login" // Default purpose
	}

	// Validate purpose
	validPurposes := map[string]bool{
		"login":          true,
		"registration":   true,
		"password_reset": true,
	}
	if !validPurposes[req.Purpose] {
		RespondWithError(w, http.StatusBadRequest, "invalid purpose")
		return
	}

	// Check rate limit
	if err := CheckOTPRateLimit(req.Email); err != nil {
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
	sessionID, err := StoreOTP(req.Email, otpCode, req.Purpose)
	if err != nil {
		log.Printf("Failed to store OTP: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store OTP")
		return
	}

	// Send OTP email
	if err := SendOTPEmail(req.Email, otpCode, req.Purpose); err != nil {
		log.Printf("Failed to send OTP email: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to send OTP email")
		return
	}

	// Return session ID to client
	response := SendOTPResponse{
		Message:   "OTP sent successfully",
		SessionID: sessionID,
		ExpiresIn: int(OTPExpiry.Seconds()),
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// VerifyOTPHandler handles requests to verify OTP codes
func VerifyOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate request
	if req.Email == "" || req.OTP == "" || req.SessionID == "" {
		RespondWithError(w, http.StatusBadRequest, "email, otp, and session_id required")
		return
	}

	// Verify OTP
	otp, err := VerifyOTP(req.Email, req.OTP, req.SessionID)
	if err != nil {
		log.Printf("OTP verification failed: %v", err)
		RespondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Handle different purposes
	switch otp.Purpose {
	case "login":
		// User is logging in - fetch user identity
		identity, err := GetIdentityByEmail(req.Email)
		if err != nil {
			log.Printf("Failed to fetch user identity: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to authenticate user")
			return
		}
		if identity == nil {
			RespondWithError(w, http.StatusUnauthorized, "user not found")
			return
		}

		// Generate tokens
		accessToken, err := GenerateAccessToken(identity.UserID)
		if err != nil {
			log.Printf("Failed to generate access token: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		refreshToken, err := GenerateRefreshToken(identity.UserID, r.UserAgent(), r.RemoteAddr)
		if err != nil {
			log.Printf("Failed to generate refresh token: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		response := VerifyOTPResponse{
			Message:      "login successful",
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			UserID:       ToExternalID(identity.UserID),
			HomeServer:   identity.HomeServer,
		}

		RespondWithJSON(w, http.StatusOK, response)

	case "registration":
		// For registration, the account should have been created in a pending state
		// We'll just return success and let the frontend handle the next steps
		RespondWithJSON(w, http.StatusOK, map[string]string{
			"message": "OTP verified successfully",
			"purpose": "registration",
		})

	case "password_reset":
		// For password reset, return success and allow password update
		RespondWithJSON(w, http.StatusOK, map[string]string{
			"message": "OTP verified successfully",
			"purpose": "password_reset",
		})

	default:
		RespondWithError(w, http.StatusInternalServerError, "invalid OTP purpose")
	}
}
