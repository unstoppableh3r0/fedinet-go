package identity

import (
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

func GetFeedPosts(userID string, limit, offset int) ([]models.Post, error) {
	// The feed selects posts from followed users and the viewer's own posts.
	// When multiple rows share the same group_id (origin post + replica on this
	// server), we keep only the one where origin_server matches THIS server
	// (i.e. the local replica) so followers see one canonical copy per post group.
	//
	// The outer query uses DISTINCT ON (COALESCE(group_id, id)) so that:
	//   • posts without a group_id are shown exactly once (their own id is the key)
	//   • posts that belong to a group show only the row where
	//     origin_server = this-server (preferred) or the earliest created_at otherwise.

	query := `
		SELECT
			p.id,
			p.author,
			p.content,
			p.created_at,
			p.updated_at,
			COALESCE(COUNT(DISTINCT l.user_id), 0)  AS like_count,
			COALESCE(COUNT(DISTINCT r.id), 0)        AS reply_count,
			COALESCE(COUNT(DISTINCT rp.user_id), 0)  AS repost_count,
			EXISTS(SELECT 1 FROM likes   WHERE post_id = p.id AND user_id = $1) AS has_liked,
			EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $1) AS has_reposted,
			p.image_url,
			p.expires_at,
			p.group_id,
			p.origin_post,
			p.origin_server,
			p.visibility
		FROM (
			SELECT DISTINCT ON (COALESCE(p2.group_id, p2.id::text))
				p2.*
			FROM posts p2
			WHERE (
				p2.author IN (SELECT followee_user_id FROM follows WHERE follower_user_id = $1)
				OR p2.author = $1
			  )
			  AND p2.visibility NOT IN ('HIDDEN', 'REJECTED')
			  AND (p2.expires_at IS NULL OR p2.expires_at > NOW())
			  AND (
			    p2.author = $1
			    OR p2.visibility = 'PUBLIC'
			    OR (p2.visibility = 'FOLLOWERS' AND p2.author IN (
			        SELECT followee_user_id FROM follows WHERE follower_user_id = $1
			    ))
			    OR (p2.visibility = 'CLOSE_FRIENDS' AND EXISTS(
			        SELECT 1 FROM close_friends WHERE user_id = p2.author AND friend_id = $1
			    ))
			  )
			  AND p2.author NOT IN (SELECT blocked_id FROM block_events WHERE blocker_id = $1)
			  AND p2.author NOT IN (SELECT blocker_id FROM block_events WHERE blocked_id = $1)
			ORDER BY
				COALESCE(p2.group_id, p2.id::text),
				-- prefer local origin replica; fall back to earliest created
				CASE WHEN p2.origin_server = $4 OR p2.origin_server IS NULL THEN 0 ELSE 1 END,
				p2.created_at ASC
		) p
		LEFT JOIN likes   l  ON p.id = l.post_id
		LEFT JOIN replies r  ON p.id = r.post_id
		LEFT JOIN reposts rp ON p.id = rp.post_id
		GROUP BY p.id, p.author, p.content, p.created_at, p.updated_at,
		         p.image_url, p.expires_at, p.group_id, p.origin_post, p.origin_server, p.visibility
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`

	thisServer := GetServerURL()
	rows, err := db.Query(query, userID, limit, offset, thisServer)
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
			&p.HasLiked, &p.HasReposted, &p.ImageURL, &p.ExpiresAt,
			&p.GroupID, &p.OriginPost, &p.OriginServer, &p.Visibility,
		)
		if err != nil {
			log.Printf("Error scanning post: %v", err)
			continue
		}

		// If this post has a group_id, find the other servers that have replicas.
		if p.GroupID != nil {
			servers, err := getReplicaServers(*p.GroupID, p.ID)
			if err == nil && len(servers) > 0 {
				p.ReplicaServers = servers
			}
		}

		posts = append(posts, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ── Cross-server feed augmentation (first page only) ─────────────────────
	// For offset > 0 we only serve local posts; cross-server pagination would
	// require cursor-based paging which is handled as a future enhancement.
	if offset == 0 {
		remoteServers, remErr := remoteFolloweeServerURLs(userID)
		if remErr != nil {
			log.Printf("GetFeedPosts: failed to list remote servers: %v", remErr)
		}
		if len(remoteServers) > 0 {
			posts = mergeWithRemoteSlices(userID, posts, remoteServers, limit)
		}
	}

	return posts, nil
}

// mergeWithRemoteSlices fetches feed slices from each remote server concurrently,
// deduplicates against the local posts (by group_id when available, else post ID),
// sorts the combined list by created_at DESC, and trims to limit.
//
// Cache strategy (stale-while-revalidate):
//   - If a fresh cache entry exists for (userID, server), it is used directly
//     with no network call.
//   - If a stale (but not too-old) entry exists, the stale data is served
//     immediately while a background goroutine refreshes it.
//   - If no usable entry exists, a synchronous fetch is performed and the result
//     is stored in the cache.
func mergeWithRemoteSlices(userID string, local []models.Post, remoteServers []string, limit int) []models.Post {
	// Build seen-set from local posts.
	seen := make(map[string]bool, len(local))
	for _, p := range local {
		if p.GroupID != nil && *p.GroupID != "" {
			seen[*p.GroupID] = true
		}
		seen[p.ID] = true
	}

	slices := make([][]models.Post, len(remoteServers))
	var wg sync.WaitGroup

	for i, ep := range remoteServers {
		cached, fresh, found := getCachedRemoteSlice(userID, ep)
		if found && fresh {
			// Cache hit — use directly, no network call.
			slices[i] = cached
			continue
		}
		if found && !fresh {
			// Stale hit — serve stale data immediately, refresh in background.
			slices[i] = cached
			wg.Add(1)
			go func(endpoint string) {
				defer wg.Done()
				if refreshed := fetchRemoteFeedSlice(userID, endpoint, limit); refreshed != nil {
					setCachedRemoteSlice(userID, endpoint, refreshed)
				}
			}(ep)
			continue
		}
		// Cache miss — fetch synchronously so the first page is complete.
		wg.Add(1)
		go func(idx int, endpoint string) {
			defer wg.Done()
			fetched := fetchRemoteFeedSlice(userID, endpoint, limit)
			slices[idx] = fetched
			setCachedRemoteSlice(userID, endpoint, fetched)
		}(i, ep)
	}
	wg.Wait()

	// Combine local + unseen remote posts.
	all := make([]models.Post, 0, len(local)+len(remoteServers)*limit)
	all = append(all, local...)
	for _, slice := range slices {
		for _, p := range slice {
			key := p.ID
			if p.GroupID != nil && *p.GroupID != "" {
				key = *p.GroupID
			}
			if !seen[key] {
				seen[key] = true
				all = append(all, p)
			}
		}
	}

	// Sort by created_at DESC.
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// getReplicaServers returns the origin_server values of all posts in a group
// other than the given postID (the one we already selected).
func getReplicaServers(groupID, excludePostID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT origin_server
		FROM posts
		WHERE group_id = $1
		  AND id != $2
		  AND origin_server IS NOT NULL
	`, groupID, excludePostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			servers = append(servers, s)
		}
	}
	return servers, rows.Err()
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
		WITH latest_messages AS (
			SELECT DISTINCT ON (
				CASE 
					WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
					ELSE recipient_id || '-' || sender_id
				END
			)
			id, sender_id, recipient_id, content, created_at, image_url, is_encrypted,
			CASE WHEN sender_id = $1 THEN recipient_id ELSE sender_id END AS other_user
			FROM messages
			WHERE sender_id = $1 OR recipient_id = $1
			ORDER BY 
				CASE 
					WHEN sender_id < recipient_id THEN sender_id || '-' || recipient_id
					ELSE recipient_id || '-' || sender_id
				END,
				created_at DESC
		)
		SELECT id, sender_id, recipient_id, content, created_at, image_url, is_encrypted, other_user
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
		var isEncrypted bool
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt, &m.ImageURL, &isEncrypted, &m.OtherUser)
		if err != nil {
			log.Printf("Error scanning conversation: %v", err)
			continue
		}
		if isEncrypted {
			masterKey := os.Getenv("SERVER_MASTER_KEY")
			if masterKey == "" {
				masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			if dec, err := crypto.Decrypt(m.Content, masterKey); err == nil {
				m.Content = dec
			}
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
	query := `
		SELECT id, sender_id, recipient_id, content, created_at, image_url, is_encrypted
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
		var isEncrypted bool
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Content, &m.CreatedAt, &m.ImageURL, &isEncrypted)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		if isEncrypted {
			masterKey := os.Getenv("SERVER_MASTER_KEY")
			if masterKey == "" {
				masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			if dec, err := crypto.Decrypt(m.Content, masterKey); err == nil {
				m.Content = dec
			}
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}
