package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

// ============ Task 4.9: Customizable Ranking ============

// GetUserRankingPreference retrieves the user's preferred ranking mode
func GetUserRankingPreference(userID string) (RankingMode, error) {
	var preference string
	err := db.QueryRow(`
		SELECT preference FROM user_ranking_preferences 
		WHERE user_id = $1
	`, userID).Scan(&preference)

	if err == sql.ErrNoRows {
		// Return default if no preference set
		return RankingModeChronological, nil
	}
	if err != nil {
		return "", err
	}

	return RankingMode(preference), nil
}

// SetUserRankingPreference saves the user's ranking preference
func SetUserRankingPreference(userID string, mode RankingMode) error {
	_, err := db.Exec(`
		INSERT INTO user_ranking_preferences (user_id, preference, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (user_id) 
		DO UPDATE SET preference = $2, updated_at = NOW()
	`, userID, string(mode))

	return err
}

// RankPosts applies the specified ranking algorithm to posts
func RankPosts(posts []Post, mode RankingMode, userID string) []RankedPost {
	rankedPosts := make([]RankedPost, len(posts))

	for i, post := range posts {
		score := calculateRankScore(post, mode)
		rankedPosts[i] = RankedPost{
			Post:      post,
			RankScore: score,
		}
	}

	// Sort by rank score (higher is better)
	sortRankedPosts(rankedPosts)

	return rankedPosts
}

// calculateRankScore computes the ranking score based on the mode
func calculateRankScore(post Post, mode RankingMode) float64 {
	switch mode {
	case RankingModeChronological:
		// More recent posts get higher scores
		return float64(post.CreatedAt.Unix())

	case RankingModePopular:
		// Total engagement
		engagement := float64(post.LikeCount + post.ReplyCount + post.RepostCount)
		return engagement

	case RankingModeRelevance:
		// Weighted engagement with recency
		engagement := float64(post.LikeCount*2 + post.ReplyCount*3 + post.RepostCount*4)
		ageHours := time.Since(post.CreatedAt).Hours()
		decayFactor := 1.0 / (1.0 + ageHours/24.0) // Decay over days
		return engagement * decayFactor

	case RankingModeTrending:
		// Engagement velocity (engagement per hour of existence)
		ageHours := time.Since(post.CreatedAt).Hours()
		if ageHours < 0.1 {
			ageHours = 0.1 // Avoid division by zero
		}
		engagement := float64(post.LikeCount + post.ReplyCount + post.RepostCount)
		velocity := engagement / ageHours
		// Boost recent posts
		recencyBoost := math.Max(0, 48.0-ageHours) / 48.0
		return velocity * (1.0 + recencyBoost)

	default:
		return float64(post.CreatedAt.Unix())
	}
}

// sortRankedPosts sorts posts by rank score (descending)
func sortRankedPosts(posts []RankedPost) {
	// Simple bubble sort (for small lists; use quicksort for production)
	n := len(posts)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if posts[j].RankScore < posts[j+1].RankScore {
				posts[j], posts[j+1] = posts[j+1], posts[j]
			}
		}
	}
}

// ============ Task 4.13: Post Versioning ============

// CreatePostVersion saves a new version when a post is edited
func CreatePostVersion(postID, editorID, content string, version int, changeNote *string) error {
	_, err := db.Exec(`
		INSERT INTO post_versions (post_id, version, content, editor_id, edited_at, change_note)
		VALUES ($1, $2, $3, $4, NOW(), $5)
	`, postID, version, content, editorID, changeNote)

	return err
}

// GetPostVersionHistory retrieves all versions of a post
func GetPostVersionHistory(postID string) ([]PostVersion, error) {
	rows, err := db.Query(`
		SELECT id, post_id, version, content, editor_id, edited_at, change_note
		FROM post_versions
		WHERE post_id = $1
		ORDER BY version DESC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []PostVersion{}
	for rows.Next() {
		var v PostVersion
		err := rows.Scan(&v.ID, &v.PostID, &v.Version, &v.Content, &v.EditorID, &v.EditedAt, &v.ChangeNote)
		if err != nil {
			log.Printf("Error scanning version: %v", err)
			continue
		}
		versions = append(versions, v)
	}

	return versions, nil
}

// GetPostVersion retrieves a specific version of a post
func GetPostVersion(postID string, version int) (*PostVersion, error) {
	var v PostVersion
	err := db.QueryRow(`
		SELECT id, post_id, version, content, editor_id, edited_at, change_note
		FROM post_versions
		WHERE post_id = $1 AND version = $2
	`, postID, version).Scan(&v.ID, &v.PostID, &v.Version, &v.Content, &v.EditorID, &v.EditedAt, &v.ChangeNote)

	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetLatestVersionNumber returns the latest version number for a post
func GetLatestVersionNumber(postID string) (int, error) {
	var version int
	err := db.QueryRow(`
		SELECT COALESCE(MAX(version), 0) FROM post_versions WHERE post_id = $1
	`, postID).Scan(&version)

	return version, err
}

// ============ Task 4.14: Offline Mode ============

// CacheTimelineForUser stores timeline data for offline access
func CacheTimelineForUser(userID string, posts []Post, config OfflineConfig) error {
	// Limit posts according to config
	if len(posts) > config.MaxPostsPerUser {
		posts = posts[:config.MaxPostsPerUser]
	}

	// Serialize posts to JSON
	postData, err := json.Marshal(posts)
	if err != nil {
		return fmt.Errorf("failed to serialize posts: %w", err)
	}

	sizeBytes := int64(len(postData))

	// Check size limit
	if sizeBytes > config.MaxCacheSizeBytes {
		return fmt.Errorf("cache size %d exceeds limit %d", sizeBytes, config.MaxCacheSizeBytes)
	}

	expiresAt := time.Now().Add(config.CacheDuration)

	// Store in database
	_, err = db.Exec(`
		INSERT INTO cached_timelines (user_id, post_data, cached_at, expires_at, size_bytes)
		VALUES ($1, $2, NOW(), $3, $4)
		ON CONFLICT (user_id)
		DO UPDATE SET post_data = $2, cached_at = NOW(), expires_at = $3, size_bytes = $4
	`, userID, postData, expiresAt, sizeBytes)

	return err
}

// GetCachedTimeline retrieves cached timeline data
func GetCachedTimeline(userID string) ([]Post, error) {
	var postData []byte
	var expiresAt time.Time

	err := db.QueryRow(`
		SELECT post_data, expires_at FROM cached_timelines
		WHERE user_id = $1
	`, userID).Scan(&postData, &expiresAt)

	if err != nil {
		return nil, err
	}

	// Check if expired
	if time.Now().After(expiresAt) {
		return nil, fmt.Errorf("cache expired")
	}

	var posts []Post
	if err := json.Unmarshal(postData, &posts); err != nil {
		return nil, fmt.Errorf("failed to deserialize posts: %w", err)
	}

	return posts, nil
}

// RefreshCachedContent updates the cache when connectivity resumes
func RefreshCachedContent(userID string, posts []Post) error {
	config := DefaultOfflineConfig()
	// Could load user-specific config from DB if needed
	return CacheTimelineForUser(userID, posts, config)
}

// CleanExpiredCaches removes expired cache entries
func CleanExpiredCaches() error {
	result, err := db.Exec(`
		DELETE FROM cached_timelines WHERE expires_at < NOW()
	`)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	log.Printf("Cleaned %d expired cache entries", rows)
	return nil
}

// GetUserOfflineConfig retrieves user's offline configuration
func GetUserOfflineConfig(userID string) (OfflineConfig, error) {
	var config OfflineConfig
	var durationSeconds int

	err := db.QueryRow(`
		SELECT max_cache_size_bytes, max_posts_per_user, cache_duration_seconds, auto_refresh
		FROM offline_configs
		WHERE user_id = $1
	`, userID).Scan(&config.MaxCacheSizeBytes, &config.MaxPostsPerUser, &durationSeconds, &config.AutoRefresh)

	if err == sql.ErrNoRows {
		return DefaultOfflineConfig(), nil
	}
	if err != nil {
		return OfflineConfig{}, err
	}

	config.CacheDuration = time.Duration(durationSeconds) * time.Second
	return config, nil
}

// ============ Task 4.15: Adaptive Feed Refresh ============

// GetRefreshConfig retrieves the refresh configuration for a user
func GetRefreshConfig(userID string) (RefreshConfig, error) {
	var config RefreshConfig
	var baseSeconds, currentSeconds, minSeconds, maxSeconds int

	err := db.QueryRow(`
		SELECT base_interval_seconds, current_interval_seconds, min_interval_seconds,
		       max_interval_seconds, last_activity, last_refresh, adaptive_enabled, throttle_enabled
		FROM refresh_configs
		WHERE user_id = $1
	`, userID).Scan(&baseSeconds, &currentSeconds, &minSeconds, &maxSeconds,
		&config.LastActivity, &config.LastRefresh, &config.AdaptiveEnabled, &config.ThrottleEnabled)

	if err == sql.ErrNoRows {
		return DefaultRefreshConfig(userID), nil
	}
	if err != nil {
		return RefreshConfig{}, err
	}

	config.UserID = userID
	config.BaseInterval = time.Duration(baseSeconds) * time.Second
	config.CurrentInterval = time.Duration(currentSeconds) * time.Second
	config.MinInterval = time.Duration(minSeconds) * time.Second
	config.MaxInterval = time.Duration(maxSeconds) * time.Second

	return config, nil
}

// UpdateRefreshConfig saves the refresh configuration
func UpdateRefreshConfig(config RefreshConfig) error {
	_, err := db.Exec(`
		INSERT INTO refresh_configs (
			user_id, base_interval_seconds, current_interval_seconds, min_interval_seconds,
			max_interval_seconds, last_activity, last_refresh, adaptive_enabled, throttle_enabled
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id)
		DO UPDATE SET
			base_interval_seconds = $2,
			current_interval_seconds = $3,
			min_interval_seconds = $4,
			max_interval_seconds = $5,
			last_activity = $6,
			last_refresh = $7,
			adaptive_enabled = $8,
			throttle_enabled = $9
	`, config.UserID, int(config.BaseInterval.Seconds()), int(config.CurrentInterval.Seconds()),
		int(config.MinInterval.Seconds()), int(config.MaxInterval.Seconds()),
		config.LastActivity, config.LastRefresh, config.AdaptiveEnabled, config.ThrottleEnabled)

	return err
}

// RecordUserActivity updates the last activity timestamp
func RecordUserActivity(userID string) error {
	config, err := GetRefreshConfig(userID)
	if err != nil {
		return err
	}

	config.LastActivity = time.Now()
	return UpdateRefreshConfig(config)
}

// CalculateAdaptiveInterval adjusts refresh rate based on user activity and server load
func CalculateAdaptiveInterval(userID string) (time.Duration, error) {
	config, err := GetRefreshConfig(userID)
	if err != nil {
		return 0, err
	}

	if !config.AdaptiveEnabled {
		return config.BaseInterval, nil
	}

	// Determine user activity level
	activityLevel := GetActivityLevel(config.LastActivity)

	// Get server load
	loadLevel, err := GetCurrentLoadLevel()
	if err != nil {
		log.Printf("Error getting load level: %v", err)
		loadLevel = LoadNormal
	}

	// Calculate interval based on activity and load
	interval := config.BaseInterval

	// Adjust for user activity
	switch activityLevel {
	case ActivityHigh:
		interval = config.MinInterval // Refresh frequently for active users
	case ActivityMedium:
		interval = config.BaseInterval
	case ActivityLow:
		interval = config.BaseInterval * 2
	case ActivityIdle:
		interval = config.MaxInterval // Slow down for idle users
	}

	// Apply throttling for server load
	if config.ThrottleEnabled {
		switch loadLevel {
		case LoadHigh:
			interval = interval * 2 // Double the interval
		case LoadCritical:
			interval = config.MaxInterval // Maximum backoff
		}
	}

	// Enforce bounds
	if interval < config.MinInterval {
		interval = config.MinInterval
	}
	if interval > config.MaxInterval {
		interval = config.MaxInterval
	}

	// Update config
	config.CurrentInterval = interval
	config.LastRefresh = time.Now()
	if err := UpdateRefreshConfig(config); err != nil {
		log.Printf("Error updating refresh config: %v", err)
	}

	return interval, nil
}

// GetActivityLevel determines user activity level based on last activity
func GetActivityLevel(lastActivity time.Time) ActivityLevel {
	elapsed := time.Since(lastActivity)

	if elapsed < 5*time.Minute {
		return ActivityHigh
	} else if elapsed < 15*time.Minute {
		return ActivityMedium
	} else if elapsed < time.Hour {
		return ActivityLow
	}
	return ActivityIdle
}

// RecordServerLoad stores current server metrics
func RecordServerLoad(load ServerLoad) error {
	_, err := db.Exec(`
		INSERT INTO server_load_metrics (cpu_percent, memory_percent, active_connections, requests_per_sec, timestamp)
		VALUES ($1, $2, $3, $4, $5)
	`, load.CPUPercent, load.MemoryPercent, load.ActiveConns, load.RequestsPerSec, load.Timestamp)

	return err
}

// GetCurrentLoadLevel determines current server load level
func GetCurrentLoadLevel() (LoadLevel, error) {
	var avgCPU, avgMemory, avgRPS sql.NullFloat64
	var avgConns sql.NullInt64

	// Get average metrics from last 5 minutes
	err := db.QueryRow(`
		SELECT AVG(cpu_percent), AVG(memory_percent), AVG(active_connections), AVG(requests_per_sec)
		FROM server_load_metrics
		WHERE timestamp > NOW() - INTERVAL '5 minutes'
	`).Scan(&avgCPU, &avgMemory, &avgConns, &avgRPS)

	if err != nil {
		return LoadNormal, err
	}

	// No recent metrics
	if !avgCPU.Valid {
		return LoadNormal, nil
	}

	// Determine load level based on thresholds
	cpuPercent := avgCPU.Float64
	memPercent := avgMemory.Float64
	rps := avgRPS.Float64

	if cpuPercent > 80 || memPercent > 80 || rps > 1000 {
		return LoadCritical, nil
	} else if cpuPercent > 60 || memPercent > 60 || rps > 500 {
		return LoadHigh, nil
	}

	return LoadNormal, nil
}

// CleanOldMetrics removes old server load metrics
func CleanOldMetrics() error {
	// Keep only last 24 hours of metrics
	result, err := db.Exec(`
		DELETE FROM server_load_metrics 
		WHERE timestamp < NOW() - INTERVAL '24 hours'
	`)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	log.Printf("Cleaned %d old metric entries", rows)
	return nil
}
