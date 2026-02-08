package main

import (
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ===============================
// Ranking & Preferences
// ===============================

// RankingMode represents supported timeline ranking modes
type RankingMode string

const (
	RankingModeChronological RankingMode = "chronological"
	RankingModePopular       RankingMode = "popular"
	RankingModeRelevance     RankingMode = "relevance"
	RankingModeTrending      RankingMode = "trending"
)

// UserRankingPreference stores user's preferred timeline sorting
type UserRankingPreference struct {
	UserID     string      `json:"user_id"`
	Preference RankingMode `json:"preference"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// ===============================
// Offline Cache
// ===============================

// ===============================
// Offline Cache
// ===============================

// ===============================
// Adaptive Refresh
// ===============================

// ===============================
// Activity & Load Levels
// ===============================

// ActivityLevel represents user activity intensity
type ActivityLevel string

const (
	ActivityHigh   ActivityLevel = "high"
	ActivityMedium ActivityLevel = "medium"
	ActivityLow    ActivityLevel = "low"
	ActivityIdle   ActivityLevel = "idle"
)

// LoadLevel represents server load intensity
type LoadLevel string

const (
	LoadNormal   LoadLevel = "normal"
	LoadHigh     LoadLevel = "high"
	LoadCritical LoadLevel = "critical"
)

// ===============================
// Timeline DTOs
// ===============================

// RankedPost extends models.Post with ranking score
type RankedPost struct {
	models.Post
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
	Posts       []RankedPost `json:"posts"`
	RankingMode RankingMode  `json:"ranking_mode"`
	Total       int          `json:"total"`
	HasMore     bool         `json:"has_more"`
}
