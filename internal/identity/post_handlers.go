package identity

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	aimoderation "github.com/unstoppableh3r0/fedinet-go/internal/ai-moderation-service"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// canViewPost checks whether viewerID is allowed to interact with the given post.
// It enforces visibility (PUBLIC / FOLLOWERS / CLOSE_FRIENDS) and mutual block rules.
func canViewPost(viewerID, postID string) (bool, string) {
	var authorID, visibility string
	err := db.QueryRow("SELECT author, visibility FROM posts WHERE id = $1", postID).
		Scan(&authorID, &visibility)
	if err != nil {
		return false, ""
	}

	// Author can always see their own posts.
	if authorID == viewerID {
		return true, authorID
	}

	// Block check — bidirectional.
	var blocked bool
	db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM block_events
			WHERE (blocker_id = $1 AND blocked_id = $2)
			   OR (blocker_id = $2 AND blocked_id = $1)
		)`, viewerID, authorID).Scan(&blocked)
	if blocked {
		return false, authorID
	}

	switch visibility {
	case "PUBLIC":
		return true, authorID
	case "FOLLOWERS":
		var isFollower bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_user_id = $1 AND followee_user_id = $2)`,
			viewerID, authorID).Scan(&isFollower)
		return isFollower, authorID
	case "CLOSE_FRIENDS":
		var isCloseFriend bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM close_friends WHERE user_id = $1 AND friend_id = $2)`,
			authorID, viewerID).Scan(&isCloseFriend)
		return isCloseFriend, authorID
	default:
		return false, authorID
	}
}

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

	// Enforce visibility — users can only like posts they can actually see.
	if canSee, _ := canViewPost(internalUserID, req.PostID); !canSee {
		RespondWithError(w, http.StatusForbidden, "post not accessible")
		return
	}

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

	// Only block new reposts — always allow un-reposting.
	var alreadyReposted bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM reposts WHERE user_id=$1 AND post_id=$2)",
		internalUserID, req.PostID).Scan(&alreadyReposted)

	if !alreadyReposted {
		// Visibility check — cannot repost what you cannot see.
		canSee, authorID := canViewPost(internalUserID, req.PostID)
		if !canSee {
			RespondWithError(w, http.StatusForbidden, "post not accessible")
			return
		}

		// Restricted-visibility posts must not be reposted (would leak content).
		var vis string
		db.QueryRow("SELECT visibility FROM posts WHERE id=$1", req.PostID).Scan(&vis)
		if vis == "FOLLOWERS" || vis == "CLOSE_FRIENDS" {
			RespondWithError(w, http.StatusForbidden, "cannot repost a restricted-visibility post")
			return
		}

		var disableResharing bool
		db.QueryRow("SELECT COALESCE(disable_resharing, false) FROM privacy_settings WHERE user_id=$1",
			authorID).Scan(&disableResharing)
		if disableResharing {
			RespondWithError(w, http.StatusForbidden, "author has disabled resharing of their posts")
			return
		}
	}

	if err := ToggleRepost(internalUserID, req.PostID); err != nil {
		log.Printf("ToggleRepost error for user %s post %s: %v", internalUserID, req.PostID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to toggle repost")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

// CreatePostHandler handles user posts
func CreatePostHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID         string  `json:"user_id"`
		Content        string  `json:"content"`
		ImageURL       *string `json:"image_url"`
		ExpiresIn      *string `json:"expires_in"` // e.g. "1h", "6h", "24h", "3d", "7d" or RFC3339 timestamp
		Visibility     string  `json:"visibility"` // "PUBLIC" | "FOLLOWERS" | "CLOSE_FRIENDS" (default: "PUBLIC")
		ContentWarning *string `json:"content_warning"`
		// LinkedTargets is a list of server base URLs to replicate this post to,
		// e.g. ["http://serverB:8080", "http://serverC:8080"].
		// Only used when the post author has confirmed account links on those servers.
		LinkedTargets []string `json:"linked_targets,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || (req.Content == "" && req.ImageURL == nil) {
		RespondWithError(w, http.StatusBadRequest, "missing user_id or content")
		return
	}

	// ⭐ STEP A — AI MODERATION (skipped when moderation feature is disabled)
	var aiResult *aimoderation.ModerationResponse
	if IsModerationEnabled() {
		var err error
		aiResult, err = aimoderation.CallModerationAPI(req.Content)
		if err != nil {
			log.Println("AI moderation failed, continuing without AI:", err)
			aiResult = &aimoderation.ModerationResponse{
				Toxicity: 0.0, Hate: 0.0, Spam: 0.0, Threat: 0.0,
				Confidence: 0.0, Recommendation: "SAFE",
			}
		}
	} else {
		aiResult = &aimoderation.ModerationResponse{
			Toxicity: 0.0, Hate: 0.0, Spam: 0.0, Threat: 0.0,
			Confidence: 0.0, Recommendation: "SAFE",
		}
	}

	// ⭐ STEP B — FLAG AND DETERMINE VISIBILITY
	// Honour the user's requested visibility; AI moderation can override to HIDDEN.
	userVisibility := req.Visibility
	switch userVisibility {
	case "FOLLOWERS", "CLOSE_FRIENDS":
		// valid user-chosen levels — keep as is
	default:
		userVisibility = "PUBLIC"
	}
	visibility := userVisibility
	if aiResult.Recommendation != "SAFE" {
		visibility = "HIDDEN"
	}

	// ⭐ STEP C — PARSE EXPIRY
	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn != "" {
		// Accept shorthand durations: "1h", "6h", "24h", "3d", "7d"
		// or an explicit RFC3339 timestamp
		duration, durErr := parseExpiryShorthand(*req.ExpiresIn)
		if durErr == nil {
			t := time.Now().Add(duration)
			expiresAt = &t
		} else if ts, tsErr := time.Parse(time.RFC3339, *req.ExpiresIn); tsErr == nil {
			expiresAt = &ts
		}
		// If unparseable, create as a normal post (no expiry)
	}

	// ⭐ STEP D — GROUP ID for multi-server posting
	// A group_id is always assigned so replica posts can be deduplicated in feeds.
	groupIDStr := uuid.New().String()
	originServerURL := GetServerURL()
	var groupIDPtr, originPostPtr, originServerPtr *string
	groupIDPtr = &groupIDStr
	originServerPtr = &originServerURL

	// ⭐ STEP E — SAVE ORIGIN POST
	internalUserID := ToInternalID(req.UserID)
	// originPost is set after we have the postID
	postID, err := CreatePost(internalUserID, req.Content, req.ImageURL, visibility, expiresAt, req.ContentWarning, groupIDPtr, nil, originServerPtr)
	if err != nil {
		log.Printf("CreatePost error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	// Now back-fill origin_post = our own post_id
	originPostPtr = &postID
	_ = SetPostOrigin(postID, postID)

	// Log the post activity so admin dashboard counters stay accurate.
	go func() {
		if logErr := LogActivity(internalUserID, "POST", "post", postID, "", ""); logErr != nil {
			log.Printf("CreatePostHandler: LogActivity: %v", logErr)
		}
	}()

	// Ensure that if the post is hidden we explicitly inform the frontend gracefully
	if visibility == "HIDDEN" {
		// ⭐ Notify the author that their post is under review
		_ = CreateNotification(internalUserID, "system", "POST_UNDER_REVIEW",
			"Your post is being reviewed by our moderation team. Post ID: "+postID)
		RespondWithJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":     "post_flagged_and_hidden",
			"post_id":    postID,
			"moderation": aiResult,
		})
		return
	}

	// ⭐ STEP F — FAN-OUT TO LINKED SERVERS (async, best-effort)
	if len(req.LinkedTargets) > 0 {
		go fanOutLinkedPost(req.UserID, postID, groupIDStr, originServerURL, req.Content, req.ImageURL, visibility, expiresAt, req.ContentWarning, req.LinkedTargets)
	}

	// Legacy ActivityPub federation send (kept for backward compat)
	go func() {
		message := map[string]interface{}{
			"activity_type": "Create",
			"actor_id":      req.UserID,
			"target_server": "http://server_b_federation:8081",
			"payload": map[string]interface{}{
				"type":    "Create",
				"post_id": postID,
				"content": req.Content,
				"moderation": map[string]interface{}{
					"origin_server": true,
				},
			},
		}
		jsonData, _ := json.Marshal(message)
		resp, err := http.Post(
			"http://server_a_federation:8081/federation/send",
			"application/json",
			bytes.NewBuffer(jsonData),
		)
		if err != nil {
			log.Println("Federation HTTP error:", err)
			return
		}
		log.Println("Federation response:", resp.Status)
	}()

	_ = originPostPtr // used via SetPostOrigin
	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"post_id":    postID,
		"group_id":   groupIDStr,
		"moderation": aiResult,
	})
}

// fanOutLinkedPost sends a linked-post replication request to each target server
// that supports the linked_posts capability. Runs in a goroutine.
func fanOutLinkedPost(
	authorUserID, originPostID, groupID, originServerURL string,
	content string, imageURL *string,
	visibility string, expiresAt *time.Time,
	contentWarning *string,
	linkedTargets []string,
) {
	payload := map[string]interface{}{
		"group_id":      groupID,
		"origin_post":   originPostID,
		"origin_server": originServerURL,
		"author":        authorUserID,
		"content":       content,
		"visibility":    visibility,
	}
	if imageURL != nil {
		payload["image_url"] = *imageURL
	}
	if expiresAt != nil {
		payload["expires_at"] = expiresAt.Format(time.RFC3339)
	}
	if contentWarning != nil {
		payload["content_warning"] = *contentWarning
	}

	sigHeader := BuildFederationSignatureHeader()
	jsonData, _ := json.Marshal(payload)

	for _, targetURL := range linkedTargets {
		if !RemoteServerSupportsLinkedPosts(targetURL) {
			log.Printf("fanOutLinkedPost: server %s does not support linked_posts, skipping", targetURL)
			continue
		}
		req, err := http.NewRequest(http.MethodPost, targetURL+"/federation/linked-post", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("fanOutLinkedPost: failed to build request for %s: %v", targetURL, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Server-ID", sigHeader.ServerID)
		req.Header.Set("X-Signature", sigHeader.Signature)
		req.Header.Set("X-Timestamp", sigHeader.Timestamp)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("fanOutLinkedPost: HTTP error for %s: %v", targetURL, err)
			continue
		}
		resp.Body.Close()
		log.Printf("fanOutLinkedPost: delivered to %s — status %s", targetURL, resp.Status)
	}
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
// GetPostByIDHandler handles GET /post/get?post_id=...&viewer_id=...
func GetPostByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	postID := r.URL.Query().Get("post_id")
	if postID == "" {
		RespondWithError(w, http.StatusBadRequest, "post_id required")
		return
	}

	viewerID := r.URL.Query().Get("viewer_id")

	post, err := GetPostByID(postID, ToInternalID(viewerID))
	if err != nil {
		log.Printf("GetPostByID error for post %s: %v", postID, err)
		RespondWithError(w, http.StatusNotFound, "post not found")
		return
	}

	post.Author = ToExternalID(post.Author)
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"post": post})
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

	internalUserID := ToInternalID(userID)
	internalViewerID := ToInternalID(viewerID)

	// Enforce posts visibility
	ps := getPrivacySettingsForUser(internalUserID)
	if !canViewContent(ps.PostsVisibility, internalViewerID, internalUserID) {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"posts":  []interface{}{},
			"limit":  limit,
			"offset": offset,
		})
		return
	}

	posts, err := GetUserPosts(internalUserID, internalViewerID, limit, offset)
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
		"identity":     ident,
		"profile":      profile,
		"badge":        GetUserBadge(internalUserID),
		"is_moderator": IsModerator(internalUserID),
	})
}

// GetUserRepliesHandler handles GET /posts/user/replies
// Query params: user_id (required), viewer_id (optional), limit, offset
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

	internalUserID := ToInternalID(userID)
	internalViewerID := ToInternalID(viewerID)

	// Enforce replies visibility
	ps := getPrivacySettingsForUser(internalUserID)
	if !canViewContent(ps.RepliesVisibility, internalViewerID, internalUserID) {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"replies": []interface{}{},
		})
		return
	}

	replies, err := GetUserReplies(internalUserID, limit, offset)
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

	internalUserID := ToInternalID(userID)
	internalViewerID := ToInternalID(viewerID)

	// Enforce likes visibility
	ps := getPrivacySettingsForUser(internalUserID)
	if !canViewContent(ps.LikesVisibility, internalViewerID, internalUserID) {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"posts": []interface{}{},
		})
		return
	}

	posts, err := GetUserLikedPosts(internalUserID, internalViewerID, limit, offset)
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

// GetUserRepostedPostsHandler handles GET /posts/user/reposts
// Returns posts reposted (highlighted) by the given user.
// Query params: user_id (required), viewer_id (optional), limit, offset
func GetUserRepostedPostsHandler(w http.ResponseWriter, r *http.Request) {
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
	posts, err := GetUserRepostedPosts(ToInternalID(userID), ToInternalID(viewerID), limit, offset)
	if err != nil {
		log.Printf("GetUserRepostedPosts error for %s: %v", userID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch reposted posts")
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

// DeletePostHandler handles POST /post/delete
// Body: { "user_id": "...", "post_id": "..." }
// The caller must own the post; the request must carry a valid Authorization JWT.
func DeletePostHandler(w http.ResponseWriter, r *http.Request) {
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

	// Verify the JWT belongs to the claimed user.
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		RespondWithError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	claims, err := ValidateUserToken(parts[1])
	if err != nil || claims.UserID != req.UserID {
		RespondWithError(w, http.StatusForbidden, "not authorized")
		return
	}

	internalUserID := ToInternalID(req.UserID)

	if err := DeletePost(req.PostID, internalUserID); err != nil {
		log.Printf("DeletePost error user=%s post=%s: %v", internalUserID, req.PostID, err)
		RespondWithError(w, http.StatusForbidden, "post not found or you are not the author")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "post deleted"})
}

// EditPostHandler handles POST /post/edit
// Body: { "user_id": "...", "post_id": "...", "content": "..." }
// Only the post author may edit; requires a valid Authorization JWT.
func EditPostHandler(w http.ResponseWriter, r *http.Request) {
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
		RespondWithError(w, http.StatusBadRequest, "missing user_id, post_id, or content")
		return
	}

	// Verify the JWT belongs to the claimed user.
	authHeader2 := r.Header.Get("Authorization")
	parts2 := strings.Split(authHeader2, " ")
	if len(parts2) != 2 || parts2[0] != "Bearer" {
		RespondWithError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	claims, err := ValidateUserToken(parts2[1])
	if err != nil || claims.UserID != req.UserID {
		RespondWithError(w, http.StatusForbidden, "not authorized")
		return
	}

	internalUserID := ToInternalID(req.UserID)

	if err := EditPost(req.PostID, internalUserID, req.Content); err != nil {
		log.Printf("EditPost error user=%s post=%s: %v", internalUserID, req.PostID, err)
		RespondWithError(w, http.StatusForbidden, "post not found or you are not the author")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "post updated"})
}

// parseExpiryShorthand converts "1h", "6h", "12h", "24h", "3d", "7d" into a time.Duration.
func parseExpiryShorthand(s string) (time.Duration, error) {
	switch s {
	case "1h":
		return time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "3d":
		return 72 * time.Hour, nil
	case "7d":
		return 168 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// StartEphemeralPostSweeper runs a background goroutine that deletes expired posts every 5 minutes.
func StartEphemeralPostSweeper() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			res, err := db.Exec(`DELETE FROM posts WHERE expires_at IS NOT NULL AND expires_at < NOW()`)
			if err != nil {
				log.Printf("EphemeralPostSweeper: error deleting expired posts: %v", err)
				continue
			}
			if n, _ := res.RowsAffected(); n > 0 {
				log.Printf("EphemeralPostSweeper: deleted %d expired post(s)", n)
			}
		}
	}()
}
