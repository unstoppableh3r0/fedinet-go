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
			(SELECT COUNT(*) FROM follows WHERE followee_user_id = profiles.user_id) as followers_count,
			(SELECT COUNT(*) FROM follows WHERE follower_user_id = profiles.user_id) as following_count
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
		&p.FollowersCount,
		&p.FollowingCount,
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

// ToggleLike toggles the like status for a post
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

// ToggleRepost toggles the repost status for a post
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

// CreateReply creates a new reply
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

// GetUserPosts retrieves posts by a specific user with pagination and viewer state
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
