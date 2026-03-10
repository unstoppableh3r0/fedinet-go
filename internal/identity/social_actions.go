package identity

import (
	"log"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

func GetFeedPosts(userID string, limit, offset int) ([]models.Post, error) {
	query := `
		SELECT
			p.id,
			p.author,
			p.content,
			p.created_at,
			p.updated_at,
			(SELECT COUNT(*) FROM likes  WHERE post_id = p.id) AS like_count,
			(SELECT COUNT(*) FROM replies WHERE post_id = p.id) AS reply_count,
			(SELECT COUNT(*) FROM reposts WHERE post_id = p.id) AS repost_count,
			EXISTS(SELECT 1 FROM likes  WHERE post_id = p.id AND user_id = $1) AS has_liked,
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $1) AS has_reposted,
			p.image_url
		FROM posts p
		WHERE UPPER(p.visibility) = 'PUBLIC'
		  AND (
			p.author = $1
			OR p.author IN (
				SELECT followee_user_id FROM follows WHERE follower_user_id = $1
			)
		  )
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := db.Query(query, userID, limit, offset)
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
			log.Printf("Error scanning post: %v", err)
			continue
		}
		posts = append(posts, p)
	}

	return posts, rows.Err()
}

func GetFollowers(userID string) ([]models.UserDocument, error) {
	// Serve from cache when available.
	if cached, ok := getFollowersCache(userID); ok {
		return cached, nil
	}

	query := `
		SELECT f.follower_user_id
		FROM follows f
		WHERE f.followee_user_id = $1
		ORDER BY f.created_at DESC
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var followers []models.UserDocument
	for rows.Next() {
		var followerID string
		if err := rows.Scan(&followerID); err != nil {
			log.Printf("Error scanning follower: %v", err)
			continue
		}

		identity, err := GetIdentityByUserID(followerID)
		if err != nil {
			log.Printf("Error getting identity for follower %s: %v", followerID, err)
			continue
		}

		// Handle federated users not stored in this server's local DB
		if identity == nil {
			uname := followerID
			if idx := strings.Index(followerID, "@"); idx >= 0 {
				uname = followerID[:idx]
			}
			followers = append(followers, models.UserDocument{
				Identity: models.Identity{UserID: followerID, AllowDiscovery: true},
				Profile:  models.Profile{UserID: followerID, DisplayName: uname},
			})
			continue
		}

		profile, err := GetProfileByUserID(followerID)
		if err != nil {
			log.Printf("Error getting profile for follower %s: %v", followerID, err)
			continue
		}
		if profile == nil {
			continue
		}

		followers = append(followers, models.UserDocument{
			Identity: *identity,
			Profile:  *profile,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	setFollowersCache(userID, followers)
	return followers, nil
}

func GetFollowing(userID string) ([]models.UserDocument, error) {
	// Serve from cache when available.
	if cached, ok := getFollowingCache(userID); ok {
		return cached, nil
	}

	query := `
		SELECT f.followee_user_id
		FROM follows f
		WHERE f.follower_user_id = $1
		ORDER BY f.created_at DESC
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var following []models.UserDocument
	for rows.Next() {
		var followeeID string
		if err := rows.Scan(&followeeID); err != nil {
			log.Printf("Error scanning followee: %v", err)
			continue
		}

		identity, err := GetIdentityByUserID(followeeID)
		if err != nil {
			log.Printf("Error getting identity for followee %s: %v", followeeID, err)
			continue
		}

		// Handle federated users not stored in this server's local DB
		if identity == nil {
			uname := followeeID
			if idx := strings.Index(followeeID, "@"); idx >= 0 {
				uname = followeeID[:idx]
			}
			following = append(following, models.UserDocument{
				Identity: models.Identity{UserID: followeeID, AllowDiscovery: true},
				Profile:  models.Profile{UserID: followeeID, DisplayName: uname},
			})
			continue
		}

		profile, err := GetProfileByUserID(followeeID)
		if err != nil {
			log.Printf("Error getting profile for followee %s: %v", followeeID, err)
			continue
		}
		if profile == nil {
			continue
		}

		following = append(following, models.UserDocument{
			Identity: *identity,
			Profile:  *profile,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	setFollowingCache(userID, following)
	return following, nil
}

func GetConversations(userID string) ([]models.Message, error) {
	query := `
		SELECT DISTINCT ON (
			CASE
				WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
				ELSE recipient_id || '-' || sender_id
			END
		)
		id,
		sender_id      AS sender,
		recipient_id   AS receiver,
		content,
		created_at,
		image_url,
		CASE WHEN sender_id = $1 THEN recipient_id ELSE sender_id END AS other_user
		FROM messages
		WHERE sender_id = $1 OR recipient_id = $1
		ORDER BY
			CASE
				WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
				ELSE recipient_id || '-' || sender_id
			END,
			created_at DESC
		LIMIT 50
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []models.Message
	for rows.Next() {
		var m models.Message
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt, &m.ImageURL, &m.OtherUser)
		if err != nil {
			log.Printf("Error scanning conversation: %v", err)
			continue
		}
		// Compute OtherUser so the handler can ToExternalID it
		if m.Sender == userID {
			m.OtherUser = m.Receiver
		} else {
			m.OtherUser = m.Sender
		}
		conversations = append(conversations, m)
	}

	return conversations, rows.Err()
}

func GetConversationMessages(userID, otherUserID string) ([]models.Message, error) {
	// Also query using the external (pre-ToInternalID) form of otherUserID so
	// that federated messages stored with the sender's original external ID are found.
	otherUserExternal := ToExternalID(otherUserID)

	query := `
		SELECT id,
		       sender_id    AS sender,
		       recipient_id AS receiver,
		       content,
		       created_at,
		       image_url
		FROM messages
		WHERE (sender_id = $1 AND (recipient_id = $2 OR recipient_id = $3))
		   OR ((sender_id = $2 OR sender_id = $3) AND recipient_id = $1)
		ORDER BY created_at ASC
		LIMIT 100
	`

	rows, err := db.Query(query, userID, otherUserID, otherUserExternal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt, &m.ImageURL)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}
