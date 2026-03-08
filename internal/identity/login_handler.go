package identity

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
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

	req.Username = strings.ToLower(req.Username)
	// Convert username to internal format
	federatedUserID := ToInternalID(req.Username)

	var passwordHash string
	var homeServer string
	var totpEnabled bool
	err := db.QueryRow(`
		SELECT password_hash, home_server, COALESCE(totp_enabled, FALSE)
		FROM identities
		WHERE user_id = $1
	`, federatedUserID).Scan(&passwordHash, &homeServer, &totpEnabled)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		log.Println("Login error:", err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !CheckPasswordHash(req.Password, passwordHash) {
		RespondWithError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// If TOTP is enabled, issue a short-lived partial token instead of full tokens.
	// The client must complete /login/totp to receive full access/refresh tokens.
	if totpEnabled {
		partialToken, err := createPartialToken(federatedUserID)
		if err != nil {
			log.Println("Failed to create partial token:", err)
			RespondWithError(w, http.StatusInternalServerError, "internal error")
			return
		}
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"totp_required": true,
			"partial_token": partialToken,
		})
		return
	}

	// No TOTP – issue full tokens directly.
	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080" // fallback for local dev
	}

	accessToken, refreshToken, err := GenerateTokenPair(federatedUserID, externalURL)
	if err != nil {
		log.Println("Token generation failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"user_id":       ToExternalID(federatedUserID),
		"home_server":   externalURL,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

// POST /logout
// Stateless JWTs cannot be server-side invalidated, so this is a no-op that
// returns 200 so the frontend can complete its client-side cleanup.
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// POST /refresh-token
// Body: {"refresh_token": "..."}
// Issues a new access token from a valid refresh token.
func RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		RespondWithError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	claims, err := ValidateUserToken(body.RefreshToken)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}
	// Ensure the token was issued as a refresh token (not an access token)
	if claims.Issuer != "federated-identity-refresh" {
		RespondWithError(w, http.StatusUnauthorized, "not a refresh token")
		return
	}

	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}

	accessToken, _, err := GenerateTokenPair(claims.UserID, externalURL)
	if err != nil {
		log.Println("RefreshTokenHandler: token generation failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
	})
}
