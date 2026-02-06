package main

import (
	"log"
	"strings"
)

// Default internal server realm
const InternalServerName = "localhost"

// ToExternalID converts a stored user ID (user@localhost) to a display ID (user@ServerName)
func ToExternalID(internalID string) string {
	config, err := GetServerConfig()
	if err != nil {
		log.Println("Error fetching server config:", err)
		return internalID // Fallback
	}

	log.Printf("DEBUG: ToExternalID Input: %s, Current Config: %s\n", internalID, config.ServerName)

	// If ID ends with @localhost, replace it
	suffix := "@" + InternalServerName
	if strings.HasSuffix(internalID, suffix) {
		return strings.TrimSuffix(internalID, suffix) + "@" + config.ServerName
	}

	return internalID
}

// ToInternalID converts a display ID (user@ServerName) back to stored ID (user@localhost)
// This allows finding the user in the database regardless of the current server name config
func ToInternalID(externalID string) string {
	config, err := GetServerConfig()
	if err != nil {
		log.Println("Error fetching server config:", err)
		return externalID // Fallback
	}

	suffix := "@" + config.ServerName
	if strings.HasSuffix(externalID, suffix) {
		return strings.TrimSuffix(externalID, suffix) + "@" + InternalServerName
	}

	// If the user didn't even provide a server suffix (e.g. searching "user"), assume it's for this server
	if !strings.Contains(externalID, "@") {
		return externalID + "@" + InternalServerName
	}

	return externalID
}
