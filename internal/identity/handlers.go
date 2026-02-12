package main

import (
	"encoding/json"
	"log"
	"net/http"
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

	// Normalize IDs to internal storage format
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

	// Normalize incoming ID to find in DB
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

	// ✅ REAL profile from PostgreSQL
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

	// Map Internal IDs to External Display IDs for response
	identity.UserID = ToExternalID(identity.UserID)
	profile.UserID = ToExternalID(profile.UserID)

	// Check if viewer follows this user
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
		Username   string `json:"username"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Username == "" {
		RespondWithError(w, http.StatusBadRequest, "username required")
		return
	}

	if req.Email == "" {
		RespondWithError(w, http.StatusBadRequest, "email required")
		return
	}

	// Validate invite code
	if req.InviteCode == "" {
		RespondWithError(w, http.StatusForbidden, "invite code required")
		return
	}

	// Check invite
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

	// Check if username already exists
	federatedUserID := req.Username + "@" + InternalServerName
	existingUser, err := GetIdentityByUserID(federatedUserID)
	if err != nil {
		log.Printf("Error checking username: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existingUser != nil {
		RespondWithError(w, http.StatusConflict, "username already taken")
		return
	}

	// Check if email already exists
	existingEmail, err := GetIdentityByEmail(req.Email)
	if err != nil {
		log.Printf("Error checking email: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existingEmail != nil {
		RespondWithError(w, http.StatusConflict, "email already registered")
		return
	}

	// Check OTP rate limit
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
	sessionID, err := StoreOTP(req.Email, otpCode, "registration")
	if err != nil {
		log.Printf("Failed to store OTP: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store OTP")
		return
	}

	// Send OTP email
	if err := SendOTPEmail(req.Email, otpCode, "registration"); err != nil {
		log.Printf("Failed to send OTP email: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to send OTP email")
		return
	}

	// Store registration data temporarily in a session (we'll use the OTP session_id as key)
	// For now, we'll return session_id and expect the frontend to call /complete-registration
	// after OTP verification

	// Return session ID and masked email for OTP verification
	maskedEmail := maskEmail(req.Email)
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "OTP sent to your email",
		"session_id":   sessionID,
		"email_hint":   maskedEmail,
		"expires_in":   int(OTPExpiry.Seconds()),
		"requires_otp": true,
	})
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Get UserID from query param
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized: missing user_id param")
		return
	}

	internalID := ToInternalID(userID)

	// 2. Fetch models.Identity
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

	// 3. Fetch models.Profile
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

	// Map Internal IDs to External Display IDs
	identity.UserID = ToExternalID(identity.UserID)
	profile.UserID = ToExternalID(profile.UserID)

	// 4. Return combined document
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

	// Normalize ID
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

	// Default limit=20, offset=0
	limit := 20
	offset := 0

	// Parse limit/offset if necessary (simple implementation assumes defaults for now or could parse query params)

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

	// Map author IDs in posts to External IDs
	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
	})
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Optionally check if the DB is reachable
	if err := db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("healthy"))
}
