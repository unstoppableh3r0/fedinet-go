package identity

import (
	"log"
	"time"
)

// deliveryBackoff defines wait durations for successive delivery attempts.
// Attempt 1 → 5 s, 2 → 15 s, 3 → 60 s (total window ≈80 s before giving up).
var deliveryBackoff = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	60 * time.Second,
}

// withRetry calls fn up to len(deliveryBackoff)+1 times, sleeping between attempts.
// It logs each transient failure and returns the last error if all attempts fail.
func withRetry(label string, fn func() error) error {
	var err error
	for attempt := 0; attempt <= len(deliveryBackoff); attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt < len(deliveryBackoff) {
			wait := deliveryBackoff[attempt]
			log.Printf("[retry] %s attempt %d failed: %v — retrying in %s", label, attempt+1, err, wait)
			time.Sleep(wait)
		}
	}
	log.Printf("[retry] %s gave up after %d attempts: %v", label, len(deliveryBackoff)+1, err)
	return err
}
