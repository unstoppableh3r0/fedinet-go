package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ToggleLikeHandler handles POST /post/like
// Body: { "user_id": "...", "post_id": "..." }
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
		RespondWithError(w, http.StatusBadRequest, "missing user_id or post_id")
		return
	}

	internalUserID := ToInternalID(req.UserID)
	if err := ToggleLike(internalUserID, req.PostID); err != nil {
		log.Printf("ToggleLike error for user %s post %s: %v", internalUserID, req.PostID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to toggle like")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

// ToggleRepostHandler handles POST /post/repost
// Body: { "user_id": "...", "post_id": "..." }
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
		RespondWithError(w, http.StatusBadRequest, "missing user_id or post_id")
		return
	}

	internalUserID := ToInternalID(req.UserID)
	if err := ToggleRepost(internalUserID, req.PostID); err != nil {
		log.Printf("ToggleRepost error for user %s post %s: %v", internalUserID, req.PostID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to toggle repost")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

// CreatePostHandler handles POST /post/create
// Body: { "user_id": "...", "content": "..." }
func CreatePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID   string  `json:"user_id"`
		Content  string  `json:"content"`
		ImageURL *string `json:"image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" || (req.Content == "" && req.ImageURL == nil) {
		RespondWithError(w, http.StatusBadRequest, "missing user_id or content")
		return
	}

	internalUserID := ToInternalID(req.UserID)
	postID, err := CreatePost(internalUserID, req.Content, req.ImageURL)
	if err != nil {
		log.Printf("CreatePost error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create post")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{"post_id": postID})
}

// GetRecentPostsHandler handles GET /posts/recent
// Query params: limit (optional, default 20), user_id (optional, for has_liked/has_reposted)
func GetRecentPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	// viewer user_id is optional — used to populate has_liked / has_reposted
	viewerID := ToInternalID(r.URL.Query().Get("user_id"))

	posts, err := GetRecentPosts(viewerID, limit)
	if err != nil {
		log.Printf("GetRecentPosts error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch recent posts")
		return
	}

	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
		"limit": limit,
	})
}

// GetUserPostsHandler handles GET /posts/user
// Query params: user_id (required), viewer_id (optional), limit, offset
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
	if viewerID == "" {
		viewerID = userID
	}

	limit := 20
	offset := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	posts, err := GetUserPosts(ToInternalID(userID), ToInternalID(viewerID), limit, offset)
	if err != nil {
		log.Printf("GetUserPosts error for user %s: %v", userID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch user posts")
		return
	}

	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts":  posts,
		"limit":  limit,
		"offset": offset,
	})
}

// GetSuggestedUsersHandler handles GET /users/suggested
// Query params: limit (optional, default 20)
func GetSuggestedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	users, err := GetSuggestedUsers(limit)
	if err != nil {
		log.Printf("GetSuggestedUsers error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch suggested users")
		return
	}

	for i := range users {
		users[i].Identity.UserID = ToExternalID(users[i].Identity.UserID)
		users[i].Profile.UserID = ToExternalID(users[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"count": len(users),
	})
}

// MeHandler handles GET /user/me
// Query params: user_id (required)
// Returns { "profile": {...}, "identity": {...} } for the authenticated user's own profile.
func MeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	internalUserID := ToInternalID(userID)

	ident, err := GetIdentityByUserID(internalUserID)
	if err != nil {
		log.Printf("MeHandler GetIdentityByUserID error for %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ident == nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	profile, err := GetProfileByUserID(internalUserID)
	if err != nil {
		log.Printf("MeHandler GetProfileByUserID error for %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if profile == nil {
		RespondWithError(w, http.StatusNotFound, "profile not found")
		return
	}

	// Compute follower / following counts from the follows table.
	var followersCount, followingCount int
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE followee_user_id = $1", internalUserID).Scan(&followersCount)
	db.QueryRow("SELECT COUNT(*) FROM follows WHERE follower_user_id = $1", internalUserID).Scan(&followingCount)

	ident.UserID = ToExternalID(ident.UserID)
	profile.UserID = ToExternalID(profile.UserID)
	profile.FollowersCount = &followersCount
	profile.FollowingCount = &followingCount

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"identity": ident,
		"profile":  profile,
	})
}

// GetUserRepliesHandler handles GET /posts/user/replies
// Query params: user_id (required), limit, offset
func GetUserRepliesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	limit, offset := 20, 0
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	replies, err := GetUserReplies(ToInternalID(userID), limit, offset)
	if err != nil {
		log.Printf("GetUserReplies error for %s: %v", userID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch replies")
		return
	}
	for i := range replies {
		replies[i].PostAuthor = ToExternalID(replies[i].PostAuthor)
	}
	if replies == nil {
		replies = []UserReply{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"replies": replies,
	})
}

// GetUserLikedPostsHandler handles GET /posts/user/likes
// Query params: user_id (required), viewer_id (optional), limit, offset
func GetUserLikedPostsHandler(w http.ResponseWriter, r *http.Request) {
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
	if viewerID == "" {
		viewerID = userID
	}
	limit, offset := 20, 0
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	posts, err := GetUserLikedPosts(ToInternalID(userID), ToInternalID(viewerID), limit, offset)
	if err != nil {
		log.Printf("GetUserLikedPosts error for %s: %v", userID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch liked posts")
		return
	}
	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}
	if posts == nil {
		posts = []models.Post{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
	})
}
