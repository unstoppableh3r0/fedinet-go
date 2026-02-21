package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type UserNotification struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient_id"`
	Actor     string    `json:"actor_id"`
	Type      string    `json:"type"`
	EntityID  string    `json:"entity_id"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`

	ActorName      string          `json:"actor_name"`
	ActorAvatar    string          `json:"actor_avatar,omitempty"`
	ActivityStream json.RawMessage `json:"activity_stream,omitempty"`
}

func GetNotificationsHandler(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(`
		SELECT n.id, n.recipient_id, n.actor_id, n.type, n.entity_id, n.is_read, n.created_at,
		       p.display_name, p.avatar_url, n.activity_stream
		FROM notifications n
		LEFT JOIN profiles p ON n.actor_id = p.user_id
		WHERE n.recipient_id = $1
		ORDER BY n.created_at DESC
		LIMIT 50
	`, internalUserID)
	if err != nil {
		log.Println("Failed to fetch notifications:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch notifications")
		return
	}
	defer rows.Close()

	var notifications []UserNotification
	for rows.Next() {
		var n UserNotification
		var displayName, avatarURL *string
		var activityStream []byte
		if err := rows.Scan(&n.ID, &n.Recipient, &n.Actor, &n.Type, &n.EntityID, &n.IsRead, &n.CreatedAt, &displayName, &avatarURL, &activityStream); err != nil {
			log.Println("Error scanning notification:", err)
			continue
		}

		n.Recipient = ToExternalID(n.Recipient)
		n.Actor = ToExternalID(n.Actor)
		if displayName != nil {
			n.ActorName = *displayName
		} else {
			n.ActorName = n.Actor
		}
		if avatarURL != nil {
			n.ActorAvatar = *avatarURL
		}
		if len(activityStream) > 0 {
			n.ActivityStream = json.RawMessage(activityStream)
		}

		notifications = append(notifications, n)
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"notifications": notifications})
}

func MarkNotificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	internalUserID := ToInternalID(req.UserID)

	_, err := db.Exec("UPDATE notifications SET is_read = TRUE WHERE recipient_id = $1", internalUserID)
	if err != nil {
		log.Println("Failed to mark notifications read:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "marked as read"})
}
