package identity

import (
	"log"
	"os"
	"time"
)

// InternalServerName is the internal identifier used in user_ids (e.g., server_a, server_b)
var InternalServerName = func() string {
	serverID := os.Getenv("SERVER_ID")
	if serverID != "" {
		return serverID
	}
	return "localhost" // fallback for development
}()

// StartSessionKeyWorker runs periodically to rotate expired keys and cleanup old ones.
// Call this from the service entry point to start background key rotation.
func StartSessionKeyWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Println("Session key worker started")

	for range ticker.C {
		// Rotate expired keys
		if err := RotateExpiredKeys(); err != nil {
			log.Printf("Session key rotation error: %v", err)
		}

		// Cleanup old expired keys
		if err := CleanupExpiredKeys(); err != nil {
			log.Printf("Session key cleanup error: %v", err)
		}
	}
}
