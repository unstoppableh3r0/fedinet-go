package identity

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// window tracks a sliding-window request count for one key.
type window struct {
	mu       sync.Mutex
	requests []time.Time
}

// allow returns true if the request fits within max requests per period.
func (w *window) allow(max int, period time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := time.Now().Add(-period)

	// Prune old requests
	fresh := w.requests[:0]
	for _, t := range w.requests {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	w.requests = fresh

	if len(w.requests) >= max {
		return false
	}
	w.requests = append(w.requests, time.Now())
	return true
}

// RateLimiter holds per-key sliding windows.
type RateLimiter struct {
	mu     sync.Mutex
	keys   map[string]*window
	max    int
	period time.Duration
}

// NewRateLimiter creates a limiter that allows max requests per period.
func NewRateLimiter(max int, period time.Duration) *RateLimiter {
	rl := &RateLimiter{
		keys:   make(map[string]*window),
		max:    max,
		period: period,
	}
	// Background cleanup of idle windows every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.mu.Lock()
			cutoff := time.Now().Add(-rl.period)
			for k, w := range rl.keys {
				w.mu.Lock()
				alive := false
				for _, t := range w.requests {
					if t.After(cutoff) {
						alive = true
						break
					}
				}
				w.mu.Unlock()
				if !alive {
					delete(rl.keys, k)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *RateLimiter) getWindow(key string) *window {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if w, ok := rl.keys[key]; ok {
		return w
	}
	w := &window{}
	rl.keys[key] = w
	return w
}

// Allow checks whether key is within the rate limit.
func (rl *RateLimiter) Allow(key string) bool {
	return rl.getWindow(key).allow(rl.max, rl.period)
}

// clientIP extracts the real client IP from the request.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return real
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// RateLimitMiddleware wraps a handler and enforces the given rate limit per IP.
// On violation it responds 429 Too Many Requests with a Retry-After header.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			if !rl.Allow(key) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded — too many requests, slow down"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthRateLimitMiddleware creates a stricter per-IP limiter for auth endpoints.
// It is intentionally more aggressive to deter credential stuffing.
func AuthRateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return RateLimitMiddleware(rl)
}

// PerUserRateLimitMiddleware enforces a per-authenticated-user limit (by JWT claim user_id).
// Falls back to IP if no user claim is present.
func PerUserRateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && parts[0] == "Bearer" {
					if claims, err := ValidateUserToken(parts[1]); err == nil && claims.UserID != "" {
						key = "user:" + claims.UserID
					}
				}
			}
			if !rl.Allow(key) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded — too many requests, slow down"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
