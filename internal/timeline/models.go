package main

import (
	"time"

	"github.com/google/uuid"
)

// RankingMode represents the supported timeline ranking modes
type RankingMode string

const (
	RankingModeChronological RankingMode = "chronological" // Newest first
	RankingModePopular       RankingMode = "popular"       // By engagement (likes + reposts + replies)
	RankingModeRelevance     RankingMode = "relevance"     // By user interaction patterns
	RankingModeTrending      RankingMode = "trending"      // Recent engagement velocity
)

// UserRankingPreference stores user's preferred timeline sorting
type UserRankingPreference struct {
	UserID      string      `json:"user_id"`
	Preference  RankingMode `json:"preference"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// PostVersion represents a version of an edited post
type PostVersion struct {
	ID          uuid.UUID `json:"id"`
	PostID      string    `json:"post_id"`
	Version     int       `json:"version"`
	Content     string    `json:"content"`
	EditorID    string    `json:"editor_id"`    // Who made this edit
	EditedAt    time.Time `json:"edited_at"`
	ChangeNote  *string   `json:"change_note,omitempty"` // Optional note about what changed
}

// PostWithVersions includes the current post and its version history
type PostWithVersions struct {
	Post     Post          `json:"post"`
	Versions []PostVersion `json:"versions"`
}

// Post represents a post in the timeline
type Post struct {
	ID          string    `json:"id"`
	Author      string    `json:"author"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LikeCount   int       `json:"like_count"`
	ReplyCount  int       `json:"reply_count"`
	RepostCount int       `json:"repost_count"`
	HasLiked    bool      `json:"has_liked"`
	HasReposted bool      `json:"has_reposted"`
	IsEdited    bool      `json:"is_edited"`
	VersionNum  int       `json:"version_num"`
}

// CachedTimeline represents cached timeline data for offline access
type CachedTimeline struct {
	ID           uuid.UUID `json:"id"`
	UserID       string    `json:"user_id"`
	PostData     string    `json:"post_data"` // JSON serialized posts
	CachedAt     time.Time `json:"cached_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	SizeBytes    int64     `json:"size_bytes"`
}

// OfflineConfig defines storage limits for offline data
type OfflineConfig struct {
	MaxCacheSizeBytes int64         `json:"max_cache_size_bytes"`
	MaxPostsPerUser   int           `json:"max_posts_per_user"`
	CacheDuration     time.Duration `json:"cache_duration"`
	AutoRefresh       bool          `json:"auto_refresh"`
}

// DefaultOfflineConfig returns sensible defaults
func DefaultOfflineConfig() OfflineConfig {
	return OfflineConfig{
		MaxCacheSizeBytes: 50 * 1024 * 1024, // 50MB per user
		MaxPostsPerUser:   500,
		CacheDuration:     24 * time.Hour,
		AutoRefresh:       true,
	}
}

// ServerLoad represents current server load metrics
type ServerLoad struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	ActiveConns   int       `json:"active_connections"`
	RequestsPerSec float64  `json:"requests_per_sec"`
	Timestamp     time.Time `json:"timestamp"`
}

// RefreshConfig manages adaptive feed refresh settings
type RefreshConfig struct {
	UserID              string        `json:"user_id"`
	BaseInterval        time.Duration `json:"base_interval"`        // Base refresh interval
	CurrentInterval     time.Duration `json:"current_interval"`     // Current adapted interval
	MinInterval         time.Duration `json:"min_interval"`         // Minimum allowed interval
	MaxInterval         time.Duration `json:"max_interval"`         // Maximum allowed interval
	LastActivity        time.Time     `json:"last_activity"`        // Last user activity
	LastRefresh         time.Time     `json:"last_refresh"`
	AdaptiveEnabled     bool          `json:"adaptive_enabled"`
	ThrottleEnabled     bool          `json:"throttle_enabled"`
}

// DefaultRefreshConfig returns default refresh settings
func DefaultRefreshConfig(userID string) RefreshConfig {
	return RefreshConfig{
		UserID:          userID,
		BaseInterval:    30 * time.Second,
		CurrentInterval: 30 * time.Second,
		MinInterval:     10 * time.Second,
		MaxInterval:     5 * time.Minute,
		LastActivity:    time.Now(),
		LastRefresh:     time.Now(),
		AdaptiveEnabled: true,
		ThrottleEnabled: true,
	}
}

// ActivityLevel represents user activity intensity
type ActivityLevel string

const (
	ActivityHigh   ActivityLevel = "high"   // Active within last 5 minutes
	ActivityMedium ActivityLevel = "medium" // Active within last 15 minutes
	ActivityLow    ActivityLevel = "low"    // Active within last hour
	ActivityIdle   ActivityLevel = "idle"   // Inactive for over an hour
)

// LoadLevel represents server load intensity
type LoadLevel string

const (
	LoadNormal LoadLevel = "normal"
	LoadHigh   LoadLevel = "high"
	LoadCritical LoadLevel = "critical"
)

// RankedPost extends Post with ranking score
type RankedPost struct {
	Post
	RankScore float64 `json:"rank_score"`
}

// TimelineRequest represents a request for timeline data
type TimelineRequest struct {
	UserID      string      `json:"user_id"`
	RankingMode RankingMode `json:"ranking_mode,omitempty"`
	Limit       int         `json:"limit"`
	Offset      int         `json:"offset"`
}

// TimelineResponse represents the timeline response
type TimelineResponse struct {
	Posts       []RankedPost  `json:"posts"`
	RankingMode RankingMode   `json:"ranking_mode"`
	Total       int           `json:"total"`
	HasMore     bool          `json:"has_more"`
}
