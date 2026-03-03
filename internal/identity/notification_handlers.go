package identity

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

	notifs, err := FetchNotifications(internalUserID)
	if err != nil {
		log.Println("Failed to fetch notifications from Redis:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch notifications")
		return
	}

	// Normalise IDs to external format for the frontend
	for i := range notifs {
		notifs[i].Recipient = ToExternalID(notifs[i].Recipient)
		notifs[i].Actor = ToExternalID(notifs[i].Actor)
		if notifs[i].ActorName == "" {
			notifs[i].ActorName = notifs[i].Actor
		}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"notifications": notifs})
}

func MarkNotificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID         string `json:"user_id"`
		NotificationID string `json:"notification_id,omitempty"` // optional — mark single
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

	var err error
	if req.NotificationID != "" {
		err = MarkSingleNotificationRead(internalUserID, req.NotificationID)
	} else {
		err = MarkAllNotificationsRead(internalUserID)
	}
	if err != nil {
		log.Println("Failed to mark notifications read:", err)
		RespondWithError(w, http.StatusInternalServerError, "action failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "marked as read"})
}
