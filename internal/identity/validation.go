package main

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)


var (
	
	
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

	
	domainRegex = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$|^localhost(?::\d+)?$`)
)


func ValidateUsername(username string) bool {
	return usernameRegex.MatchString(username)
}







func ValidateUserID(userID string) bool {
	parts := strings.Split(userID, "@")
	if len(parts) != 2 {
		return false
	}
	return ValidateUsername(parts[0]) && domainRegex.MatchString(parts[1])
}


func NormalizeUserID(userID string) string {
	return strings.ToLower(strings.TrimSpace(userID))
}


func ValidateIdentityDocument(identity *models.Identity) bool {
	
	if identity.ID == uuid.Nil {
		return false
	}

	
	if !ValidateUserID(identity.UserID) {
		return false
	}

	
	if strings.TrimSpace(identity.PublicKey) == "" {
		return false
	}

	
	if !strings.HasPrefix(identity.HomeServer, "http") {
		return false
	}

	
	if identity.CreatedAt.IsZero() {
		return false
	}

	return true
}
