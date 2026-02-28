package main

import (
	"log"
	"net/http"

	"github.com/unstoppableh3r0/fedinet-go/internal/federation"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Signature, Date, Digest")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	log.Println("🚀 Starting Federation Service...")

	// Initialize database
	federation.InitDB()

	mux := http.NewServeMux()

	// Federation endpoints
	mux.HandleFunc("/federation/inbox", federation.InboxHandler)
	mux.HandleFunc("/federation/outbox", federation.OutboxHandler)
	mux.HandleFunc("/federation/send", federation.SendActivityHandler)
	mux.HandleFunc("/federation/acknowledgment", federation.AcknowledgmentHandler)
	mux.HandleFunc("/federation/capabilities", federation.CapabilitiesHandler)
	mux.HandleFunc("/federation/capabilities/discover", federation.DiscoverCapabilitiesHandler)
	mux.HandleFunc("/federation/discover", federation.DiscoverCapabilitiesHandler)
	mux.HandleFunc("/federation/health", federation.HealthHandler)
	mux.HandleFunc("/federation/blocked", federation.BlockedServersHandler)
	mux.HandleFunc("/federation/mode", federation.FederationModeHandler)
	mux.HandleFunc("/federation/rate-limits", federation.RateLimitsHandler)
	mux.HandleFunc("/federation/handshake", federation.HandshakeHandler)
	mux.HandleFunc("/federation/initiate-handshake", federation.InitiateHandshakeHandler)
	mux.HandleFunc("/federation/handshake/initiate", federation.InitiateHandshakeHandler)
	mux.HandleFunc("/api/moderation/logs", federation.GetReportsHandler)
	mux.HandleFunc("/federation/moderation/logs", federation.GetReportsHandler)
	mux.HandleFunc("/federation/moderation/reports", federation.GetReportsHandler)
	mux.HandleFunc("/federation/moderation/reports/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			federation.ResolveReportHandler(w, r)
		} else {
			federation.GetReportsHandler(w, r)
		}
	})
	// Signed request endpoints (signature verification middleware)
	mux.HandleFunc("/federation/signed/inbox", federation.VerifySignatureMiddleware(federation.InboxHandler))
	mux.HandleFunc("/federation/lookup", federation.VerifySignatureMiddleware(federation.HandleFederatedLookup))

	handler := corsMiddleware(mux)

	log.Println("✅ Federation service listening on :8081")
	if err := http.ListenAndServe(":8081", handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
