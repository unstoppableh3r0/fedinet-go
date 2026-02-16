package main

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)


func VerifyRequestSignature(req models.InboxRequest) error {
	if req.Signature == nil || *req.Signature == "" {
		
		
		
		
		return fmt.Errorf("missing signature")
	}

	
	if req.Actor == "" {
		return fmt.Errorf("missing actor in request")
	}

	
	doc, err := ResolveAccount(req.Actor)
	if err != nil {
		return fmt.Errorf("failed to resolve actor identity: %w", err)
	}

	
	
	
	payloadBytes, err := json.Marshal(req.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	
	log.Printf("Verifying signature for actor %s", req.Actor)
	

	
	
	
	
	
	revoked, err := IsKeyRevoked(doc.Identity.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to check revocation status: %w", err)
	}
	if revoked {
		return fmt.Errorf("public key is revoked")
	}

	
	valid, err := crypto.VerifySignature(payloadBytes, *req.Signature, doc.Identity.PublicKey)
	if err != nil {
		return fmt.Errorf("verification error: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}

	return nil
}


func IsKeyRevoked(keyID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM key_revocations WHERE key_id=$1)", keyID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
