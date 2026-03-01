package moderation

import "net/http"

func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	// Reports
	mux.HandleFunc("/reports", handler.SubmitReport)
	mux.HandleFunc("/moderation/reports", handler.ListPendingReports)
	mux.HandleFunc("/moderation/resolve", handler.ResolveReport)

	// Server blocking (admin)
	mux.HandleFunc("/servers/block", handler.BlockServer)

	// User blocking
	mux.HandleFunc("/users/block", handler.BlockUser)
	mux.HandleFunc("/users/unblock", handler.UnblockUser)
	mux.HandleFunc("/users/blocked", handler.ListBlockedUsers)
	mux.HandleFunc("/users/block/check", handler.CheckUserBlock)
}
