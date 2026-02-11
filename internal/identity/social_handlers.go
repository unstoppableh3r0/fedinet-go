package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// GetFeedHandler returns posts from users that the current user follows
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

	// Convert to internal format
	internalUserID := ToInternalID(userID)

	// Get limit and offset for pagination
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20 // default
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

	// Get posts from followed users
	posts, err := GetFeedPosts(internalUserID, limit, offset)
	if err != nil {
		log.Printf("GetFeedHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch feed")
		return
	}

	// Convert author IDs to external format
	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts":  posts,
		"limit":  limit,
		"offset": offset,
	})
}

// GetFollowersHandler returns list of users following the specified user
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

	internalUserID := ToInternalID(userID)

	followers, err := GetFollowers(internalUserID)
	if err != nil {
		log.Printf("GetFollowersHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch followers")
		return
	}

	// Convert to external format
	for i := range followers {
		followers[i].Identity.UserID = ToExternalID(followers[i].Identity.UserID)
		followers[i].Profile.UserID = ToExternalID(followers[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"followers": followers,
		"count":     len(followers),
	})
}

// RemoveFollowerHandler handles unfollow action (called /follower/remove by frontend)
func RemoveFollowerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID     string `json:"user_id"`     // Wrapper around identity.user_id (the one doing the removal/unfollowing)
		FollowerID string `json:"follower_id"` // The one being removed/unfollowed
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Frontend sends: user_id (me), follower_id (them)
	// If this is "Remove Follower" (block them from following me), it's different from "Unfollow" (I stop following them).
	// The frontend button says "Remove". context: FollowersPage.
	// "Remove" usually means "Force them to unfollow me".
	// BUT, conventionally "Followers" list -> "Remove" means "Block/Remove follower".
	// "Following" list -> "Unfollow".
	// The frontend code: `handleRemoveFollower` in `FollowersPage`.
	// logic: `user_id: identity.user_id` (ME), `follower_id: userId` (THEM).
	// Backend needs to DELETE FROM follows WHERE follower=THEM AND followee=ME.

	if req.UserID == "" || req.FollowerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
		return
	}

	me := ToInternalID(req.UserID)
	them := ToInternalID(req.FollowerID)

	// Remove THEM from following ME
	// follower_user_id = THEM, followee_user_id = ME
	if err := UnfollowUser(them, me); err != nil {
		log.Println("Remove follower failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "follower removed"})
}

// UnfollowHandler handles unfollow action (called /unfollow by frontend)
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

	// Unfollow: Follower (ME) stops following Followee (THEM)
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
		ParentID *string `json:"parent_id"` // Optional
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" || req.PostID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "missing fields")
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

	// Convert UserIDs to external
	for i := range replies {
		replies[i].UserID = ToExternalID(replies[i].UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"replies": replies})
}

// GetFollowingHandler returns list of users that the specified user follows
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

	internalUserID := ToInternalID(userID)

	following, err := GetFollowing(internalUserID)
	if err != nil {
		log.Printf("GetFollowingHandler error for user %s: %v", internalUserID, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch following")
		return
	}

	// Convert to external format
	for i := range following {
		following[i].Identity.UserID = ToExternalID(following[i].Identity.UserID)
		following[i].Profile.UserID = ToExternalID(following[i].Profile.UserID)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"following": following,
		"count":     len(following),
	})
}

// GetConversationsHandler returns list of message conversations for a user
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

	// Convert IDs to external format
	for i := range conversations {
		conversations[i].Sender = ToExternalID(conversations[i].Sender)
		conversations[i].Receiver = ToExternalID(conversations[i].Receiver)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"conversations": conversations,
		"count":         len(conversations),
	})
}

// GetConversationMessagesHandler returns messages in a specific conversation
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

	// Convert IDs to external format
	for i := range messages {
		messages[i].Sender = ToExternalID(messages[i].Sender)
		messages[i].Receiver = ToExternalID(messages[i].Receiver)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}
