package main

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
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
		"bio updated",
	)
}

func LikePost(actorID, postID string) error {
	return LogActivity(
		actorID,
		"LIKE",
		"post",
		postID,
		"",
		"",
	)
}

func LogActivity(actorID, verb, objectType, objectID, targetID, payload string) error {
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
			updated_at
		FROM profiles
		WHERE user_id = $1
	`

	var p Profile

	err := db.QueryRow(query, userID).Scan(
		&p.UserID,
		&p.DisplayName,
		&p.AvatarURL,
		&p.BannerURL,
		&p.Bio,
		&p.PortfolioURL,
		&p.BirthDate,
		&p.Location,
		&p.FollowersVisibility,
		&p.FollowingVisibility,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// CreateAccount atomically creates an identity and a default profile
func CreateAccount(userID, homeServer string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Check if user exists
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)", userID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("user already exists")
	}

	// 2. Insert Identity
	identityID := uuid.New()
	_, err = tx.Exec(`
		INSERT INTO identities (
			id, user_id, home_server, public_key, allow_discovery, created_at, updated_at
		) VALUES ($1, $2, $3, 'DEFAULT_KEY', true, NOW(), NOW())
	`, identityID, userID, homeServer)
	if err != nil {
		return err
	}

	// 3. Insert Default Profile
	_, err = tx.Exec(`
		INSERT INTO profiles (
			user_id, display_name, bio, location, 
			followers_visibility, following_visibility, created_at, updated_at
		) VALUES (
			$1, $2, 'Just joined Gotham Social', 'Unknown',
			'public', 'public', NOW(), NOW()
		)
	`, userID, userID) // Display name defaults to userID
	if err != nil {
		return err
	}

	return tx.Commit()
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
			updated_at
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
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &i, nil
}

// UpdateProfile updates profile fields dynamically
func UpdateProfile(req UpdateProfileRequest) error {
	query := "UPDATE profiles SET updated_at = NOW()"
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
	return err
}

// CreatePost inserts a new post into the database
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

// GetUserPosts retrieves all posts by a specific user
func GetUserPosts(userID string) ([]Post, error) {
	query := `
		SELECT id, author, content, created_at, updated_at
		FROM posts
		WHERE author = $1
		ORDER BY created_at DESC
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		err := rows.Scan(&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt)
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
