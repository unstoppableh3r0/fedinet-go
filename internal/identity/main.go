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

// StartTOTPPartialTokenSweeper runs every 15 minutes to delete used or expired
// rows from totp_partial_tokens, preventing unbounded table growth.
func StartTOTPPartialTokenSweeper() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	log.Println("TOTP partial token sweeper started")

	for range ticker.C {
		res, err := db.Exec(`
			DELETE FROM totp_partial_tokens
			WHERE used = TRUE OR expires_at < NOW()
		`)
		if err != nil {
			log.Printf("TOTP partial token sweep error: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("TOTP partial token sweep: removed %d expired/used rows", n)
		}
	}
}

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
