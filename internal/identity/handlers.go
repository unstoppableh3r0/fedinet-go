package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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
		From    string `json:"from"`
		To      string `json:"to"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.From == "" || req.To == "" || req.Content == "" {
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
	if err := SendMessage(ToInternalID(req.From), ToInternalID(req.To), req.Content); err != nil {
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
		Email           string `json:"email"`
		InviteCode      string `json:"invite_code"`
		HomeServer      string `json:"home_server"`
		ClientPublicKey string `json:"client_public_key"` // New: client's Ed25519 public key
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		RespondWithError(w, http.StatusBadRequest, "username, email and password required")
		return
	}

	// Validate invite code
	req.InviteCode = strings.TrimSpace(req.InviteCode)
	if req.InviteCode == "" {
		RespondWithError(w, http.StatusForbidden, "invite code required")
		return
	}

	invite, err := ValidateInvite(req.InviteCode)
	if err != nil {
		RespondWithError(w, http.StatusForbidden, "invalid or expired invite: "+err.Error())
		return
	}
	if invite.InviteType != "user" {
		RespondWithError(w, http.StatusForbidden, "invalid invite type")
		return
	}

	if !ValidateUsername(req.Username) {
		RespondWithError(w, http.StatusBadRequest, "invalid username format (alphanumeric, 3-30 chars)")
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
		homeServer = "http://localhost:8082" // Default fallback
	}

	// Create Account with client public key
	recoveryKey, err := CreateAccountWithClientKey(federatedUserID, homeServer, hashedPassword, req.ClientPublicKey)
	if err != nil {
		log.Println("CreateAccount error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	// Mark invite as used
	if err := UseInvite(req.InviteCode, federatedUserID, r.RemoteAddr, r.UserAgent()); err != nil {
		log.Printf("Failed to mark invite %s as used: %v", req.InviteCode, err)
	}

	// Generate session key for the new user
	sessionKey, err := GenerateSessionKey(federatedUserID)
	if err != nil {
		log.Printf("Failed to generate session key for %s: %v", federatedUserID, err)
		// Continue without session key for now
	}

	// Generate Tokens
	accessToken, refreshToken, err := GenerateTokenPair(federatedUserID, homeServer)
	if err != nil {
		log.Println("Token generation failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	response := map[string]interface{}{
		"user_id":       ToExternalID(federatedUserID),
		"home_server":   homeServer,
		"recovery_key":  recoveryKey,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	}

	// Include session key if generated
	if sessionKey != nil {
		response["session_key_encrypted"] = sessionKey.SymmetricKeyEncrypted
		response["session_key_signature"] = sessionKey.Signature
		response["session_key_version"] = sessionKey.KeyVersion
		response["session_key_expires_at"] = sessionKey.ExpiresAt.Format(time.RFC3339)
	}

	RespondWithJSON(w, http.StatusCreated, response)
}

func MeHandler(w http.ResponseWriter, r *http.Request) {

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized: missing user_id param")
		return
	}

	internalID := ToInternalID(userID)

	identity, err := GetIdentityByUserID(internalID)
	if err != nil {
		log.Printf("MeHandler: GetIdentityByUserID error for user %s: %v", internalID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if identity == nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	profile, err := GetProfileByUserID(internalID)
	if err != nil {
		log.Printf("MeHandler: GetProfileByUserID error for user %s: %v", internalID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if profile == nil {
		RespondWithError(w, http.StatusInternalServerError, "profile missing (integrity error)")
		return
	}

	identity.UserID = ToExternalID(identity.UserID)
	profile.UserID = ToExternalID(profile.UserID)

	doc := models.UserDocument{
		Identity: *identity,
		Profile:  *profile,
	}

	RespondWithJSON(w, http.StatusOK, doc)
}

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
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	req.UserID = ToInternalID(req.UserID)

	if err := UpdateProfile(req); err != nil {
		log.Println("models.Profile update failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "profile update failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"message": "profile updated successfully",
	})
}

func CreatePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID  string `json:"user_id"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and content required")
		return
	}

	internalID := ToInternalID(req.UserID)

	postID, err := CreatePost(internalID, req.Content)
	if err != nil {
		log.Println("models.Post creation failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "post creation failed")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{
		"post_id": postID,
		"message": "post created successfully",
	})
}

func ToggleLikeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		PostID string `json:"post_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.PostID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and post_id required")
		return
	}

	if err := ToggleLike(ToInternalID(req.UserID), req.PostID); err != nil {
		log.Println("Like failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "like toggled"})
}

func ToggleRepostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		PostID string `json:"post_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.PostID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and post_id required")
		return
	}

	if err := ToggleRepost(ToInternalID(req.UserID), req.PostID); err != nil {
		log.Println("Repost failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "repost toggled"})
}

func GetUserPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	viewerID := r.URL.Query().Get("viewer_id")

	limit := 20
	offset := 0

	internalTarget := ToInternalID(userID)
	internalViewer := ""
	if viewerID != "" {
		internalViewer = ToInternalID(viewerID)
	}

	posts, err := GetUserPosts(internalTarget, internalViewer, limit, offset)
	if err != nil {
		log.Println("Failed to fetch posts:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch posts")
		return
	}

	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
	})
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {

	if err := db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("healthy"))
}

func GetRecentPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	viewerID := r.URL.Query().Get("viewer_id")
	limit := 20

	internalViewer := ""
	if viewerID != "" {
		internalViewer = ToInternalID(viewerID)
	}

	posts, err := GetRecentPosts(internalViewer, limit)
	if err != nil {
		log.Println("Failed to fetch recent posts:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch posts")
		return
	}

	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
	})
}

func GetSuggestedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 20

	users, err := GetSuggestedUsers(limit)
	if err != nil {
		log.Println("Failed to fetch suggested users:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch users")
		return
	}

	for i := range users {
		users[i].Identity.UserID = ToExternalID(users[i].Identity.UserID)
		users[i].Profile.UserID = ToExternalID(users[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
	})
}
