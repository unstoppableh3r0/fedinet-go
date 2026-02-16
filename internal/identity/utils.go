package main

import (
	"log"
	"strings"
)


const InternalServerName = "localhost"


func ToExternalID(internalID string) string {
	config, err := GetServerConfig()
	if err != nil {
		log.Println("Error fetching server config:", err)
		return internalID 
	}

	log.Printf("DEBUG: ToExternalID Input: %s, Current Config: %s\n", internalID, config.ServerName)

	
	suffix := "@" + InternalServerName
	if strings.HasSuffix(internalID, suffix) {
		return strings.TrimSuffix(internalID, suffix) + "@" + config.ServerName
	}

	return internalID
}



func ToInternalID(externalID string) string {
	config, err := GetServerConfig()
	if err != nil {
		log.Println("Error fetching server config:", err)
		return strings.ToLower(externalID) 
	}

	
	externalID = strings.ToLower(externalID)

	suffix := "@" + config.ServerName
	if strings.HasSuffix(externalID, suffix) {
		return strings.TrimSuffix(externalID, suffix) + "@" + InternalServerName
	}

	
	if !strings.Contains(externalID, "@") {
		return externalID + "@" + InternalServerName
	}

	return externalID
}
