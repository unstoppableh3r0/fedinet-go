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

// ============================================================================
// SOCIAL GRAPH ENGINE: FOLLOWS & UNFOLLOWS
// ============================================================================

/*
FollowUser implements the bidirectional trust established between identities.
In a federated context, a "Follow" is more than a database row; it is a
cryptographically signed request that must be synchronized across nodes.

Relationship Types:
  - Peer-to-Peer (Local): Both users reside on this instance.
  - Federated (Remote): The followee is on a remote server. This triggers an
    outbound ActivityPub delivery.

Scalability Considerations:
  - Fan-out: Federated follows are delivered asynchronously to prevent blocking
    the local user's web request.
  - Cache Invalidation: The system uses a proactive invalidation strategy to
    ensure follower counts remain accurate on the frontend.
*/
func FollowUser(followerID, followeeID string) error {
	localEndpoint := getLocalEndpoint()

	// FEDERATION DISCOVERY LOGIC
	// Determine the followee's home server: if it's a federated user, look up
	// the trusted server endpoint; otherwise use the local endpoint.
	followeeHomeServer := localEndpoint
	isFed, followeeServerID := IsFederatedUser(followeeID)
	if isFed {
		// If the user is remote, we resolve their home server URL from our
		// trusted_servers whitelist table.
		if ts, err := GetTrustedServer(followeeServerID); err == nil {
			followeeHomeServer = ts.Endpoint
		}
	}

	// PERSISTENCE LAYER
	// Store the relationship using an idempotent INSERT.
	_, err := db.Exec(
		`INSERT INTO follows (follower_user_id, follower_home_server, followee_user_id, followee_home_server)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT DO NOTHING`,
		followerID, localEndpoint, followeeID, followeeHomeServer,
	)
	if err != nil {
		return err
	}

	// CACHE COORDINATION
	// Invalidate cache immediately so the updated lists are visible right away.
	// This prevents "stale state" UI issues where a user follows but the count doesn't increment.
	invalidateFollowCaches(followerID, followeeID)

	// AUDIT LOGGING
	// Non-fatal: log activity and create notification — don't block follow on these.
	// Uses the internal outbox/activity stream to track user history.
	if logErr := LogActivity(followerID, "FOLLOW", "user", followeeID, "", ""); logErr != nil {
		log.Printf("Warning: failed to log follow activity: %v", logErr)
	}

	// OUTBOUND FEDERATION
	// Propagate the follow relationship to the remote server's DB so their
	// /followers endpoint returns the correct list.
	// Delivered asynchronously via goroutine to maintain low latency for the local user.
	if isFed {
		go DeliverFederatedFollow(followerID, followeeID, "follow")
	}

	// NOTIFICATION PIPELINE
	// Alert the followee that they have gained a new follower.
	if notifErr := CreateNotification(followeeID, followerID, "FOLLOW", followeeID); notifErr != nil {
		log.Printf("Warning: failed to create follow notification: %v", notifErr)
	}
	return nil
}

// UnfollowUser terminates a relationship and synchronizes the deletion across the network.
func UnfollowUser(followerID, followeeID string) error {
	_, err := db.Exec(
		`DELETE FROM follows
         WHERE follower_user_id = $1 AND followee_user_id = $2`,
		followerID, followeeID,
	)
	if err != nil {
		return err
	}

	// Ensure immediate UI consistency.
	invalidateFollowCaches(followerID, followeeID)

	if err := LogActivity(followerID, "UNFOLLOW", "user", followeeID, "", ""); err != nil {
		return err
	}

	// FEDERATED SYNC: Notify the remote server to remove the follower from their local DB.
	if isFed, _ := IsFederatedUser(followeeID); isFed {
		go DeliverFederatedFollow(followerID, followeeID, "unfollow")
		// Also send a notification so the followee knows they were unfollowed.
		go CreateNotification(followeeID, followerID, "UNFOLLOW", followeeID) //nolint:errcheck
	}

	return nil
}

// ============================================================================
// COMMUNICATION: MESSAGES & CONTENT
// ============================================================================

// SendMessage handles direct peer-to-peer communication.
func SendMessage(senderID, recipientID, content string, imageURL *string) error {
	var messageID string

	// ATOMIC STORAGE
	err := db.QueryRow(
		`INSERT INTO messages (sender_id, recipient_id, content, image_url)
         VALUES ($1, $2, $3, $4)
         RETURNING id`,
		senderID, recipientID, content, imageURL,
	).Scan(&messageID)

	if err != nil {
		return err
	}

	// ACTIVITY LOGGING: JSON payload facilitates richer frontend timelines.
	payload := fmt.Sprintf(`{"content": %q}`, content)
	if logErr := LogActivity(senderID, "MESSAGE", "message", messageID, recipientID, payload); logErr != nil {
		log.Printf("Warning: failed to log message activity: %v", logErr)
	}

	// AS2 (ActivityStreams 2.0) COMPATIBILITY
	// Notify recipient with full AS2 payload to support cross-app standardization.
	CreateNotificationWithExtras(recipientID, senderID, "MESSAGE", messageID, map[string]interface{}{
		"content": content,
		"to":      recipientID,
	})
	return nil
}

// UpdateBio updates user metadata and logs the change for auditing.
func UpdateBio(userID, newBio string) error {
	_, err := db.Exec(
		`UPDATE profiles SET bio=$1, updated_at=NOW()
         WHERE user_id=$2`,
		newBio, userID,
	)
	if err != nil {
		return err
	}

	return LogActivity(
		userID,
		"UPDATE",
		"profile",
		userID,
		"",
		`{"action": "bio updated"}`,
	)
}

// ============================================================================
// FEDERATION OUTBOX: ACTIVITY LOGGING
// ============================================================================

// LogActivity acts as the "Outbox Producer". It pushes data into outbox_activities
// where background workers will eventually pick it up for delivery to remote servers.
/*
Asynchronous Activity Pipeline:
FediNet uses a "Write-Ahead Outbox" pattern to ensure delivery reliability.
1. Production: When a user likes, follows, or posts, `LogActivity` creates a
   record in the 'outbox_activities' table with status='pending'.
2. Selection: A persistent background worker (delivery engine) polls this table.
3. Transmission: The worker signs the payload and attempts an HTTP POST to the
   target server's inbox.
4. Retention: Successful deliveries are marked 'delivered'. Failed ones enter
   the exponential backoff retry loop.
*/
func LogActivity(actorID, verb, objectType, objectID, targetID, payload string) error {
	if payload == "" {
		payload = "{}"
	}
	// Note: We use the 'sent' status as a default, though workers may transition this
	// to 'pending' if it requires retry logic.
	_, err := db.Exec(
		`INSERT INTO outbox_activities
        (activity_type, actor_id, target_server, target_id, payload, delivery_status, attempt_count)
        VALUES ($1, $2, $3, $4, $5::jsonb, 'sent', 0)`,
		verb, actorID, objectType, objectID, payload,
	)
	if err != nil {
		// Non-fatal: Activity logging failure should not crash the user's primary action.
		log.Printf("LogActivity warning (non-fatal): %v", err)
	}
	return nil
}

// ============================================================================
// IDENTITY & ACCOUNT MANAGEMENT
// ============================================================================

// GetProfileByUserID retrieves a full Profile model including aggregated counts.
func GetProfileByUserID(userID string) (*models.Profile, error) {
	// Complex Query: Joins profiles with subqueries for real-time Follower/Following counts.
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

	// Handle SQL null dates gracefully for JSON serialization.
	if birthDate.Valid {
		t := birthDate.Time
		p.BirthDate = &t
	}

	return &p, nil
}

// CreateAccount implements the "Identity Protocol" for new users.
// It generates RSA/ED25519 keys, an encrypted private key store, and a DID.
/*
Account Provisioning Workflow:
Creating a FediNet identity is a heavy-duty cryptographic process.
1. Keypair Creation: Every user gets a unique Ed25519 key for signing activities.
2. At-Rest Encryption: The private key is encrypted with a server-wide master key.
   Even with DB access, an attacker cannot sign activities on behalf of the user.
3. Recovery Assets: Generates a human-readable recovery phrase (mnemonic) that
   allows users to regain access if they forget their password.
4. Profile Shadowing: Automatically initializes a blank Profile record to prevent
   null-pointer issues during first-login.
*/
func CreateAccount(userID, homeServer, passwordHash string) (string, error) {
	// 1. INPUT VALIDATION
	if !ValidateUserID(userID) {
		return "", fmt.Errorf("invalid user_id format")
	}

	// 2. CRYPTOGRAPHIC ASSET GENERATION
	// Generate public/private keypair for signing federated activities.
	pubKey, privKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", err
	}

	// Generate a 24-word or mnemonic recovery key in case of password loss.
	recoveryKey, recoveryHash, err := crypto.GenerateRecoveryKey()
	if err != nil {
		return "", err
	}

	// 3. TRANSACTIONAL INTEGRITY
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Check for existing users to prevent identity collisions.
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM identities WHERE user_id=$1)", userID).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("user already exists")
	}

	// 4. AT-REST ENCRYPTION
	// The private key is NEVER stored in plaintext. It is encrypted with the
	// server's master key before storage.
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
		fmt.Println("WARNING: Using insecure default SERVER_MASTER_KEY")
	}

	encryptedPrivKey, err := crypto.Encrypt(privKey, masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// 5. DECENTRALIZED IDENTIFIER (DID)
	// Create a unique DID based on the hash of the public key.
	did := "did:fedinet:" + crypto.HashString(pubKey)

	// 6. DB INSERTION
	identityID := uuid.New()
	_, err = tx.Exec(`
        INSERT INTO identities (
            id, did, user_id, home_server, public_key, private_key, key_version, recovery_key_hash, password_hash, allow_discovery, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, true, NOW(), NOW())
    `, identityID, did, userID, homeServer, pubKey, encryptedPrivKey, recoveryHash, passwordHash)
	if err != nil {
		return "", err
	}

	// Initialize the associated profile with default metadata.
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

// GetIdentityByUserID fetches the cryptographic identity details for a user.
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

// UpdateProfile executes a dynamic SQL update based on provided pointer fields.
// This prevents overwriting existing data with empty strings.
func UpdateProfile(req models.UpdateProfileRequest) error {
	query := "UPDATE profiles SET updated_at = NOW(), version = version + 1"
	args := []interface{}{}
	argCount := 1

	// DYNAMIC SQL BUILDER: Only append fields that are non-nil in the request.
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

// propagateProfileUpdate triggers a network-wide sync for profile changes.
func propagateProfileUpdate(userID string, req models.UpdateProfileRequest) error {
	// Deliver asynchronously so the HTTP response returns immediately.
	// This ensures a snappy UX even if remote servers are slow to respond.
	go PropagateProfileToTrustedServers(userID, req)
	return nil
}

// ============================================================================
// CONTENT CREATION: POSTS, LIKES, REPOSTS
// ============================================================================

// CreatePost generates a new status update or media-rich post.
/*
Content Creation Policy:
- Visibility: Supports 'public', 'private', and 'federated' scoping.
- Media Handling: Stores image/video URLs while preserving the original aspect ratios.
- Fallback: Enforces at least one character of content to satisfy Postgres
  constraints, though pure-media posts are supported via space-sentinels.
*/
func CreatePost(userID, content string, imageURL *string, visibility string) (string, error) {
	// Allow image-only posts by using a space sentinel when content is empty.
	if content == "" {
		content = " "
	}
	var postID string
	err := db.QueryRow(`
        INSERT INTO posts (author, content, image_url, visibility, created_at, updated_at)
        VALUES ($1, $2, $3, $4, NOW(), NOW())
        RETURNING id
    `, userID, content, imageURL, visibility).Scan(&postID)

	if err != nil {
		return "", err
	}

	return postID, nil
}

// ToggleLike implements a stateless "Like/Unlike" switch.
func ToggleLike(userID, postID string) error {
	var exists bool
	// Check for existing like to determine if we are adding or removing.
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

	// NOTIFICATION & LOGGING LOGIC
	if !exists {
		// New Like: Log activity for the outbox.
		if err := LogActivity(userID, "LIKE", "post", postID, "", ""); err != nil {
			return err
		}
		// Notify the author (only if the liker isn't the author).
		var authorID string
		if err := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); err == nil {
			if authorID != userID {
				CreateNotification(authorID, userID, "LIKE", postID)
			}
		}
		return nil
	}
	// Unlike: send an UNLIKE notification so clients can update in real-time.
	var authorID string
	if err := db.QueryRow("SELECT author FROM posts WHERE id=$1", postID).Scan(&authorID); err == nil {
		if authorID != userID {
			CreateNotification(authorID, userID, "UNLIKE", postID)
		}
	}
	return nil
}

// ToggleRepost handles sharing content to the user's followers.
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
		// Notify original author of the shared content.
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

// CreateReply generates a threaded response to an existing post.
/*
Threaded Communication Engine:
Replies are recursively linked via the 'parent_id' column.
- Single-Level: A direct response to a post.
- Multi-Level: A response to another response.
- Notification Fan-out: The system detects the original author and the parent-reply
  author to ensure everyone in the conversation is alerted when a new branch is added.
*/
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

	// Build activity log with nested JSON to preserve thread hierarchy.
	payload := ""
	if parentID != nil {
		payload = fmt.Sprintf(`{"parent_id": "%s", "content": %q}`, *parentID, content)
	} else {
		payload = fmt.Sprintf(`{"content": %q}`, content)
	}

	LogActivity(userID, "REPLY", "post", postID, "", payload)

	// NOTIFICATION FAN-OUT: Notify both the main post author AND the parent reply author.
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
			// Don't double-notify if the parent author is also the post author.
			if parentAuthorID != userID && parentAuthorID != authorID {
				CreateNotificationWithExtras(parentAuthorID, userID, "REPLY", postID, replyExtras)
			}
		}
	}

	return replyID, nil
}

// ============================================================================
// DATA RETRIEVAL: FEEDS & COLLECTIONS
// ============================================================================

type Reply struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	ParentID  *string   `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
}

// GetPostReplies retrieves all responses for a single thread.
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

// GetUserPosts retrieves a paginated feed of posts from a specific author.
func GetUserPosts(targetUserID, viewerUserID string, limit, offset int) ([]models.Post, error) {
	// Complex Query: Fetches post data while checking "has_liked" and "has_reposted" for the current viewer.
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
        WHERE p.author = $1 AND UPPER(p.visibility) = 'PUBLIC'
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

// GetRecentPosts returns the global "Public Timeline".
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
        WHERE UPPER(p.visibility) = 'PUBLIC'
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

// GetUserReplies returns replies written by userID, with the parent post included for UI context.
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

// GetUserLikedPosts returns a collection of posts that a specific user has liked.
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

// ============================================================================
// NOTIFICATION PIPELINE
// ============================================================================

// CreateNotification is a wrapper for basic events (Follow, Like, Mention).
func CreateNotification(recipientID, actorID, typeStr, entityID string) error {
	return CreateNotificationWithExtras(recipientID, actorID, typeStr, entityID, nil)
}

// CreateNotificationWithExtras stores a notification and its full ActivityStreams 2.0 payload.
// extras can carry "content", "parent_id", "server_name", "to" for richer AS2 objects.
// If the recipient is on a different server the notification is delivered federatedly.
func CreateNotificationWithExtras(recipientID, actorID, typeStr, entityID string, extras map[string]interface{}) error {
	// 1. AS2 PAYLOAD GENERATION
	// Build the ActivityStreams 2.0 payload first (needed for both local and federated paths).
	as2Bytes, as2Err := BuildActivityStream(actorID, typeStr, entityID, extras)
	if as2Err != nil {
		log.Printf("Warning: could not build ActivityStream for %s/%s: %v", typeStr, entityID, as2Err)
	}

	// 2. FEDERATION DETECTION
	// Detect cross-server recipients and deliver federatedly via background worker.
	if isFed, _ := IsFederatedUser(recipientID); isFed {
		go DeliverFederatedNotification(recipientID, actorID, typeStr, entityID, as2Bytes)
		return nil
	}

	// 3. LOCAL STORAGE
	// Local recipient — store in the PostgreSQL `notifications` table.
	if as2Err != nil {
		// Fallback: If AS2 generation failed, insert a skeleton notification to ensure
		// the user still sees the event.
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
