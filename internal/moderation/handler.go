package moderation

import (
	"encoding/json"
	"net/http"
)

// Handler is the HTTP handler layer for the moderation service.
type Handler struct {
	service *Service
}

// NewHandler creates a new moderation HTTP handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetModerationQueue returns posts pending moderation review.
// GET /moderation/queue
func (h *Handler) GetModerationQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	queue, err := h.service.GetModerationQueue()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch moderation queue")
		return
	}

	if queue == nil {
		queue = []map[string]interface{}{}
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"queue": queue,
		"count": len(queue),
	})
}

// ApproveContent approves a flagged post and makes it publicly visible.
// POST /moderation/approve
func (h *Handler) ApproveContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ContentID   string `json:"content_id"`
		ModeratorID string `json:"moderator_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ContentID == "" {
		respondWithError(w, http.StatusBadRequest, "content_id is required")
		return
	}
	if err := h.service.UpdateReviewStatus(req.ContentID, "APPROVED"); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to approve content")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{
		"status":     "approved",
		"content_id": req.ContentID,
	})
}

// RejectContent rejects a flagged post and permanently hides it.
// POST /moderation/reject
func (h *Handler) RejectContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ContentID   string `json:"content_id"`
		ModeratorID string `json:"moderator_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ContentID == "" {
		respondWithError(w, http.StatusBadRequest, "content_id is required")
		return
	}
	if err := h.service.UpdateReviewStatus(req.ContentID, "REJECTED"); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to reject content")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{
		"status":     "rejected",
		"content_id": req.ContentID,
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}
