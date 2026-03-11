package main

import (
	"log"
	"net/http"
	"time"

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
	log.Println("Starting Federation Service...")

	// Initialize database
	federation.InitDB()
	federation.ApplyMigrations()

	// ── Background workers ────────────────────────────────────────────────────
	// Retry worker: drain delivery_attempts every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := federation.ProcessRetryQueue(); err != nil {
				log.Printf("[retry-worker] error processing retry queue: %v", err)
			}
		}
	}()

	// Expiry worker: mark stale pending messages as expired every hour
	go func() {
		// Run once immediately on startup, then every hour
		if err := federation.ExpireOldMessages(); err != nil {
			log.Printf("[expiry-worker] error expiring old messages: %v", err)
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := federation.ExpireOldMessages(); err != nil {
				log.Printf("[expiry-worker] error expiring old messages: %v", err)
			}
		}
	}()

	// Health metrics worker: refresh instance_health stats every minute
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := federation.UpdateHealthMetrics(); err != nil {
				log.Printf("[health-worker] error updating health metrics: %v", err)
			}
		}
	}()

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

	// Signed request endpoints (signature verification middleware)
	mux.HandleFunc("/federation/signed/inbox", federation.VerifySignatureMiddleware(federation.InboxHandler))
	mux.HandleFunc("/federation/lookup", federation.VerifySignatureMiddleware(federation.HandleFederatedLookup))

	handler := corsMiddleware(mux)

	log.Println("Federation service listening on :8081")
	if err := http.ListenAndServe(":8081", handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
