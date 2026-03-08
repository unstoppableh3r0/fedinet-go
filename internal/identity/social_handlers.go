package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	aimoderation "github.com/unstoppableh3r0/fedinet-go/internal/ai-moderation-service"
)

func GetFeedHandler(w http.ResponseWriter, r *http.Request) {
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

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	posts, err := GetFeedPosts(internalUserID, limit, offset)
	if err != nil {
		log.Printf("GetFeedHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch feed")
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

func GetFollowersHandler(w http.ResponseWriter, r *http.Request) {
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

	internalUserID := ToInternalID(userID)
	internalViewerID := ToInternalID(viewerID)

	// Enforce followers list visibility
	ps := getPrivacySettingsForUser(internalUserID)
	if !canViewContent(ps.FollowersListVisibility, internalViewerID, internalUserID) {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"followers": []interface{}{},
			"count":     0,
		})
		return
	}

	followers, err := GetFollowers(internalUserID)
	if err != nil {
		log.Printf("GetFollowersHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch followers")
		return
	}

	for i := range followers {
		followers[i].Identity.UserID = ToExternalID(followers[i].Identity.UserID)
		followers[i].Profile.UserID = ToExternalID(followers[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"followers": followers,
		"count":     len(followers),
	})
}

func RemoveFollowerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID     string `json:"user_id"`
		FollowerID string `json:"follower_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.FollowerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	me := ToInternalID(req.UserID)
	them := ToInternalID(req.FollowerID)

	if err := UnfollowUser(them, me); err != nil {
		log.Println("Remove follower failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "follower removed"})
}

func UnfollowHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := UnfollowUser(internalFollower, internalFollowee); err != nil {
		log.Println("Unfollow failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "unfollowed"})
}

func CreateReplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID   string  `json:"user_id"`
		PostID   string  `json:"post_id"`
		Content  string  `json:"content"`
		ParentID *string `json:"parent_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.PostID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	// ⭐ STEP A — AI MODERATION
	aiResult, err := aimoderation.CallModerationAPI(req.Content)
	if err != nil {
		log.Println("AI moderation failed, continuing without AI:", err)

		// fallback safe response
		aiResult = &aimoderation.ModerationResponse{
			Toxicity:       0.0,
			Hate:           0.0,
			Spam:           0.0,
			Threat:         0.0,
			Confidence:     0.0,
			Recommendation: "SAFE",
		}
	}

	// ⭐ STEP B — BLOCK OR FLAG
	if aiResult.Recommendation != "SAFE" {
		RespondWithJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":     "reply_flagged",
			"moderation": aiResult,
		})
		return
	}

	replyID, err := CreateReply(ToInternalID(req.UserID), req.PostID, req.Content, req.ParentID)
	if err != nil {
		log.Println("Reply failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "reply failed")
		return
	}

	RespondWithJSON(w, http.StatusCreated, map[string]string{"reply_id": replyID})
}

func GetPostRepliesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	postID := r.URL.Query().Get("post_id")
	if postID == "" {
		RespondWithError(w, http.StatusBadRequest, "post_id required")
		return
	}

	replies, err := GetPostReplies(postID)
	if err != nil {
		log.Println("Failed to fetch replies:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch replies")
		return
	}

	for i := range replies {
		replies[i].UserID = ToExternalID(replies[i].UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"replies": replies})
}

func GetFollowingHandler(w http.ResponseWriter, r *http.Request) {
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

	internalUserID := ToInternalID(userID)
	internalViewerID := ToInternalID(viewerID)

	// Enforce following list visibility
	ps := getPrivacySettingsForUser(internalUserID)
	if !canViewContent(ps.FollowingListVisibility, internalViewerID, internalUserID) {
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"following": []interface{}{},
			"count":     0,
		})
		return
	}

	following, err := GetFollowing(internalUserID)
	if err != nil {
		log.Printf("GetFollowingHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch following")
		return
	}

	for i := range following {
		following[i].Identity.UserID = ToExternalID(following[i].Identity.UserID)
		following[i].Profile.UserID = ToExternalID(following[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"following": following,
		"count":     len(following),
	})
}

func GetConversationsHandler(w http.ResponseWriter, r *http.Request) {
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

	conversations, err := GetConversations(internalUserID)
	if err != nil {
		log.Printf("GetConversationsHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch conversations")
		return
	}

	for i := range conversations {
		conversations[i].Sender = ToExternalID(conversations[i].Sender)
		conversations[i].Receiver = ToExternalID(conversations[i].Receiver)
		conversations[i].OtherUser = ToExternalID(conversations[i].OtherUser)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"conversations": conversations,
		"count":         len(conversations),
	})
}

func GetConversationMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := r.URL.Query().Get("user_id")
	otherUserID := r.URL.Query().Get("other_user_id")

	if userID == "" || otherUserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and other_user_id required")
		return
	}

	internalUserID := ToInternalID(userID)
	internalOtherUserID := ToInternalID(otherUserID)

	messages, err := GetConversationMessages(internalUserID, internalOtherUserID)
	if err != nil {
		log.Printf("GetConversationMessagesHandler error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch messages")
		return
	}

	for i := range messages {
		messages[i].Sender = ToExternalID(messages[i].Sender)
		messages[i].Receiver = ToExternalID(messages[i].Receiver)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}
