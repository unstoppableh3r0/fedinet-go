package moderation

import "net/http"

func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	mux.HandleFunc("/reports", handler.SubmitReport)
	mux.HandleFunc("/moderation/reports", handler.ListPendingReports)
	mux.HandleFunc("/moderation/resolve", handler.ResolveReport)
	mux.HandleFunc("/servers/block", handler.BlockServer)
}
