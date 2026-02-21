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
	// Just preserve the ID as-is and normalize to lowercase
	externalID = strings.ToLower(externalID)

	// If no @ sign, add the server name
	if !strings.Contains(externalID, "@") {
		return externalID + "@" + InternalServerName
	}

	return externalID
}
