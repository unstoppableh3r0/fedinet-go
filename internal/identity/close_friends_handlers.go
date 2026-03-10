package identity

import (
	"encoding/json"
	"net/http"
)

// CloseFriendsHandler handles GET and POST for the close friends list.
//
//	GET  /close-friends?user_id=alice@server_a          → list alice's close friends
//	POST /close-friends {"user_id":"alice@server_a","friend_id":"bob@server_a"} → add
func CloseFriendsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listCloseFriends(w, r)
	case http.MethodPost:
		addCloseFriend(w, r)
	default:
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// RemoveCloseFriendHandler handles DELETE /close-friends/remove
//
//	DELETE /close-friends/remove {"user_id":"alice@server_a","friend_id":"bob@server_a"}
func RemoveCloseFriendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		UserID   string `json:"user_id"`
		FriendID string `json:"friend_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.FriendID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and friend_id required")
		return
	}
	userID := ToInternalID(req.UserID)
	friendID := ToInternalID(req.FriendID)

	if _, err := db.Exec(`DELETE FROM close_friends WHERE user_id = $1 AND friend_id = $2`, userID, friendID); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to remove close friend")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// listCloseFriends returns the caller's close friends list.
func listCloseFriends(w http.ResponseWriter, r *http.Request) {
	userID := ToInternalID(r.URL.Query().Get("user_id"))
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	rows, err := db.Query(`
		SELECT cf.friend_id, COALESCE(pr.display_name, cf.friend_id) AS display_name
		FROM close_friends cf
		LEFT JOIN profiles pr ON pr.user_id = cf.friend_id
		WHERE cf.user_id = $1
		ORDER BY cf.created_at DESC
	`, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch close friends")
		return
	}
	defer rows.Close()

	type entry struct {
		FriendID    string `json:"friend_id"`
		DisplayName string `json:"display_name"`
	}
	var friends []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.FriendID, &e.DisplayName); err != nil {
			continue
		}
		e.FriendID = ToExternalID(e.FriendID)
		friends = append(friends, e)
	}
	if friends == nil {
		friends = []entry{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"friends": friends})
}

// addCloseFriend adds a follower to the user's close friends list.
// The candidate must already follow the user (mutual awareness check).
func addCloseFriend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id"`   // list owner
		FriendID string `json:"friend_id"` // follower to add
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.FriendID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id and friend_id required")
		return
	}
	userID := ToInternalID(req.UserID)
	friendID := ToInternalID(req.FriendID)

	// Cannot add yourself
	if userID == friendID {
		RespondWithError(w, http.StatusBadRequest, "cannot add yourself as a close friend")
		return
	}

	// The friend_id must follow the user (i.e. be a follower of user_id)
	var isFollower bool
	if err := db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM follows WHERE follower_user_id = $1 AND followee_user_id = $2)
	`, friendID, userID).Scan(&isFollower); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	if !isFollower {
		RespondWithError(w, http.StatusBadRequest, "user must be your follower to be added as a close friend")
		return
	}

	_, err := db.Exec(`
		INSERT INTO close_friends (user_id, friend_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT DO NOTHING
	`, userID, friendID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to add close friend")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "added"})
}
