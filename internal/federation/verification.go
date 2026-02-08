package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// VerifyRequestSignature verifies the signature on an incoming InboxRequest
func VerifyRequestSignature(req InboxRequest) error {
	if req.Signature == nil || *req.Signature == "" {
		// For MVP, we might allow unsigned requests if FederationMode is 'soft'?
		// But user story emphasizes security.
		// Let's log warning and return error if strict.
		// But for now, enforce it.
		return fmt.Errorf("missing signature")
	}

	// 1. Validate Actor
	if req.Actor == "" {
		return fmt.Errorf("missing actor in request")
	}

	// 2. Resolve Actor Identity to get Public Key
	doc, err := ResolveAccount(req.Actor)
	if err != nil {
		return fmt.Errorf("failed to resolve actor identity: %w", err)
	}

	// 3. Serialize Payload for verification
	// Note: basic marshalling doesn't guarantee canonical ordering.
	// In production, use JWS or canonical JSON.
	payloadBytes, err := json.Marshal(req.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Debug logging for MVP dev
	log.Printf("Verifying signature for actor %s", req.Actor)
	// log.Printf("Payload: %s", string(payloadBytes))

	// 4. Verify Key Not Revoked
	// 4. Verify Key Not Revoked
	// We need to import the identity package helper if it's in a different package.
	// However, verification.go is in 'main' package (same as identity/revocation.go presumably?)
	// Yes, both are package main in the current file structure shown.
	revoked, err := IsKeyRevoked(doc.Identity.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to check revocation status: %w", err)
	}
	if revoked {
		return fmt.Errorf("public key is revoked")
	}

	// 5. Verify Signature
	valid, err := crypto.VerifySignature(payloadBytes, *req.Signature, doc.Identity.PublicKey)
	if err != nil {
		return fmt.Errorf("verification error: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// IsKeyRevoked checks if a public key has been revoked (Duplicated from identity service for now)
func IsKeyRevoked(keyID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM key_revocations WHERE key_id=$1)", keyID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
