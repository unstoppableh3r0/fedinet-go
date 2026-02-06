package models

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
	UserID              string     `json:"user_id"`
	DisplayName         string     `json:"display_name"`
	AvatarURL           *string    `json:"avatar_url,omitempty"`
	BannerURL           *string    `json:"banner_url,omitempty"`
	Bio                 *string    `json:"bio,omitempty"`
	PortfolioURL        *string    `json:"portfolio_url,omitempty"`
	BirthDate           *time.Time `json:"birth_date,omitempty"`
	Location            *string    `json:"location,omitempty"`
	FollowersVisibility string     `json:"followers_visibility"`
	FollowingVisibility string     `json:"following_visibility"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	FollowersCount      *int       `json:"followers_count,omitempty"`
	FollowingCount      *int       `json:"following_count,omitempty"`
}

type UserDocument struct {
	Identity Identity `json:"identity"`
	Profile  Profile  `json:"profile"`
}

type Follow struct {
	FollowerUserID     string    `json:"follower_user_id"`
	FollowerHomeServer string    `json:"follower_home_server"`
	FolloweeUserID     string    `json:"followee_user_id"`
	FolloweeHomeServer string    `json:"followee_home_server"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Activity struct {
	ID         string    `json:"id"`
	ActorID    string    `json:"actor_id"`
	Verb       string    `json:"verb"`
	ObjectType *string   `json:"object_type"`
	ObjectID   *string   `json:"object_id"`
	TargetID   *string   `json:"target_id"`
	Payload    any       `json:"payload"`
	CreatedAt  time.Time `json:"created_at"`
}

type Post struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID        string    `json:"id"`
	Sender    string    `json:"sender"`
	Receiver  string    `json:"receiver"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type UpdateProfileRequest struct {
	UserID              string  `json:"user_id"`
	DisplayName         *string `json:"display_name,omitempty"`
	AvatarURL           *string `json:"avatar_url,omitempty"`
	BannerURL           *string `json:"banner_url,omitempty"`
	Bio                 *string `json:"bio,omitempty"`
	PortfolioURL        *string `json:"portfolio_url,omitempty"`
	BirthDate           *string `json:"birth_date,omitempty"`
	Location            *string `json:"location,omitempty"`
	FollowersVisibility *string `json:"followers_visibility,omitempty"`
	FollowingVisibility *string `json:"following_visibility,omitempty"`
}

/*
===============================
EPIC 5 – Moderation
===============================
*/

type ReportStatus string

const (
	ReportPending  ReportStatus = "pending"
	ReportResolved ReportStatus = "resolved"
)

type Report struct {
	ID           int64        `json:"id"`
	ReporterID   string       `json:"reporter_id"`
	TargetRef    string       `json:"target_ref"`
	TargetServer string       `json:"target_server"`
	Reason       string       `json:"reason"`
	Status       ReportStatus `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	ResolvedAt   *time.Time   `json:"resolved_at,omitempty"`
	ResolvedBy   *string      `json:"resolved_by,omitempty"`
}

type BlockedServer struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Reason    string    `json:"reason"`
	BlockedAt time.Time `json:"blocked_at"`
	BlockedBy string    `json:"blocked_by"`
}

type FederationEventType string

const (
	EventAbuseReportForward FederationEventType = "abuse_report_forward"
	EventServerBlockNotice  FederationEventType = "server_block_notice"
)

type FederationEvent struct {
	ID           int64               `json:"id"`
	EventType    FederationEventType `json:"event_type"`
	TargetServer string              `json:"target_server"`
	Payload      []byte              `json:"payload"`
	RetryCount   int                 `json:"retry_count"`
	CreatedAt    time.Time           `json:"created_at"`
	LastTriedAt  *time.Time          `json:"last_tried_at,omitempty"`
}

type BackupMetadata struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Location  string    `json:"location"`
	CreatedBy string    `json:"created_by"`
}
