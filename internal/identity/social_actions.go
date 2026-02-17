package identity

import (
	"log"

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
			COALESCE(COUNT(DISTINCT l.user_id), 0) as like_count,
			COALESCE(COUNT(DISTINCT r.id), 0) as reply_count,
			COALESCE(COUNT(DISTINCT rp.user_id), 0) as repost_count,
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) as has_liked,
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $1) as has_reposted
		FROM posts p
		LEFT JOIN likes l ON p.id = l.post_id
		LEFT JOIN replies r ON p.id = r.post_id
		LEFT JOIN reposts rp ON p.id = rp.post_id
		WHERE p.author IN (
			SELECT followee_user_id FROM follows WHERE follower_user_id = $1
		) OR p.author = $1
		GROUP BY p.id, p.author, p.content, p.created_at, p.updated_at
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
			&p.HasLiked, &p.HasReposted,
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

		profile, err := GetProfileByUserID(followerID)
		if err != nil {
			log.Printf("Error getting profile for follower %s: %v", followerID, err)
			continue
		}

		followers = append(followers, models.UserDocument{
			Identity: *identity,
			Profile:  *profile,
		})
	}

	return followers, rows.Err()
}


func GetFollowing(userID string) ([]models.UserDocument, error) {
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

		profile, err := GetProfileByUserID(followeeID)
		if err != nil {
			log.Printf("Error getting profile for followee %s: %v", followeeID, err)
			continue
		}

		following = append(following, models.UserDocument{
			Identity: *identity,
			Profile:  *profile,
		})
	}

	return following, rows.Err()
}


func GetConversations(userID string) ([]models.Message, error) {
	query := `
		WITH latest_messages AS (
			SELECT DISTINCT ON (
				CASE 
					WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
					ELSE recipient_id || '-' || sender_id
				END
			)
			id, sender_id, recipient_id, content, created_at
			FROM messages
			WHERE sender_id = $1 OR recipient_id = $1
			ORDER BY 
				CASE 
					WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
					ELSE recipient_id || '-' || sender_id
				END,
				created_at DESC
		)
		SELECT id, sender_id, recipient_id, content, created_at
		FROM latest_messages
		ORDER BY created_at DESC
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
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt)
		if err != nil {
			log.Printf("Error scanning conversation: %v", err)
			continue
		}
		conversations = append(conversations, m)
	}

	return conversations, rows.Err()
}


func GetConversationMessages(userID, otherUserID string) ([]models.Message, error) {
	query := `
		SELECT id, sender_id, recipient_id, content, created_at
		FROM messages
		WHERE (sender_id = $1 AND recipient_id = $2)
		   OR (sender_id = $2 AND recipient_id = $1)
		ORDER BY created_at ASC
		LIMIT 100
	`

	rows, err := db.Query(query, userID, otherUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}
