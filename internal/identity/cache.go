package main

import (
	"sync"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// followCacheTTL is how long a follower/following list stays valid before a
// fresh DB read is required.  30 s keeps UX snappy while staying current.
const followCacheTTL = 30 * time.Second

type followEntry struct {
	data      []models.UserDocument
	expiresAt time.Time
}

var (
	muFollowers    sync.RWMutex
	followersCache = make(map[string]*followEntry)

	muFollowing    sync.RWMutex
	followingCache = make(map[string]*followEntry)
)

// getFollowersCache returns the cached followers list for userID, if fresh.
func getFollowersCache(userID string) ([]models.UserDocument, bool) {
	muFollowers.RLock()
	e, ok := followersCache[userID]
	muFollowers.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		return e.data, true
	}
	return nil, false
}

// setFollowersCache stores the followers list under the given TTL.
func setFollowersCache(userID string, data []models.UserDocument) {
	muFollowers.Lock()
	followersCache[userID] = &followEntry{
		data:      data,
		expiresAt: time.Now().Add(followCacheTTL),
	}
	muFollowers.Unlock()
}

// getFollowingCache returns the cached following list for userID, if fresh.
func getFollowingCache(userID string) ([]models.UserDocument, bool) {
	muFollowing.RLock()
	e, ok := followingCache[userID]
	muFollowing.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		return e.data, true
	}
	return nil, false
}

// setFollowingCache stores the following list under the given TTL.
func setFollowingCache(userID string, data []models.UserDocument) {
	muFollowing.Lock()
	followingCache[userID] = &followEntry{
		data:      data,
		expiresAt: time.Now().Add(followCacheTTL),
	}
	muFollowing.Unlock()
}

// invalidateFollowCaches drops the relevant cache entries immediately after a
// follow or unfollow event.
//
//   - followeeID's "followers" list has changed
//   - followerID's "following" list has changed
func invalidateFollowCaches(followerID, followeeID string) {
	muFollowers.Lock()
	delete(followersCache, followeeID)
	muFollowers.Unlock()

	muFollowing.Lock()
	delete(followingCache, followerID)
	muFollowing.Unlock()
}
