package main

import (
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
