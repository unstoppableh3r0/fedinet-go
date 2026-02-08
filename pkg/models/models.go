package models

import (
	"time"

	"github.com/google/uuid"
)

// Visibility represents content visibility settings
type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityFollowers Visibility = "followers"
	VisibilityPrivate   Visibility = "private"
	VisibilityServer    Visibility = "server"
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
	Signature       string   `json:"signature,omitempty"`         // Self-signed Identity
	KeyVersion      int      `json:"key_version"`                 // Current key version
	RecoveryKeyHash string   `json:"recovery_key_hash,omitempty"` // Hashed recovery key
	Metadata        Metadata `json:"metadata,omitempty"`          // Extensible metadata
	PrivateKey      string   `json:"-"`                           // Encrypted private key (never JSON exported)
}

type Metadata map[string]interface{}

// PortableProfile represents a full export of a user's data
type PortableProfile struct {
	User        UserDocument `json:"user_document"`
	Posts       []Post       `json:"posts"`
	Followers   []string     `json:"followers"`
	Following   []string     `json:"following"`
	ExportedAt  time.Time    `json:"exported_at"`
	IdentitySig string       `json:"identity_signature"` // Signed by the Identity Key
}

// KeyRevocation represents a revoked key
type KeyRevocation struct {
	KeyID      string    `json:"key_id"`
	IdentityID uuid.UUID `json:"identity_id"`
	Reason     string    `json:"reason"`
	RevokedAt  time.Time `json:"revoked_at"`
	Signature  string    `json:"signature"` // Signed by a valid key (or recovery key)
}

// BlockEvent represents a federation-wide block
type BlockEvent struct {
	BlockerID string    `json:"blocker_id"`
	BlockedID string    `json:"blocked_id"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Signature string    `json:"signature"`
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

	Version int `json:"version"` // For sync versioning
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

	LikeCount   int `json:"like_count"`
	ReplyCount  int `json:"reply_count"`
	RepostCount int `json:"repost_count"`

	HasLiked    bool `json:"has_liked"`
	HasReposted bool `json:"has_reposted"`
}

type Reply struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	ParentID  *string   `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
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

// =================================================================
// EPIC 3 — PRIVACY, ENCRYPTION & USER SAFETY
// =================================================================

type PrivacyAuditLog struct {
	ID            string    `json:"id" db:"id"`
	ActorID       string    `json:"actor_id" db:"actor_id"`
	TargetID      string    `json:"target_id" db:"target_id"`
	Action        string    `json:"action" db:"action"`
	AccessGranted bool      `json:"access_granted" db:"access_granted"`
	Reason        string    `json:"reason" db:"reason"`
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
}

type ProxyRequest struct {
	RequestID   string    `json:"request_id"`
	OriginalURL string    `json:"original_url"`
	Method      string    `json:"method"`
	UserAgent   string    `json:"user_agent"`
	CreatedAt   time.Time `json:"created_at"`
}

type EncryptionEnvelope struct {
	KeyID      string `json:"kid"`
	Algorithm  string `json:"alg"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type VisibilityLevel string

const (
	VisibilityCircle  VisibilityLevel = "circle"
	VisibilityMutuals VisibilityLevel = "mutuals"
)

// =================================================================
// EPIC 5 — GOVERNANCE & MODERATION
// =================================================================

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
