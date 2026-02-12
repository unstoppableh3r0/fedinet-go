package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// RefreshTokenRequest represents the request body for token refresh
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenResponse represents the response after token refresh
type RefreshTokenResponse struct {
	AccessToken string `json:"access_token"`
	Message     string `json:"message"`
}

// LogoutRequest represents the request body for logout
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenHandler handles token refresh requests
func RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.RefreshToken == "" {
		RespondWithError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	// Validate refresh token
	userID, err := ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		log.Printf("Refresh token validation failed: %v", err)
		RespondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Generate new access token
	accessToken, err := GenerateAccessToken(userID)
	if err != nil {
		log.Printf("Failed to generate access token: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	response := RefreshTokenResponse{
		AccessToken: accessToken,
		Message:     "token refreshed successfully",
	}

	RespondWithJSON(w, http.StatusOK, response)
}

// LogoutHandler handles user logout and token revocation
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.RefreshToken == "" {
		RespondWithError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	// Revoke the refresh token
	err := RevokeRefreshToken(req.RefreshToken)
	if err != nil {
		log.Printf("Failed to revoke token: %v", err)
		// Don't return error to client - logout should always succeed
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "logged out successfully",
	})
}
