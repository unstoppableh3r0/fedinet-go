package main

import (
	"time"

	"github.com/google/uuid"
)

type Identity struct {
	ID             uuid.UUID `json:"id"`
	DID            string    `json:"did,omitempty"` // Decentralized Identifier
	UserID         string    `json:"user_id"`
	HomeServer     string    `json:"home_server"`
	PublicKey      string    `json:"public_key"`
	AllowDiscovery bool      `json:"allow_discovery"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Crypto & Federation
	Signature       string   `json:"signature,omitempty"`
	KeyVersion      int      `json:"key_version"`
	RecoveryKeyHash string   `json:"recovery_key_hash,omitempty"`
	Metadata        Metadata `json:"metadata,omitempty"`
	PrivateKey      string   `json:"-"`
}

type Metadata map[string]interface{}

type Profile struct {
	UserID              string    `json:"user_id"`
	DisplayName         string    `json:"display_name"`
	AvatarURL           *string   `json:"avatar_url"`
	BannerURL           *string   `json:"banner_url"`
	Bio                 *string   `json:"bio"`
	PortfolioURL        *string   `json:"portfolio_url"`
	BirthDate           *string   `json:"birth_date"`
	Location            *string   `json:"location"`
	FollowersVisibility string    `json:"followers_visibility"`
	FollowingVisibility string    `json:"following_visibility"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	FollowersCount      int       `json:"followers_count"`
	FollowingCount      int       `json:"following_count"`
	Version             int       `json:"version"`
}

type UserDocument struct {
	Identity Identity `json:"identity"`
	Profile  Profile  `json:"profile"`
}

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

// PortableProfile represents a full export of a user's data
type PortableProfile struct {
	User        UserDocument `json:"user_document"`
	Posts       []Post       `json:"posts"`
	Followers   []string     `json:"followers"`
	Following   []string     `json:"following"`
	ExportedAt  time.Time    `json:"exported_at"`
	IdentitySig string       `json:"identity_signature"`
	PrivateKey  string       `json:"private_key"`
}

type KeyRevocation struct {
	KeyID      string    `json:"key_id"`
	IdentityID uuid.UUID `json:"identity_id"`
	Reason     string    `json:"reason"`
	RevokedAt  time.Time `json:"revoked_at"`
	Signature  string    `json:"signature"`
}

type BlockEvent struct {
	BlockerID string    `json:"blocker_id"`
	BlockedID string    `json:"blocked_id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	Signature string    `json:"signature"`
}
