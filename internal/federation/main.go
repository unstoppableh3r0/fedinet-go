package main

import (
	"log"
	"net/http"
	"time"
)

var startTime time.Time

func main() {
	startTime = time.Now()

	
	InitDB()

	
	go retryWorker()
	go expirationWorker()
	go healthWorker()

	
	registerHandlers()

	log.Println("Federation server starting on :8081")
	log.Println("Endpoints:")
	log.Println("  POST   /federation/inbox          - Receive incoming activities")
	log.Println("  GET    /federation/outbox         - Retrieve outgoing activities")
	log.Println("  POST   /federation/send           - Send an activity to remote server")
	log.Println("  POST   /federation/ack            - Receive delivery acknowledgments")
	log.Println("  GET    /federation/capabilities   - Get server capabilities")
	log.Println("  POST   /federation/discover       - Discover remote capabilities")
	log.Println("  GET    /federation/health         - Get instance health status")
	log.Println("  POST   /federation/handshake      - Receive handshake from remote server")
	log.Println("  POST   /federation/handshake/init - Initiate handshake with remote server")
	log.Println("  GET    /federation/admin/blocks   - List blocked servers")
	log.Println("  POST   /federation/admin/blocks   - Block a server")
	log.Println("  DELETE /federation/admin/blocks   - Unblock a server")
	log.Println("  GET    /federation/admin/mode     - Get federation mode")
	log.Println("  PUT    /federation/admin/mode     - Set federation mode")
	log.Println("  POST   /federation/admin/limits   - Configure rate limits")

	
	log.Fatal(http.ListenAndServe(":8081", enableCORS(http.DefaultServeMux)))
}

func registerHandlers() {
	
	http.HandleFunc("/federation/inbox", VerifySignatureMiddleware(InboxHandler))
	http.HandleFunc("/federation/outbox", OutboxHandler)
	http.HandleFunc("/federation/send", SendActivityHandler)
	http.HandleFunc("/federation/ack", AcknowledgmentHandler)

	
	http.HandleFunc("/federation/capabilities", CapabilitiesHandler)
	http.HandleFunc("/federation/discover", DiscoverCapabilitiesHandler)

	
	http.HandleFunc("/federation/lookup", VerifySignatureMiddleware(HandleFederatedLookup))

	
	http.HandleFunc("/federation/health", HealthHandler)

	
	http.HandleFunc("/federation/handshake", HandshakeHandler)
	http.HandleFunc("/federation/handshake/initiate", InitiateHandshakeHandler)

	
	http.HandleFunc("/federation/admin/blocks", BlockedServersHandler)
	http.HandleFunc("/federation/admin/mode", FederationModeHandler)
	http.HandleFunc("/federation/admin/limits", RateLimitsHandler)
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}






func retryWorker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("Retry worker started")

	for range ticker.C {
		err := ProcessRetryQueue()
		if err != nil {
			log.Printf("Retry worker error: %v", err)
		}
	}
}


func expirationWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	log.Println("Expiration worker started")

	for range ticker.C {
		err := ExpireOldMessages()
		if err != nil {
			log.Printf("Expiration worker error: %v", err)
		} else {
			log.Println("Expired old messages")
		}
	}
}


func healthWorker() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("Health worker started")

	for range ticker.C {
		
		uptime := int64(time.Since(startTime).Seconds())
		db.Exec(`
			UPDATE instance_health
			SET uptime_seconds = $1
			WHERE id = (SELECT id FROM instance_health ORDER BY created_at LIMIT 1)
		`, uptime)

		
		err := UpdateHealthMetrics()
		if err != nil {
			log.Printf("Health worker error: %v", err)
		}
	}
}
