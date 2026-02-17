package timeline

import (
	"time"

	"github.com/google/uuid"
)






type Post struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	LikeCount   int `json:"like_count"`
	ReplyCount  int `json:"reply_count"`
	RepostCount int `json:"repost_count"`

	HasLiked    bool `json:"has_liked"`
	HasReposted bool `json:"has_reposted"`

	
	IsEdited   bool `json:"is_edited"`
	VersionNum int  `json:"version_num"`
}


type PostVersion struct {
	ID         uuid.UUID `json:"id"`
	PostID     string    `json:"post_id"`
	Version    int       `json:"version"`
	Content    string    `json:"content"`
	EditorID   string    `json:"editor_id"`
	EditedAt   time.Time `json:"edited_at"`
	ChangeNote *string   `json:"change_note,omitempty"`
}


type PostWithVersions struct {
	Post     Post          `json:"post"`
	Versions []PostVersion `json:"versions"`
}

type Reply struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	ParentID  *string   `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}






type RankingMode string

const (
	RankingModeChronological RankingMode = "chronological"
	RankingModePopular       RankingMode = "popular"
	RankingModeRelevance     RankingMode = "relevance"
	RankingModeTrending      RankingMode = "trending"
)


type UserRankingPreference struct {
	UserID     string      `json:"user_id"`
	Preference RankingMode `json:"preference"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}






type CachedTimeline struct {
	ID        uuid.UUID `json:"id"`
	UserID    string    `json:"user_id"`
	PostData  string    `json:"post_data"` 
	CachedAt  time.Time `json:"cached_at"`
	ExpiresAt time.Time `json:"expires_at"`
	SizeBytes int64     `json:"size_bytes"`
}

type OfflineConfig struct {
	MaxCacheSizeBytes int64         `json:"max_cache_size_bytes"`
	MaxPostsPerUser   int           `json:"max_posts_per_user"`
	CacheDuration     time.Duration `json:"cache_duration"`
	AutoRefresh       bool          `json:"auto_refresh"`
}

func DefaultOfflineConfig() OfflineConfig {
	return OfflineConfig{
		MaxCacheSizeBytes: 50 * 1024 * 1024, 
		MaxPostsPerUser:   500,
		CacheDuration:     24 * time.Hour,
		AutoRefresh:       true,
	}
}





type RefreshConfig struct {
	UserID          string        `json:"user_id"`
	BaseInterval    time.Duration `json:"base_interval"`
	CurrentInterval time.Duration `json:"current_interval"`
	MinInterval     time.Duration `json:"min_interval"`
	MaxInterval     time.Duration `json:"max_interval"`
	LastActivity    time.Time     `json:"last_activity"`
	LastRefresh     time.Time     `json:"last_refresh"`
	AdaptiveEnabled bool          `json:"adaptive_enabled"`
	ThrottleEnabled bool          `json:"throttle_enabled"`
}

func DefaultRefreshConfig(userID string) RefreshConfig {
	return RefreshConfig{
		UserID:          userID,
		BaseInterval:    30 * time.Second,
		CurrentInterval: 30 * time.Second,
		MinInterval:     10 * time.Second,
		MaxInterval:     5 * time.Minute,
		AdaptiveEnabled: true,
		ThrottleEnabled: true,
	}
}






type ActivityLevel string

const (
	ActivityHigh   ActivityLevel = "high"
	ActivityMedium ActivityLevel = "medium"
	ActivityLow    ActivityLevel = "low"
	ActivityIdle   ActivityLevel = "idle"
)


type LoadLevel string

const (
	LoadNormal   LoadLevel = "normal"
	LoadHigh     LoadLevel = "high"
	LoadCritical LoadLevel = "critical"
)

type ServerLoad struct {
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryPercent  float64   `json:"memory_percent"`
	ActiveConns    int       `json:"active_connections"`
	RequestsPerSec float64   `json:"requests_per_sec"`
	Timestamp      time.Time `json:"timestamp"`
}






type RankedPost struct {
	Post
	RankScore float64 `json:"rank_score"`
}


type TimelineRequest struct {
	UserID      string      `json:"user_id"`
	RankingMode RankingMode `json:"ranking_mode,omitempty"`
	Limit       int         `json:"limit"`
	Offset      int         `json:"offset"`
}


type TimelineResponse struct {
	Posts       []RankedPost `json:"posts"`
	RankingMode RankingMode  `json:"ranking_mode"`
	Total       int          `json:"total"`
	HasMore     bool         `json:"has_more"`
}
