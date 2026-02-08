package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ResolveAccount finds an identity and profile by handle (local or remote)
func ResolveAccount(handle string) (*models.UserDocument, error) {
	if handle == "" {
		return nil, fmt.Errorf("empty handle")
	}

	// 1. Parse Handle
	username, domain, err := parseHandle(handle)
	if err != nil {
		return nil, err
	}

	// 2. Check if local (no domain or localhost)
	if isLocalDomain(domain) {
		return resolveLocalIdentity(username)
	}

	// 3. Remote Lookup
	return resolveRemoteIdentity(username, domain)
}

func parseHandle(handle string) (string, string, error) {
	// format: username@domain or username
	// remove leading @
	handle = strings.TrimPrefix(handle, "@")
	parts := strings.Split(handle, "@")
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("invalid handle format")
}

func isLocalDomain(domain string) bool {
	// check internal server name env var?
	// internal/identity uses "internal_server" const maybe?
	// For now hardcode localhost checking
	return domain == "" || domain == "localhost" || domain == "localhost:8080" || domain == "localhost:8081"
}

func resolveLocalIdentity(username string) (*models.UserDocument, error) {
	// Query local DB
	// We need to ensure we look up by the full federated ID if that's how it's stored.
	// In RegisterHandler, we store "username@localhost" (or whatever InternalServerName is).
	// So if 'username' is just "alice", we might fail.

	targetID := username
	if !strings.Contains(username, "@") {
		targetID = username + "@localhost" // Default to localhost for now
		// TODO: Load from config
	}

	var i models.Identity

	err := db.QueryRow(`
		SELECT id, user_id, home_server, public_key, allow_discovery, created_at, updated_at
		FROM identities WHERE user_id = $1
	`, targetID).Scan(&i.ID, &i.UserID, &i.HomeServer, &i.PublicKey, &i.AllowDiscovery, &i.CreatedAt, &i.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			// Try without domain if first attempt failed?
			// Or maybe the input WAS the full ID?
			return nil, fmt.Errorf("identity not found")
		}
		return nil, err
	}
	// Fetch Profile
	var p models.Profile
	err = db.QueryRow(`
		SELECT user_id, display_name, bio, avatar_url, banner_url, created_at, updated_at
		FROM profiles WHERE user_id = $1
	`, i.UserID).Scan(&p.UserID, &p.DisplayName, &p.Bio, &p.AvatarURL, &p.BannerURL, &p.CreatedAt, &p.UpdatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch profile: %w", err)
	}

	// Create UserDocument
	doc := &models.UserDocument{
		Identity: i,
		Profile:  p,
	}
	return doc, nil
}

func resolveRemoteIdentity(username, domain string) (*models.UserDocument, error) {
	// 1. Construct Federated ID
	federatedID := username + "@" + domain

	// 2. Check Cache (DB)
	var i models.Identity
	err := db.QueryRow(`
		SELECT id, user_id, home_server, public_key, allow_discovery, created_at, updated_at
		FROM identities WHERE user_id = $1
	`, federatedID).Scan(&i.ID, &i.UserID, &i.HomeServer, &i.PublicKey, &i.AllowDiscovery, &i.CreatedAt, &i.UpdatedAt)

	if err == nil {
		// Cache Hit
		// Check for Staleness (e.g., 24 hours)
		// For MVP, if it exists, valid enough.
		// logic: if time.Since(i.UpdatedAt) < 24 * time.Hour { return &i, nil }
		// Check keys for profile
		// Basic cache implementation
		// For now return identity with empty profile if not cached?
		// Or assume if identity is cached, we should try to fetch profile.
		return &models.UserDocument{Identity: i, Profile: models.Profile{UserID: i.UserID, DisplayName: username}}, nil
	}

	// 3. Cache Miss - Fetch Remote (Stub)
	// In a real implementation:
	// a. WebFinger lookup to get Actor URL
	// b. Fetch Actor JSON
	// c. Parse Public Key

	// Simulating Discovery of a valid remote user
	// We'll generate a dummy one for demonstration purposes if domain is "remote.com"
	if domain == "remote.com" || domain == "example.com" {
		log.Printf("Simulating remote fetch for %s", federatedID)

		// Generate new identity
		// We need a UUID
		newID := uuid.New()

		// Dummy/Simulated Key (In reality, we'd fetch this)
		remotePub := "simulated-remote-public-key-for-" + federatedID

		_, err = db.Exec(`
            INSERT INTO identities (
                id, user_id, home_server, public_key, key_version, recovery_key_hash, allow_discovery, created_at, updated_at
            ) VALUES ($1, $2, $3, $4, 1, '', true, NOW(), NOW())
        `, newID, federatedID, "https://"+domain, remotePub)

		if err != nil {
			return nil, fmt.Errorf("failed to cache remote identity: %w", err)
		}

		// Recurse to return the stored object locally
		return resolveRemoteIdentity(username, domain)
	}

	return nil, fmt.Errorf("remote user not found (fetch failed)")
}
