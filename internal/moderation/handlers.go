package moderation

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SubmitReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReporterID   string `json:"reporter_id"`
		TargetRef    string `json:"target_ref"`
		TargetServer string `json:"target_server"`
		Reason       string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.service.SubmitReport(
		req.ReporterID,
		req.TargetRef,
		req.TargetServer,
		req.Reason,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) ListPendingReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	reports, err := h.service.ListPendingReports()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if reports == nil {
		reports = []models.Report{}
	}
	json.NewEncoder(w).Encode(reports)
}

func (h *Handler) ResolveReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	reportIDStr := r.URL.Query().Get("id")
	resolvedBy := r.URL.Query().Get("resolved_by")

	reportID, err := strconv.ParseInt(reportIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid report id", http.StatusBadRequest)
		return
	}

	if err := h.service.ResolveReport(reportID, resolvedBy); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ─── USER BLOCK HANDLERS ─────────────────────────────────────────────────────

// POST /users/block
func (h *Handler) BlockUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BlockerUserID string `json:"blocker_user_id"`
		BlockedUserID string `json:"blocked_user_id"`
		Reason        string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.service.BlockUser(req.BlockerUserID, req.BlockedUserID, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "blocked"})
}

// POST /users/unblock
func (h *Handler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BlockerUserID string `json:"blocker_user_id"`
		BlockedUserID string `json:"blocked_user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.service.UnblockUser(req.BlockerUserID, req.BlockedUserID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "unblocked"})
}

// GET /users/blocked?blocker_user_id=...
func (h *Handler) ListBlockedUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	blockerUserID := r.URL.Query().Get("blocker_user_id")
	if blockerUserID == "" {
		http.Error(w, "blocker_user_id query param is required", http.StatusBadRequest)
		return
	}

	blocks, err := h.service.ListBlockedUsers(blockerUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"blocked_users": blocks})
}

// GET /users/block/check?blocker_user_id=...&blocked_user_id=...
func (h *Handler) CheckUserBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	blockerUserID := r.URL.Query().Get("blocker_user_id")
	blockedUserID := r.URL.Query().Get("blocked_user_id")

	if blockerUserID == "" || blockedUserID == "" {
		http.Error(w, "blocker_user_id and blocked_user_id query params are required", http.StatusBadRequest)
		return
	}

	isBlocked, err := h.service.IsUserBlocked(blockerUserID, blockedUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"is_blocked": isBlocked})
}

// ─── SERVER BLOCK HANDLER ─────────────────────────────────────────────────────

func (h *Handler) BlockServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain  string `json:"domain"`
		Reason  string `json:"reason"`
		AdminID string `json:"admin_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.service.BlockServer(req.Domain, req.Reason, req.AdminID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetModerationQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	results, err := h.service.GetModerationQueue()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (h *Handler) ApproveContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ContentID string `json:"content_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.UpdateReviewStatus(req.ContentID, "APPROVED")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"approved"}`))
}

func (h *Handler) RejectContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ContentID string `json:"content_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.UpdateReviewStatus(req.ContentID, "REJECTED")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"rejected"}`))
}
