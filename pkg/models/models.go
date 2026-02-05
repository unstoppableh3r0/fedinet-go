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

	// Social Counts
	LikeCount   int `json:"like_count"`
	ReplyCount  int `json:"reply_count"`
	RepostCount int `json:"repost_count"`

	// User State (for the viewer)
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
// EPIC - 3: PRIVACY, ENCRYPTION & USER SAFETY
// =================================================================

// PrivacyAuditLog tracks sensitive data access for compliance (Story 3.11)
type PrivacyAuditLog struct {
	ID            string    `json:"id" db:"id"`
	ActorID       string    `json:"actor_id" db:"actor_id"`   // Identity who accessed data
	TargetID      string    `json:"target_id" db:"target_id"` // Identity whose data was accessed
	Action        string    `json:"action" db:"action"`       // e.g., "VIEW_PRIVATE_POST"
	AccessGranted bool      `json:"access_granted" db:"access_granted"`
	Reason        string    `json:"reason" db:"reason"` // e.g., "FOLLOW_RELATIONSHIP_VALID"
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
}

// ProxyRequest holds metadata for the IP Masking Proxy (Story 3.14)
type ProxyRequest struct {
	RequestID   string    `json:"request_id"`
	OriginalURL string    `json:"original_url"`
	Method      string    `json:"method"`
	UserAgent   string    `json:"user_agent"` // Scrubbed version
	CreatedAt   time.Time `json:"created_at"`
}

// EncryptionEnvelope manages E2E message metadata (Story 3.1)
type EncryptionEnvelope struct {
	KeyID      string `json:"kid"`        // ID of the public key
	Algorithm  string `json:"alg"`        // e.g., "X25519-ChaCha20-Poly1305"
	Nonce      []byte `json:"nonce"`      // Random salt/IV
	Ciphertext []byte `json:"ciphertext"` // The actual encrypted message
}

// VisibilityLevel represents specialized privacy scopes
type VisibilityLevel string

const (
	VisibilityCircle  VisibilityLevel = "circle"  // Specific user-defined group
	VisibilityMutuals VisibilityLevel = "mutuals" // Only if both follow each other
)
