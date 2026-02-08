package main

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Regex patterns
var (
	// Username: alphanumeric, underscores, 3-30 chars
	// We allow dots for flexibility but typically avoiding them for simplicity in some systems
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

	// Domain: standard simplified domain regex
	domainRegex = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$|^localhost(?::\d+)?$`)
)

// ValidateUsername checks if the local username part is valid
func ValidateUsername(username string) bool {
	return usernameRegex.MatchString(username)
}

// ValidateUserID checks if a full UserID (handle or URL) is valid
// For fedinet-go, UserID is expected to be "username@server" format internally?
// Or URL? "http://server/users/username"
// The prompt implies "user_name@server_name".
// Our ToInternalID / ToExternalID logic in handlers suggests we treat them somewhat interchangeably or normalize.
// Let's support the `user@domain` format as the canonical internal ID for this story.
func ValidateUserID(userID string) bool {
	parts := strings.Split(userID, "@")
	if len(parts) != 2 {
		return false
	}
	return ValidateUsername(parts[0]) && domainRegex.MatchString(parts[1])
}

// NormalizeUserID converts a UserID to a canonical format (lowercase)
func NormalizeUserID(userID string) string {
	return strings.ToLower(strings.TrimSpace(userID))
}

// ValidateIdentityDocument checks if an Identity struct is valid
func ValidateIdentityDocument(identity *Identity) bool {
	// 1. Check ID (UUID)
	if identity.ID == uuid.Nil {
		return false
	}

	// 2. Check UserID format
	if !ValidateUserID(identity.UserID) {
		return false
	}

	// 3. Check Public Key presence
	if strings.TrimSpace(identity.PublicKey) == "" {
		return false
	}

	// 4. Check Home Server URL format (basic check)
	if !strings.HasPrefix(identity.HomeServer, "http") {
		return false
	}

	// 5. Check Creation Time
	if identity.CreatedAt.IsZero() {
		return false
	}

	return true
}
