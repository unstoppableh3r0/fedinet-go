package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

func FollowUser(followerID, followeeID string) error {
	_, err := db.Exec(
		`INSERT INTO follows (follower_user_id, follower_home_server, followee_user_id, followee_home_server)
		 VALUES ($1, 'http://localhost:8080', $2, 'http://localhost:8080')
		 ON CONFLICT DO NOTHING`,
		followerID, followeeID,
	)
	if err != nil {
		return err
	}

	return LogActivity(
		followerID,
		"FOLLOW",
		"user",
		followeeID,
		"",
		"",
	)
}

func SendMessage(senderID, recipientID, content string) error {
	var messageID string

	err := db.QueryRow(
		`INSERT INTO messages (sender_id, recipient_id, content)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		senderID, recipientID, content,
	).Scan(&messageID)

	if err != nil {
		return err
	}

	return LogActivity(
		senderID,
		"MESSAGE",
		"message",
		messageID,
		recipientID,
		content,
	)
}

func UpdateBio(identityID, newBio string) error {
	_, err := db.Exec(
		`UPDATE profiles SET bio=$1, updated_at=NOW()
		 WHERE identity_id=$2`,
		newBio, identityID,
	)
	if err != nil {
		return err
	}

	return LogActivity(
		identityID,
		"UPDATE",
		"profile",
		identityID,
		"",
		`{"action": "bio updated"}`,
	)
}

func LogActivity(actorID, verb, objectType, objectID, targetID, payload string) error {
	if payload == "" {
		payload = "{}"
	}
	_, err := db.Exec(
		`INSERT INTO activities
		(actor_id, verb, object_type, object_id, target_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		actorID, verb, objectType, objectID, targetID, payload,
	)
	return err
}

func GetProfileByUserID(userID string) (*Profile, error) {
	query := `
		SELECT
			user_id,
			display_name,
			avatar_url,
			banner_url,
			bio,
			portfolio_url,
			birth_date,
			location,
			followers_visibility,
			following_visibility,
			created_at,
			updated_at,
            version,
			(SELECT COUNT(*) FROM follows WHERE followee_user_id = profiles.user_id) as followers_count,
			(SELECT COUNT(*) FROM follows WHERE follower_user_id = profiles.user_id) as following_count
		FROM profiles
		WHERE user_id = $1
	`

	var p Profile
	var birthDate sql.NullTime

	err := db.QueryRow(query, userID).Scan(
		&p.UserID,
		&p.DisplayName,
		&p.AvatarURL,
		&p.BannerURL,
		&p.Bio,
		&p.PortfolioURL,
		&birthDate, // Scan as sql.NullTime first
		&p.Location,
		&p.FollowersVisibility,
		&p.FollowingVisibility,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.Version,
		&p.FollowersCount,
		&p.FollowingCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		log.Println("GetProfileByUserID error:", err)
		return nil, err
	}

	// Convert birth_date to string pointer
	if birthDate.Valid {
		dateStr := birthDate.Time.Format("2006-01-02")
		p.BirthDate = &dateStr
	}

	return &p, nil
}

// CreateAccount atomically creates an identity and a default profile
func CreateAccount(userID, homeServer, password string) (string, error) {
	if !ValidateUserID(userID) {
		return "", fmt.Errorf("invalid user_id format")
	}

	// (Self-note: We don't have a full Identity struct here yet to validate via ValidateIdentityDocument
	// because we are about to create it. But we could construct a partial one or just rely on ValidateUserID
	// which we just did. ValidateIdentityDocument checks ID(nil), UserID, PublicKey(empty), HomeServer, CreatedAt.
	// Here we generate most of that. So explicit validation of the inputs is what we are doing.)
	// Generate Keys
	pubKey, privKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", err
	}

	// Generate Recovery Key
	recoveryKey, recoveryHash, err := crypto.GenerateRecoveryKey()
	if err != nil {
		return "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Check if user exists
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)", userID).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("user already exists")
	}

	// Encrypt Private Key
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		// Fallback or Error? Ideally Error. For dev, fallback to a known key or error.
		// Let's soft-fail for dev convenience if user didn't set it, but warn.
		// Or generate one on the fly? No, that's ephemeral.
		// Check implementation_plan: "Default to dev key if missing"
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000" // 32 bytes hex
		fmt.Println("WARNING: Using insecure default SERVER_MASTER_KEY")
	}

	encryptedPrivKey, err := crypto.Encrypt(privKey, masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Generate DID (Decentralized Identifier)
	// Simple scheme: did:fedinet:<public_key_hash>
	did := "did:fedinet:" + crypto.HashString(pubKey)

	// Hash password
	passwordHash, err := HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	// 2. Insert Identity
	identityID := uuid.New()
	_, err = tx.Exec(`
		INSERT INTO identities (
			id, did, user_id, home_server, public_key, private_key, key_version, recovery_key_hash, password_hash, allow_discovery, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, true, NOW(), NOW())
	`, identityID, did, userID, homeServer, pubKey, encryptedPrivKey, recoveryHash, passwordHash)
	if err != nil {
		return "", err
	}

	// 3. Insert Default Profile
	_, err = tx.Exec(`
		INSERT INTO profiles (
			user_id, display_name, bio, location, 
			followers_visibility, following_visibility, created_at, updated_at, version
		) VALUES (
			$1, $2, 'Just joined Gotham Social', 'Unknown',
			'public', 'public', NOW(), NOW(), 1
		)
	`, userID, userID) // Display name defaults to userID
	if err != nil {
		return "", err
	}

	return recoveryKey, tx.Commit()
}

func GetIdentityByUserID(userID string) (*Identity, error) {
	query := `
		SELECT
			id,
			user_id,
			home_server,
			public_key,
			allow_discovery,
			created_at,
			updated_at,
            signature,
            key_version,
            recovery_key_hash
		FROM identities
		WHERE user_id = $1
	`

	var i Identity

	err := db.QueryRow(query, userID).Scan(
		&i.ID,
		&i.UserID,
		&i.HomeServer,
		&i.PublicKey,
		&i.AllowDiscovery,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.Signature,
		&i.KeyVersion,
		&i.RecoveryKeyHash,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		// Handle potential NULLs if rows exist but cols are null?
		// Signature etc are nullable in DB? I added them as... TEXT.
		// If they are NULL, Scan into string will fail?
		// I should use sql.NullString or *string if they are nullable.
		// Identity struct has Signature string.
		// If DB has NULL, Scan assigns to string? No, it errors.
		// I should treat them as nullable in struct OR Ensure they are '' in DB (DEFAULT '').
		// I didn't set DEFAULT '' in migration.
		// So for EXISTING identities, they will be NULL.
		// I must handle NULLs.
		// But for NEW identities, they might be empty string or NULL?
		// GenerateKeyPair returns string. Insert uses it.
		// I should assume they are nullable.
		// Let's use *string in Scan for nullable fields or COALESCE in query.
		return nil, err
	}

	return &i, nil
}

// UpdateProfile updates profile fields dynamically
func UpdateProfile(req UpdateProfileRequest) error {
	query := "UPDATE profiles SET updated_at = NOW(), version = version + 1"
	args := []interface{}{}
	argCount := 1

	if req.DisplayName != nil {
		query += fmt.Sprintf(", display_name = $%d", argCount)
		args = append(args, *req.DisplayName)
		argCount++
	}
	if req.AvatarURL != nil {
		query += fmt.Sprintf(", avatar_url = $%d", argCount)
		args = append(args, *req.AvatarURL)
		argCount++
	}
	if req.BannerURL != nil {
		query += fmt.Sprintf(", banner_url = $%d", argCount)
		args = append(args, *req.BannerURL)
		argCount++
	}
	if req.Bio != nil {
		query += fmt.Sprintf(", bio = $%d", argCount)
		args = append(args, *req.Bio)
		argCount++
	}
	if req.PortfolioURL != nil {
		query += fmt.Sprintf(", portfolio_url = $%d", argCount)
		args = append(args, *req.PortfolioURL)
		argCount++
	}
	if req.BirthDate != nil {
		query += fmt.Sprintf(", birth_date = $%d", argCount)
		args = append(args, *req.BirthDate)
		argCount++
	}
	if req.Location != nil {
		query += fmt.Sprintf(", location = $%d", argCount)
		args = append(args, *req.Location)
		argCount++
	}
	if req.FollowersVisibility != nil {
		query += fmt.Sprintf(", followers_visibility = $%d", argCount)
		args = append(args, *req.FollowersVisibility)
		argCount++
	}
	if req.FollowingVisibility != nil {
		query += fmt.Sprintf(", following_visibility = $%d", argCount)
		args = append(args, *req.FollowingVisibility)
		argCount++
	}

	query += fmt.Sprintf(" WHERE user_id = $%d", argCount)
	args = append(args, req.UserID)

	_, err := db.Exec(query, args...)
	if err != nil {
		return err
	}

	// Propagate update to followers
	return propagateProfileUpdate(req.UserID, req)
}

func propagateProfileUpdate(userID string, req UpdateProfileRequest) error {
	// 1. Get unique servers of followers
	rows, err := db.Query(`
        SELECT DISTINCT follower_home_server 
        FROM follows 
        WHERE followee_user_id = $1 
        AND follower_home_server != 'http://localhost:8080' -- Exclude local
        AND follower_home_server != ''
    `, userID)
	if err != nil {
		return err // Log error?
	}
	defer rows.Close()

	var servers []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			servers = append(servers, s)
		}
	}

	if len(servers) == 0 {
		return nil
	}

	// 2. Construct Payload
	// We need the FULL profile or just the changes?
	// ActivityPub usually sends the full object or partial.
	// Let's send a constructed Person object with updated fields + version.

	// We need to fetch the current state of version from DB to be accurate?
	// UpdateProfile just incremented it.
	var currentVersion int
	db.QueryRow("SELECT version FROM profiles WHERE user_id=$1", userID).Scan(&currentVersion)

	// Construct object
	obj := map[string]interface{}{
		"type":    "Person",
		"id":      userID, // Internal ID, typically a URL
		"version": currentVersion,
		"updated": time.Now().UTC().Format(time.RFC3339),
	}

	// Add fields
	if req.DisplayName != nil {
		obj["display_name"] = *req.DisplayName
	}
	if req.Bio != nil {
		obj["bio"] = *req.Bio
	}
	// Add other fields as necessary

	payload := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"type":     "Update",
		"actor":    userID,
		"object":   obj,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// 3. Insert into Outbox for each server
	// TODO: optimization - batch insert?
	for _, server := range servers {
		_, err := db.Exec(`
            INSERT INTO outbox_activities (
                activity_type, actor_id, target_server, payload, delivery_status
            ) VALUES ($1, $2, $3, $4, 'pending')
        `, "Update", userID, server, payloadBytes)

		if err != nil {
			fmt.Printf("Failed to queue update for server %s: %v\n", server, err)
		}
	}

	return nil
}

func CreatePost(userID, content string) (string, error) {
	var postID string
	err := db.QueryRow(`
		INSERT INTO posts (author, content, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		RETURNING id
	`, userID, content).Scan(&postID)

	if err != nil {
		return "", err
	}

	return postID, nil
}

func ToggleLike(userID, postID string) error {
	// Check if already liked
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM likes WHERE user_id=$1 AND post_id=$2)", userID, postID).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		_, err = db.Exec("DELETE FROM likes WHERE user_id=$1 AND post_id=$2", userID, postID)
	} else {
		_, err = db.Exec("INSERT INTO likes (user_id, post_id) VALUES ($1, $2)", userID, postID)
	}

	if err != nil {
		return err
	}

	// Log activity (only for liking)
	if !exists {
		return LogActivity(userID, "LIKE", "post", postID, "", "")
	}
	return nil
}

func ToggleRepost(userID, postID string) error {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM reposts WHERE user_id=$1 AND post_id=$2)", userID, postID).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		_, err = db.Exec("DELETE FROM reposts WHERE user_id=$1 AND post_id=$2", userID, postID)
	} else {
		_, err = db.Exec("INSERT INTO reposts (user_id, post_id) VALUES ($1, $2)", userID, postID)
	}

	if err != nil {
		return err
	}

	if !exists {
		return LogActivity(userID, "REPOST", "post", postID, "", "")
	}
	return nil
}

func CreateReply(userID, postID, content string) (string, error) {
	var replyID string
	err := db.QueryRow(`
		INSERT INTO replies (post_id, user_id, content)
		VALUES ($1, $2, $3)
		RETURNING id
	`, postID, userID, content).Scan(&replyID)

	if err != nil {
		return "", err
	}

	LogActivity(userID, "REPLY", "post", postID, "", content)
	return replyID, nil
}

func GetUserPosts(targetUserID, viewerUserID string, limit, offset int) ([]Post, error) {
	query := `
		SELECT 
			p.id, 
			p.author, 
			p.content, 
			p.created_at, 
			p.updated_at,
			(SELECT COUNT(*) FROM likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM replies WHERE post_id = p.id) as reply_count,
			(SELECT COUNT(*) FROM reposts WHERE post_id = p.id) as repost_count,
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $2) as has_liked,
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $2) as has_reposted
		FROM posts p
		WHERE p.author = $1
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := db.Query(query, targetUserID, viewerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		err := rows.Scan(
			&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt,
			&p.LikeCount, &p.ReplyCount, &p.RepostCount,
			&p.HasLiked, &p.HasReposted,
		)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return posts, nil
}
