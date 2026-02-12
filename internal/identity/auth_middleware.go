package main

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const userIDKey contextKey = "user_id"

// AuthMiddleware validates JWT access tokens and protects routes
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			RespondWithError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Check if it's a Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			RespondWithError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		tokenString := parts[1]

		// Validate access token
		claims, err := ValidateAccessToken(tokenString)
		if err != nil {
			log.Printf("Token validation failed: %v", err)
			RespondWithError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Add user ID to request context
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// GetUserIDFromContext extracts the user ID from the request context
func GetUserIDFromContext(r *http.Request) string {
	if userID, ok := r.Context().Value(userIDKey).(string); ok {
		return userID
	}
	return ""
}
