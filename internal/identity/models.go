package main

import (
	"time"

	"github.com/google/uuid"
)

type Identity struct {
	ID             uuid.UUID `json:"id"`
	UserID         string    `json:"user_id"`
	HomeServer     string    `json:"home_server"`
	PublicKey      string    `json:"public_key"`
	AllowDiscovery bool      `json:"allow_discovery"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Profile struct {
	UserID              string    `json:"user_id"`
	DisplayName         string    `json:"display_name"`
	AvatarURL           *string   `json:"avatar_url"`
	BannerURL           *string   `json:"banner_url"`
	Bio                 *string   `json:"bio"`
	PortfolioURL        *string   `json:"portfolio_url"`
	BirthDate           *string   `json:"birth_date"` // Keeping as string to match Scan usage or date type? actions.go scans into it. If postgres DATE, could be time.Time or string. Let's assume time.Time or string. In actions.go line 118 it scans into &p.BirthDate. If it's *string, db driver handles it? Let's check Scan.
	Location            *string   `json:"location"`
	FollowersVisibility string    `json:"followers_visibility"`
	FollowingVisibility string    `json:"following_visibility"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	FollowersCount      int       `json:"followers_count"`
	FollowingCount      int       `json:"following_count"`
}

type UserDocument struct {
	Identity Identity `json:"identity"`
	Profile  Profile  `json:"profile"`
}

type Post struct {
	ID          uuid.UUID `json:"id"`
	Author      string    `json:"author"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LikeCount   int       `json:"like_count"`
	ReplyCount  int       `json:"reply_count"`
	RepostCount int       `json:"repost_count"`
	HasLiked    bool      `json:"has_liked"`
	HasReposted bool      `json:"has_reposted"`
}

type UpdateProfileRequest struct {
	UserID              string  `json:"user_id"`
	DisplayName         *string `json:"display_name"`
	AvatarURL           *string `json:"avatar_url"`
	BannerURL           *string `json:"banner_url"`
	Bio                 *string `json:"bio"`
	PortfolioURL        *string `json:"portfolio_url"`
	BirthDate           *string `json:"birth_date"`
	Location            *string `json:"location"`
	FollowersVisibility *string `json:"followers_visibility"`
	FollowingVisibility *string `json:"following_visibility"`
}
