package identity

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
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

	internalUserID := ToInternalID(userID)

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

	if err := UnfollowUser(ToInternalID(req.Follower), ToInternalID(req.Followee)); err != nil {
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

	internalUserID := ToInternalID(userID)

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

	messages, err := GetConversationMessages(ToInternalID(userID), ToInternalID(otherUserID))
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

// CreatePostHandler handles POST /post/create
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
	postID, err := CreatePost(ToInternalID(req.UserID), req.Content)
	if err != nil {
		log.Println("CreatePost error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	RespondWithJSON(w, http.StatusCreated, map[string]string{"post_id": postID})
}

// FollowHandler handles POST /follow
func FollowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		FollowerID string `json:"follower_id"`
		FolloweeID string `json:"followee_id"`
		Follower   string `json:"follower"`
		Followee   string `json:"followee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	followerID := req.FollowerID
	if followerID == "" {
		followerID = req.Follower
	}
	followeeID := req.FolloweeID
	if followeeID == "" {
		followeeID = req.Followee
	}
	if followerID == "" || followeeID == "" {
		RespondWithError(w, http.StatusBadRequest, "follower and followee required")
		return
	}
	if err := FollowUser(ToInternalID(followerID), ToInternalID(followeeID)); err != nil {
		log.Println("FollowUser error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to follow user")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "followed"})
}

// GetUserMeHandler handles GET /user/me?user_id=
func GetUserMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	internalID := ToInternalID(userID)
	ident, err := GetIdentityByUserID(internalID)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	profile, _ := GetProfileByUserID(internalID)
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"identity": ident,
		"profile":  profile,
	})
}

// GetUserSearchHandler handles GET /user/search?user_id=
func GetUserSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.URL.Query().Get("q")
	}
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	internalID := ToInternalID(userID)
	ident, err := GetIdentityByUserID(internalID)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	profile, _ := GetProfileByUserID(internalID)
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"identity": ident,
		"profile":  profile,
	})
}

// GetUserPostsHandler handles GET /posts/user?user_id=&viewer_id=
func GetUserPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	viewerID := r.URL.Query().Get("viewer_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	posts, err := GetUserPosts(ToInternalID(userID), ToInternalID(viewerID), limit, offset)
	if err != nil {
		log.Println("GetUserPosts error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get posts")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"posts": posts,
		"count": len(posts),
	})
}

// LikePostHandler handles POST /post/like
func LikePostHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Println("ToggleLike error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to like post")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "toggled"})
}

// RepostHandler handles POST /post/repost
func RepostHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Println("ToggleRepost error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to repost")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "toggled"})
}

// PostReplyHandler handles POST /post/reply
func PostReplyHandler(w http.ResponseWriter, r *http.Request) {
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
		RespondWithError(w, http.StatusBadRequest, "user_id, post_id and content required")
		return
	}
	replyID, err := CreatePost(ToInternalID(req.UserID), req.Content)
	if err != nil {
		log.Println("PostReply error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to create reply")
		return
	}
	RespondWithJSON(w, http.StatusCreated, map[string]string{"reply_id": replyID})
}

// GetPostRepliesAltHandler handles GET /post/replies?post_id=
func GetPostRepliesAltHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Println("GetPostReplies error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get replies")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"replies": replies, "count": len(replies)})
}

// SendMessageHandler handles POST /message — accepts {from, to} or {sender_id, recipient_id}
func SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		SenderID    string `json:"sender_id"`
		RecipientID string `json:"recipient_id"`
		From        string `json:"from"`
		To          string `json:"to"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	senderID := req.SenderID
	if senderID == "" {
		senderID = req.From
	}
	recipientID := req.RecipientID
	if recipientID == "" {
		recipientID = req.To
	}
	if senderID == "" || recipientID == "" || req.Content == "" {
		RespondWithError(w, http.StatusBadRequest, "sender, recipient and content required")
		return
	}
	if err := SendMessage(ToInternalID(senderID), ToInternalID(recipientID), req.Content); err != nil {
		log.Println("SendMessage error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "sent"})
}

// GetMessagesHandler handles GET /messages?user_id=
func GetMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	convos, err := GetConversations(ToInternalID(userID))
	if err != nil {
		log.Println("GetMessages error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"conversations": convos, "count": len(convos)})
}

// GetMessagesConversationHandler handles GET /messages/conversation?user_id=&other_user_id=
func GetMessagesConversationHandler(w http.ResponseWriter, r *http.Request) {
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
	msgs, err := GetConversationMessages(ToInternalID(userID), ToInternalID(otherUserID))
	if err != nil {
		log.Println("GetConversationMessages error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get conversation")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs, "count": len(msgs)})
}

// GetRecentPostsHandler handles GET /posts/recent?limit=&viewer_id=
func GetRecentPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	viewerID := r.URL.Query().Get("viewer_id")
	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	posts, err := GetRecentPosts(ToInternalID(viewerID), limit)
	if err != nil {
		log.Println("GetRecentPosts error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get recent posts")
		return
	}
	if posts == nil {
		posts = []models.Post{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"posts": posts, "count": len(posts)})
}

// GetSuggestedUsersHandler handles GET /users/suggested?user_id=&limit=
func GetSuggestedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	users, err := GetExploreSuggestedUsers(ToInternalID(userID), limit)
	if err != nil {
		log.Println("GetSuggestedUsers error:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to get suggested users")
		return
	}
	if users == nil {
		users = []models.UserDocument{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"users": users, "count": len(users)})
}
