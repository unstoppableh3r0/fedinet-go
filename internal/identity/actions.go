package identity

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

func FollowUser(followerID, followeeID string) error {
	localEndpoint := getLocalEndpoint()

	// Determine the followee's home server: if it's a federated user, look up
	// the trusted server endpoint; otherwise use the local endpoint.
	followeeHomeServer := localEndpoint
	isFed, followeeServerID := IsFederatedUser(followeeID)
	if isFed {
		if ts, err := GetTrustedServer(followeeServerID); err == nil {
			followeeHomeServer = ts.Endpoint
		}
	}

	_, err := db.Exec(
		`INSERT INTO follows (follower_user_id, follower_home_server, followee_user_id, followee_home_server)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`,
		followerID, localEndpoint, followeeID, followeeHomeServer,
	)
	if err != nil {
		return err
	}

	// Invalidate cache immediately so the updated lists are visible right away.
	invalidateFollowCaches(followerID, followeeID)

	if err := LogActivity(followerID, "FOLLOW", "user", followeeID, "", ""); err != nil {
		return err
	}

	// Propagate the follow relationship to the remote server's DB so their
	// /followers endpoint returns the correct list.
	if isFed {
		go DeliverFederatedFollow(followerID, followeeID, "follow")
	}

	return CreateNotification(followeeID, followerID, "FOLLOW", followeeID)
}

func UnfollowUser(followerID, followeeID string) error {
	_, err := db.Exec(
		`DELETE FROM follows
		 WHERE follower_user_id = $1 AND followee_user_id = $2`,
		followerID, followeeID,
	)
	if err != nil {
		return err
	}

	// Invalidate cache so lists are accurate immediately.
	invalidateFollowCaches(followerID, followeeID)

	if err := LogActivity(followerID, "UNFOLLOW", "user", followeeID, "", ""); err != nil {
		return err
	}

	// Propagate the unfollow to the remote server's DB.
	if isFed, _ := IsFederatedUser(followeeID); isFed {
		go DeliverFederatedFollow(followerID, followeeID, "unfollow")
		// Also send a notification so the followee knows they were unfollowed.
		go CreateNotification(followeeID, followerID, "UNFOLLOW", followeeID) //nolint:errcheck
	}

	return nil
}

func SendMessage(senderID, recipientID, content string, imageURL *string) error {
	var messageID string

	err := db.QueryRow(
		`INSERT INTO messages (sender_id, recipient_id, content, image_url)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		senderID, recipientID, content, imageURL,
	).Scan(&messageID)

	if err != nil {
		return err
	}

	payload := fmt.Sprintf(`{"content": %q}`, content)
	if logErr := LogActivity(senderID, "MESSAGE", "message", messageID, recipientID, payload); logErr != nil {
		log.Printf("Warning: failed to log message activity: %v", logErr)
	}

	// Notify recipient with full AS2 payload
	CreateNotificationWithExtras(recipientID, senderID, "MESSAGE", messageID, map[string]interface{}{
		"content": content,
		"to":      recipientID,
	})
	return nil
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

func GetProfileByUserID(userID string) (*models.Profile, error) {
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

	var p models.Profile
	var birthDate sql.NullTime

	err := db.QueryRow(query, userID).Scan(
		&p.UserID,
		&p.DisplayName,
		&p.AvatarURL,
		&p.BannerURL,
		&p.Bio,
		&p.PortfolioURL,
		&birthDate,
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

	if birthDate.Valid {
		t := birthDate.Time
		p.BirthDate = &t
	}

	return &p, nil
}

func CreateAccount(userID, homeServer, passwordHash string) (string, error) {
	if !ValidateUserID(userID) {
		return "", fmt.Errorf("invalid user_id format")
	}

	pubKey, privKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", err
	}

	recoveryKey, recoveryHash, err := crypto.GenerateRecoveryKey()
	if err != nil {
		return "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)", userID).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("user already exists")
	}

	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
		fmt.Println("WARNING: Using insecure default SERVER_MASTER_KEY")
	}

	encryptedPrivKey, err := crypto.Encrypt(privKey, masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt private key: %w", err)
	}

	did := "did:fedinet:" + crypto.HashString(pubKey)

	identityID := uuid.New()
	_, err = tx.Exec(`
		INSERT INTO identities (
			id, did, user_id, home_server, public_key, private_key, key_version, recovery_key_hash, password_hash, allow_discovery, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, true, NOW(), NOW())
	`, identityID, did, userID, homeServer, pubKey, encryptedPrivKey, recoveryHash, passwordHash)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(`
		INSERT INTO profiles (
			user_id, display_name, bio, location, 
			followers_visibility, following_visibility, created_at, updated_at, version
		) VALUES (
			$1, $2, 'Just joined Gotham Social', 'Unknown',
			'public', 'public', NOW(), NOW(), 1
		)
	`, userID, userID)
	if err != nil {
		return "", err
	}

	return recoveryKey, tx.Commit()
}

func GetIdentityByUserID(userID string) (*models.Identity, error) {
	query := `
		SELECT
			id,
			user_id,
			home_server,
			public_key,
			allow_discovery,
			created_at,
			updated_at,
            COALESCE(signature, '') as signature,
            key_version,
            COALESCE(recovery_key_hash, '') as recovery_key_hash
		FROM identities
		WHERE user_id = $1
	`

	var i models.Identity

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

		return nil, err
	}

	return &i, nil
}

func UpdateProfile(req models.UpdateProfileRequest) error {
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

	return propagateProfileUpdate(req.UserID, req)
}

func propagateProfileUpdate(userID string, req models.UpdateProfileRequest) error {
	// Deliver asynchronously so the HTTP response returns immediately.
	// PropagateProfileToTrustedServers fans out to every trusted server via
	// its own goroutines, so we just call it directly.
	go PropagateProfileToTrustedServers(userID, req)
	return nil
}

func CreatePost(userID, content string, imageURL *string) (string, error) {
	// Allow image-only posts by using a space sentinel when content is empty
	if content == "" {
		content = " "
	}
	var postID string
	err := db.QueryRow(`
		INSERT INTO posts (author, content, image_url, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id
	`, userID, content, imageURL).Scan(&postID)

	if err != nil {
		return "", err
	}

	return postID, nil
}

func ToggleLike(userID, postID string) error {

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

	if !exists {
		if err := LogActivity(userID, "LIKE", "post", postID, "", ""); err != nil {
			return err
		}
		var authorID string
		if err := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); err == nil {
			if authorID != userID {
				CreateNotification(authorID, userID, "LIKE", postID)
			}
		}
		return nil
	}
	// Unlike: also send an Undo/Like notification so clients can withdraw it
	var authorID string
	if err := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); err == nil {
		if authorID != userID {
			CreateNotification(authorID, userID, "UNLIKE", postID)
		}
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
		if err := LogActivity(userID, "REPOST", "post", postID, "", ""); err != nil {
			return err
		}
		// Notify original author
		var authorID string
		if qErr := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); qErr == nil {
			if authorID != userID {
				CreateNotification(authorID, userID, "REPOST", postID)
			}
		}
		return nil
	}
	return nil
}

func CreateReply(userID, postID, content string, parentID *string) (string, error) {
	var replyID string
	err := db.QueryRow(`
		INSERT INTO replies (post_id, user_id, content, parent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, postID, userID, content, parentID).Scan(&replyID)

	if err != nil {
		return "", err
	}

	payload := ""
	if parentID != nil {
		payload = fmt.Sprintf(`{"parent_id": "%s", "content": %q}`, *parentID, content)
	} else {
		payload = fmt.Sprintf(`{"content": %q}`, content)
	}

	LogActivity(userID, "REPLY", "post", postID, "", payload)

	// Build extras for the richer AS2 Create/Note payload
	replyExtras := map[string]interface{}{"content": content}
	if parentID != nil {
		replyExtras["parent_id"] = *parentID
	}

	var authorID string
	if err := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); err == nil {
		if authorID != userID {
			CreateNotificationWithExtras(authorID, userID, "REPLY", postID, replyExtras)
		}
	}

	if parentID != nil {
		var parentAuthorID string
		if err := db.QueryRow("SELECT user_id FROM replies WHERE id=$1", *parentID).Scan(&parentAuthorID); err == nil {
			if parentAuthorID != userID && parentAuthorID != authorID {
				CreateNotificationWithExtras(parentAuthorID, userID, "REPLY", postID, replyExtras)
			}
		}
	}

	return replyID, nil
}

type Reply struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	ParentID  *string   `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
}

func GetPostReplies(postID string) ([]Reply, error) {
	rows, err := db.Query(`
		SELECT id, post_id, user_id, content, parent_id, created_at
		FROM replies
		WHERE post_id = $1
		ORDER BY created_at ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var replies []Reply
	for rows.Next() {
		var r Reply
		if err := rows.Scan(&r.ID, &r.PostID, &r.UserID, &r.Content, &r.ParentID, &r.CreatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, r)
	}
	return replies, nil
}

func GetUserPosts(targetUserID, viewerUserID string, limit, offset int) ([]models.Post, error) {
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
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $2) as has_reposted,
			p.image_url
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

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		err := rows.Scan(
			&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt,
			&p.LikeCount, &p.ReplyCount, &p.RepostCount,
			&p.HasLiked, &p.HasReposted, &p.ImageURL,
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

func GetRecentPosts(viewerUserID string, limit int) ([]models.Post, error) {
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
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) as has_liked,
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $1) as has_reposted,
			p.image_url
		FROM posts p
		ORDER BY p.created_at DESC
		LIMIT $2
	`

	rows, err := db.Query(query, viewerUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		err := rows.Scan(
			&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt,
			&p.LikeCount, &p.ReplyCount, &p.RepostCount,
			&p.HasLiked, &p.HasReposted, &p.ImageURL,
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

// UserReply is a reply made by a user, enriched with the parent post context.
type UserReply struct {
	ID          string    `json:"id"`
	PostID      string    `json:"post_id"`
	PostContent string    `json:"post_content"`
	PostAuthor  string    `json:"post_author"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetUserReplies returns replies written by userID, with the parent post included.
func GetUserReplies(userID string, limit, offset int) ([]UserReply, error) {
	rows, err := db.Query(`
		SELECT r.id, r.post_id,
		       COALESCE(p.content, '') AS post_content,
		       COALESCE(p.author,  '') AS post_author,
		       r.content, r.created_at
		FROM replies r
		LEFT JOIN posts p ON p.id = r.post_id
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var replies []UserReply
	for rows.Next() {
		var rep UserReply
		if err := rows.Scan(&rep.ID, &rep.PostID, &rep.PostContent, &rep.PostAuthor, &rep.Content, &rep.CreatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, rep)
	}
	return replies, rows.Err()
}

// GetUserLikedPosts returns posts liked by userID.
func GetUserLikedPosts(userID, viewerUserID string, limit, offset int) ([]models.Post, error) {
	rows, err := db.Query(`
		SELECT p.id, p.author, p.content, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM likes  WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM replies WHERE post_id = p.id) AS reply_count,
		       (SELECT COUNT(*) FROM reposts WHERE post_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes  WHERE post_id = p.id AND user_id = $2) AS has_liked,
		       EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $2) AS has_reposted
		FROM posts p
		INNER JOIN likes l ON l.post_id = p.id
		WHERE l.user_id = $1
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`, userID, viewerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(
			&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt,
			&p.LikeCount, &p.ReplyCount, &p.RepostCount,
			&p.HasLiked, &p.HasReposted,
		); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func CreateNotification(recipientID, actorID, typeStr, entityID string) error {
	return CreateNotificationWithExtras(recipientID, actorID, typeStr, entityID, nil)
}

// CreateNotificationWithExtras stores a notification and its full ActivityStreams 2.0 payload.
// extras can carry "content", "parent_id", "server_name", "to" for richer AS2 objects.
// If the recipient is on a different server the notification is delivered federatedly.
func CreateNotificationWithExtras(recipientID, actorID, typeStr, entityID string, extras map[string]interface{}) error {
	// Build the AS2 payload first (needed for both local and federated paths).
	as2Bytes, as2Err := BuildActivityStream(actorID, typeStr, entityID, extras)
	if as2Err != nil {
		log.Printf("Warning: could not build ActivityStream for %s/%s: %v", typeStr, entityID, as2Err)
	}

	// Detect cross-server recipients and deliver federatedly.
	if isFed, _ := IsFederatedUser(recipientID); isFed {
		go DeliverFederatedNotification(recipientID, actorID, typeStr, entityID, as2Bytes)
		return nil
	}

	// Local recipient — store in DB.
	if as2Err != nil {
		// Fall back: insert without activity_stream
		_, err := db.Exec(`
			INSERT INTO notifications (recipient_id, actor_id, type, entity_id, created_at)
			VALUES ($1, $2, $3, $4, NOW())
		`, recipientID, actorID, typeStr, entityID)
		return err
	}

	_, err := db.Exec(`
		INSERT INTO notifications (recipient_id, actor_id, type, entity_id, activity_stream, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, recipientID, actorID, typeStr, entityID, as2Bytes)
	return err
}
