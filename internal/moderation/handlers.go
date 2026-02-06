package moderation

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// --------------------
// Abuse Reports
// --------------------

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

// --------------------
// Server Blacklisting
// --------------------

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
