package identity

import (
	"log"
	"strings"
)

func ToExternalID(internalID string) string {
	config, err := GetServerConfig()
	if err != nil {
		log.Println("Error fetching server config:", err)
		return internalID
	}

	log.Printf("DEBUG: ToExternalID Input: %s, Current Config: %s\n", internalID, config.ServerName)

	// Convert from internal format (username@server_id) to external (username@server_name)
	suffix := "@" + InternalServerName
	if strings.HasSuffix(internalID, suffix) {
		return strings.TrimSuffix(internalID, suffix) + "@" + config.ServerName
	}

	return internalID
}

func ToInternalID(externalID string) string {
	// Normalize to lowercase
	externalID = strings.ToLower(externalID)

	// If no @ sign, append the internal server identifier
	if !strings.Contains(externalID, "@") {
		return externalID + "@" + InternalServerName
	}

	// If the domain part matches the configured server name (e.g. "server a"),
	// replace it with the internal server identifier (e.g. "server_a").
	// This handles the round-trip: ToExternalID produces "alice@Server A",
	// and ToInternalID must convert that back to "alice@server_a".
	atIdx := strings.LastIndex(externalID, "@")
	domain := externalID[atIdx+1:] // already lowercased
	if config, err := GetServerConfig(); err == nil {
		configuredName := strings.ToLower(config.ServerName)
		if domain == configuredName && configuredName != strings.ToLower(InternalServerName) {
			return externalID[:atIdx+1] + InternalServerName
		}
	}

	return externalID
}
