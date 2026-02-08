package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
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

	// Normalize incoming ID to find in DB
	internalUserID := ToInternalID(userID)

	identity, err := GetIdentityByUserID(internalUserID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if identity == nil {
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

	doc := UserDocument{
		Identity: *identity, // kept for later crypto use
		Profile:  *profile,  // UI-safe
	}

	RespondWithJSON(w, http.StatusOK, doc)
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
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Username == "" {
		RespondWithError(w, http.StatusBadRequest, "username required")
		return
	}

	// Validate Username Format
	if !ValidateUsername(req.Username) {
		RespondWithError(w, http.StatusBadRequest, "invalid username format (alphanumeric, 3-30 chars)")
		return
	}

	// Normalize
	req.Username = strings.ToLower(req.Username)

	// Always creating as localhost internal user
	federatedUserID := req.Username + "@" + InternalServerName
	homeServer := "http://localhost:8082"

	recoveryKey, err := CreateAccount(federatedUserID, homeServer, req.Password)
	if err != nil {
		log.Println("Registration failed:", err)
		if err.Error() == "user already exists" {
			RespondWithError(w, http.StatusConflict, "username taken")
			return
		}
		RespondWithError(w, http.StatusInternalServerError, "internal registration error")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{
		"user_id":      ToExternalID(federatedUserID),
		"home_server":  homeServer,
		"recovery_key": recoveryKey,
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

	// 2. Fetch Identity
	identity, err := GetIdentityByUserID(internalID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if identity == nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	// 3. Fetch Profile
	profile, err := GetProfileByUserID(internalID)
	if err != nil {
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
	doc := UserDocument{
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

	var req UpdateProfileRequest
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
		log.Println("Profile update failed:", err)
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
		log.Println("Post creation failed:", err)
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

func CreateReplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID  string `json:"user_id"`
		PostID  string `json:"post_id"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.PostID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	replyID, err := CreateReply(ToInternalID(req.UserID), req.PostID, req.Content)
	if err != nil {
		log.Println("Reply failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "reply failed")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{"reply_id": replyID})
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
