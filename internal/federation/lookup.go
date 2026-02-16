package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)


type Identity struct {
	ID             string
	UserID         string
	HomeServer     string
	PublicKey      string
	AllowDiscovery bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Profile struct {
	UserID      string
	DisplayName string
	Bio         *string
	AvatarURL   *string
	BannerURL   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UserDocument struct {
	Identity Identity
	Profile  Profile
}


func ResolveAccount(handle string) (*UserDocument, error) {
	if handle == "" {
		return nil, fmt.Errorf("empty handle")
	}

	
	username, domain, err := parseHandle(handle)
	if err != nil {
		return nil, err
	}

	
	if isLocalDomain(domain) {
		return resolveLocalIdentity(username)
	}

	
	return resolveRemoteIdentity(username, domain)
}

func parseHandle(handle string) (string, string, error) {
	
	
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
	
	
	
	return domain == "" || domain == "localhost" || domain == "localhost:8080" || domain == "localhost:8081"
}

func resolveLocalIdentity(username string) (*UserDocument, error) {
	
	
	
	

	targetID := username
	if !strings.Contains(username, "@") {
		targetID = username + "@localhost" 
		
	}

	var i Identity

	err := db.QueryRow(`
		SELECT id, user_id, home_server, public_key, allow_discovery, created_at, updated_at
		FROM identities WHERE user_id = $1
	`, targetID).Scan(&i.ID, &i.UserID, &i.HomeServer, &i.PublicKey, &i.AllowDiscovery, &i.CreatedAt, &i.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			
			
			return nil, fmt.Errorf("identity not found")
		}
		return nil, err
	}
	
	var p Profile
	err = db.QueryRow(`
		SELECT user_id, display_name, bio, avatar_url, banner_url, created_at, updated_at
		FROM profiles WHERE user_id = $1
	`, i.UserID).Scan(&p.UserID, &p.DisplayName, &p.Bio, &p.AvatarURL, &p.BannerURL, &p.CreatedAt, &p.UpdatedAt)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch profile: %w", err)
	}

	
	doc := &UserDocument{
		Identity: i,
		Profile:  p,
	}
	return doc, nil
}

func resolveRemoteIdentity(username, domain string) (*UserDocument, error) {
	
	federatedID := username + "@" + domain

	
	var i Identity
	err := db.QueryRow(`
		SELECT id, user_id, home_server, public_key, allow_discovery, created_at, updated_at
		FROM identities WHERE user_id = $1
	`, federatedID).Scan(&i.ID, &i.UserID, &i.HomeServer, &i.PublicKey, &i.AllowDiscovery, &i.CreatedAt, &i.UpdatedAt)

	if err == nil {
		
		
		
		
		
		
		
		
		return &UserDocument{Identity: i, Profile: Profile{UserID: i.UserID, DisplayName: username}}, nil
	}

	
	
	
	
	

	
	
	if domain == "remote.com" || domain == "example.com" {
		log.Printf("Simulating remote fetch for %s", federatedID)

		
		
		newID := uuid.New()

		
		remotePub := "simulated-remote-public-key-for-" + federatedID

		_, err = db.Exec(`
            INSERT INTO identities (
                id, user_id, home_server, public_key, key_version, recovery_key_hash, allow_discovery, created_at, updated_at
            ) VALUES ($1, $2, $3, $4, 1, '', true, NOW(), NOW())
        `, newID, federatedID, "https://"+domain, remotePub)

		if err != nil {
			return nil, fmt.Errorf("failed to cache remote identity: %w", err)
		}

		
		return resolveRemoteIdentity(username, domain)
	}

	return nil, fmt.Errorf("remote user not found (fetch failed)")
}
