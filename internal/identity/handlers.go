package identity

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

func FollowHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("---- /follow HIT ----")
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Follower string `json:"follower"`
		Followee string `json:"followee"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Follower == "" || req.Followee == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	internalFollower := ToInternalID(req.Follower)
	internalFollowee := ToInternalID(req.Followee)

	if err := FollowUser(internalFollower, internalFollowee); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "followed"})
}

func MessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		From     string  `json:"from"`
		To       string  `json:"to"`
		Content  string  `json:"content"`
		ImageURL *string `json:"image_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.From == "" || req.To == "" || (req.Content == "" && req.ImageURL == nil) {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	// Check if this is a federated message (recipient on another server)
	isFederated, serverName := IsFederatedUser(req.To)

	if isFederated {
		// Deliver to remote server
		log.Printf("Routing federated message from %s to %s (server: %s)", req.From, req.To, serverName)

		err := DeliverFederatedMessage(ToInternalID(req.From), req.To, req.Content)
		if err != nil {
			log.Printf("Failed to deliver federated message: %v", err)
			RespondWithError(w, http.StatusBadGateway, fmt.Sprintf("failed to deliver message: %v", err))
			return
		}

		// Store a copy for sender's message history
		if err := StoreSentFederatedMessage(ToInternalID(req.From), req.To, req.Content); err != nil {
			log.Printf("Warning: failed to store sent message copy: %v", err)
			// Don't fail the request - message was already delivered
		}

		RespondWithJSON(w, http.StatusOK, map[string]string{
			"message": "federated message sent",
			"server":  serverName,
		})
		return
	}

	// Local message - use existing logic
	// Block check — do not allow messaging between blocked users.
	internalFrom := ToInternalID(req.From)
	internalTo := ToInternalID(req.To)
	var msgBlocked bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM block_events WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
		internalFrom, internalTo).Scan(&msgBlocked) //nolint:errcheck
	if msgBlocked {
		RespondWithError(w, http.StatusForbidden, "cannot message a blocked user")
		return
	}
	if err := SendMessage(internalFrom, internalTo, req.Content, req.ImageURL); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "message sent"})
}

func UserSearchHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Println("PANIC recovered:", rec)
			RespondWithError(w, http.StatusInternalServerError, "internal server error")
		}
	}()

	log.Println("---- /user/search HIT ----")

	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing user_id")
		return
	}

	log.Printf("Search request: original userID = %s", userID)

	internalUserID := ToInternalID(userID)

	log.Printf("Search request: converted to internalUserID = %s", internalUserID)

	identity, err := GetIdentityByUserID(internalUserID)
	if err != nil {
		log.Printf("GetIdentityByUserID error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if identity == nil {
		log.Printf("User not found in database: %s", internalUserID)
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	if !identity.AllowDiscovery {
		RespondWithError(w, http.StatusForbidden, "profile unavailable")
		return
	}

	profile, err := GetProfileByUserID(internalUserID)
	if err != nil {
		log.Printf("GetProfileByUserID error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if profile == nil {
		RespondWithError(w, http.StatusNotFound, "profile not found")
		return
	}

	identity.UserID = ToExternalID(identity.UserID)
	profile.UserID = ToExternalID(profile.UserID)

	// Compute follower / following counts
	var followersCount, followingCount int
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE followee_user_id = $1", internalUserID).Scan(&followersCount)
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE follower_user_id = $1", internalUserID).Scan(&followingCount)
	profile.FollowersCount = &followersCount
	profile.FollowingCount = &followingCount

	viewerID := r.URL.Query().Get("viewer_id")
	isFollowing := false
	if viewerID != "" && viewerID != userID {
		internalViewerID := ToInternalID(viewerID)
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM follows WHERE follower_user_id=$1 AND followee_user_id=$2)", internalViewerID, internalUserID).Scan(&isFollowing)
		if err != nil {
			log.Println("Error checking follow status:", err)
		}
	}

	response := map[string]interface{}{
		"identity":     *identity,
		"profile":      *profile,
		"is_following": isFollowing,
		"badge":        GetUserBadge(internalUserID),
		"is_moderator": IsModerator(internalUserID),
	}

	RespondWithJSON(w, http.StatusOK, response)
}

func strPtr(s string) *string {
	return &s
}

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

	// Convert username to internal format
	federatedUserID := ToInternalID(strings.ToLower(req.Username))
	if identity, err := GetIdentityByUserID(federatedUserID); err == nil && identity != nil {
		RespondWithError(w, http.StatusConflict, "username taken")
		return
	}

	// Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Determine Home Server URL
	homeServer := os.Getenv("SERVER_URL")
	if homeServer == "" {
		homeServer = "http://localhost:8080"
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

// UpdateProfileHandler handles POST /profile/update
// Body: models.UpdateProfileRequest (user_id required; all other fields optional)
func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	req.UserID = ToInternalID(req.UserID)

	if err := UpdateProfile(req); err != nil {
		log.Printf("UpdateProfileHandler error for user %s: %v", req.UserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}
