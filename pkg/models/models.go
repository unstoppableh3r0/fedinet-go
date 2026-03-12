package models

import (
	"time"

	"github.com/google/uuid"
)

// Visibility defines the scope of content accessibility within the network.
/*
Visibility Scoping Policy:
- public: Default. Content is accessible to everyone and federated globally.
- followers: Restricted to users who have an active Follow relationship.
- private: Local-only. Content does not leave the originating server.
- server: Restricted to users residing on the same physical node.
*/
type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityFollowers Visibility = "followers"
	VisibilityPrivate   Visibility = "private"
	VisibilityServer    Visibility = "server"
)

/*
Identity represents a user's globally unique cryptographic presence.
This model is the foundation of the 'Self-Sovereign Identity' (SSI) pattern.

Attributes:
- DID: Decentralized Identifier (e.g., did:fedinet:hash).
- PublicKey: Used by remote servers to verify the authenticity of user activities.
- PrivateKey: Encrypted at-rest. Used by the local node to sign outbound data.
- RecoveryKeyHash: A fallback mechanism for identity recovery in case of credential loss.
*/
type Identity struct {
	ID             uuid.UUID `json:"id"`
	DID            string    `json:"did,omitempty"`
	UserID         string    `json:"user_id"`
	HomeServer     string    `json:"home_server"`
	PublicKey      string    `json:"public_key"`
	AllowDiscovery bool      `json:"allow_discovery"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	Signature       string   `json:"signature,omitempty"`
	KeyVersion      int      `json:"key_version"`
	RecoveryKeyHash string   `json:"recovery_key_hash,omitempty"`
	Metadata        Metadata `json:"metadata,omitempty"`
	PrivateKey      string   `json:"-"`
}

type Metadata map[string]interface{}

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
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Signature string    `json:"signature"`
}

// UserBlock represents one user blocking another user.
type UserBlock struct {
	ID            int64      `json:"id"`
	BlockerUserID string     `json:"blocker_user_id"`
	BlockedUserID string     `json:"blocked_user_id"`
	Reason        string     `json:"reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	IsActive      bool       `json:"is_active"`
}

type BlockUserRequest struct {
	BlockerUserID string     `json:"blocker_user_id"`
	BlockedUserID string     `json:"blocked_user_id"`
	Reason        string     `json:"reason,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

/*
Profile contains mutable metadata about a user.
Unlike Identity, which is mostly static and cryptographic, the Profile is
designed for high-frequency updates and social display.

Features:
  - Count Caching: FollowersCount and FollowingCount are optionally returned to
    minimize database join costs during feed generation.
  - Visibility Toggles: Allows users to hide their social graph from public view.
*/
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

	Version int `json:"version"`
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
	OtherUser string    `json:"other_user"` // the conversation partner (computed by GetConversations)
	Content   string    `json:"content"`
	ImageURL  *string   `json:"image_url,omitempty"`
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

/*
Post represents a user-generated status, image, or article.
This is the primary atomic unit of content in the FediNet ecosystem.

Attributes:
- HasLiked/HasReposted: Injected booleans relative to the requesting 'viewer'.
- Engagement Metrics: Denormalized counts (LikeCount, ReplyCount) for performance.
*/
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

	IsEdited   bool    `json:"is_edited"`
	VersionNum int     `json:"version_num"`
	ImageURL   *string `json:"image_url,omitempty"`
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

type ModerationQueueItem struct {
	ContentID      string  `json:"content_id"`
	ToxicityScore  float64 `json:"toxicity_score"`
	Recommendation string  `json:"recommendation"`
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
	EventAbuseReportForward  = "abuse_report_forward"
	EventAbuseReportResolved = "abuse_report_resolved"
	EventServerBlockNotice   = "server_block_notice"
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

type ServerLoad struct {
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryPercent  float64   `json:"memory_percent"`
	ActiveConns    int       `json:"active_connections"`
	RequestsPerSec float64   `json:"requests_per_sec"`
	Timestamp      time.Time `json:"timestamp"`
}

type ProtocolVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
}

type FederationMessage struct {
	ID             uuid.UUID              `json:"id"`
	Version        string                 `json:"version"`
	MessageType    string                 `json:"message_type"`
	SenderServer   string                 `json:"sender_server"`
	ReceiverServer string                 `json:"receiver_server"`
	Timestamp      time.Time              `json:"timestamp"`
	Payload        map[string]interface{} `json:"payload"`
	Signature      *string                `json:"signature,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

type FederationRequest struct {
	Version string                 `json:"version"`
	Type    string                 `json:"type"`
	Sender  string                 `json:"sender"`
	Payload map[string]interface{} `json:"payload"`
}

type FederationResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Error   *ErrorResponse         `json:"error,omitempty"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

type DeliveryAttempt struct {
	ID             uuid.UUID  `json:"id"`
	MessageID      uuid.UUID  `json:"message_id"`
	AttemptNumber  int        `json:"attempt_number"`
	Status         string     `json:"status"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	BackoffSeconds int        `json:"backoff_seconds"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RetryConfig struct {
	MaxRetries     int           `json:"max_retries"`
	InitialBackoff time.Duration `json:"initial_backoff"`
	MaxBackoff     time.Duration `json:"max_backoff"`
	ExpirationTime time.Duration `json:"expiration_time"`
}

/*
InboxActivity represents an event arriving from a remote node.
It serves as the persistence layer for the incoming ActivityPub stream.

States (Status):
- received: Stored but not yet processed.
- processed: Business logic (like creating a follow row) complete.
- failed: Processing error documented in 'error_message'.
*/
type InboxActivity struct {
	ID           uuid.UUID  `json:"id"`
	ActivityType string     `json:"activity_type"`
	ActorID      string     `json:"actor_id"`
	ActorServer  string     `json:"actor_server"`
	TargetID     *string    `json:"target_id,omitempty"`
	Payload      string     `json:"payload"`
	ReceivedAt   time.Time  `json:"received_at"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
	ProcessedBy  *string    `json:"processed_by,omitempty"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type OutboxActivity struct {
	ID             uuid.UUID  `json:"id"`
	ActivityType   string     `json:"activity_type"`
	ActorID        string     `json:"actor_id"`
	TargetServer   string     `json:"target_server"`
	TargetID       *string    `json:"target_id,omitempty"`
	Payload        string     `json:"payload"`
	DeliveryStatus string     `json:"delivery_status"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

/*
InboxRequest is the wire-format model for incoming Activities.
It maps strictly to the ActivityPub/Fedinet JSON schema for external delivery.
*/
type InboxRequest struct {
	ActivityType string                 `json:"activity_type"`
	Actor        string                 `json:"actor"`
	ActorServer  string                 `json:"actor_server"`
	Target       *string                `json:"target,omitempty"`
	Payload      map[string]interface{} `json:"payload"`
	Signature    *string                `json:"signature,omitempty"`
}

type DeliveryAcknowledgment struct {
	ID             uuid.UUID `json:"id"`
	MessageID      uuid.UUID `json:"message_id"`
	SenderServer   string    `json:"sender_server"`
	ReceiverServer string    `json:"receiver_server"`
	Status         string    `json:"status"`
	Reason         *string   `json:"reason,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type AcknowledgmentRequest struct {
	MessageID uuid.UUID `json:"message_id"`
	Status    string    `json:"status"`
	Reason    *string   `json:"reason,omitempty"`
}

type RateLimit struct {
	ID              uuid.UUID  `json:"id"`
	ServerURL       string     `json:"server_url"`
	Endpoint        string     `json:"endpoint"`
	RequestsPerMin  int        `json:"requests_per_min"`
	BurstAllowance  int        `json:"burst_allowance"`
	CurrentCount    int        `json:"current_count"`
	WindowStartedAt time.Time  `json:"window_started_at"`
	LastRequestAt   *time.Time `json:"last_request_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type RateLimitConfig struct {
	ServerURL      string `json:"server_url"`
	Endpoint       string `json:"endpoint"`
	RequestsPerMin int    `json:"requests_per_min"`
	BurstAllowance int    `json:"burst_allowance"`
}

type SerializedActivity struct {
	Version   string                 `json:"@version"`
	Type      string                 `json:"@type"`
	ID        string                 `json:"id"`
	Actor     string                 `json:"actor"`
	Published time.Time              `json:"published"`
	Object    map[string]interface{} `json:"object,omitempty"`
	Target    *string                `json:"target,omitempty"`
	Context   map[string]interface{} `json:"@context,omitempty"`
}

type ServerCapabilities struct {
	ID               uuid.UUID `json:"id"`
	ServerURL        string    `json:"server_url"`
	ProtocolVersions string    `json:"protocol_versions"`
	SupportedTypes   string    `json:"supported_types"`
	MaxMessageSize   int       `json:"max_message_size"`
	SupportsRetries  bool      `json:"supports_retries"`
	SupportsAcks     bool      `json:"supports_acks"`
	RateLimitInfo    *string   `json:"rate_limit_info,omitempty"`
	CustomFeatures   *string   `json:"custom_features,omitempty"`
	LastDiscoveredAt time.Time `json:"last_discovered_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CapabilityRequest struct {
	ServerURL string `json:"server_url"`
}

type FederationBlockedServer struct {
	ID        uuid.UUID  `json:"id"`
	ServerURL string     `json:"server_url"`
	Reason    string     `json:"reason"`
	BlockedBy string     `json:"blocked_by"`
	BlockedAt time.Time  `json:"blocked_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type BlockServerRequest struct {
	ServerURL string     `json:"server_url"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type FederationConfig struct {
	ID                   uuid.UUID `json:"id"`
	Mode                 string    `json:"mode"`
	AllowUnknownServers  bool      `json:"allow_unknown_servers"`
	RequireCapabilityNeg bool      `json:"require_capability_neg"`
	StrictValidation     bool      `json:"strict_validation"`
	LogUnknownServers    bool      `json:"log_unknown_servers"`
	AutoBlockMalicious   bool      `json:"auto_block_malicious"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type FederationModeRequest struct {
	Mode                 string `json:"mode"`
	AllowUnknownServers  *bool  `json:"allow_unknown_servers,omitempty"`
	RequireCapabilityNeg *bool  `json:"require_capability_neg,omitempty"`
	StrictValidation     *bool  `json:"strict_validation,omitempty"`
}

type InstanceHealth struct {
	ID                   uuid.UUID `json:"id"`
	Status               string    `json:"status"`
	TotalMessages        int64     `json:"total_messages"`
	SuccessfulDeliveries int64     `json:"successful_deliveries"`
	FailedDeliveries     int64     `json:"failed_deliveries"`
	PendingRetries       int64     `json:"pending_retries"`
	AverageLatencyMs     int       `json:"average_latency_ms"`
	ActiveConnections    int       `json:"active_connections"`
	BlockedServersCount  int       `json:"blocked_servers_count"`
	RateLimitViolations  int64     `json:"rate_limit_violations"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	LastHealthCheckAt    time.Time `json:"last_health_check_at"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type HealthResponse struct {
	Status               string    `json:"status"`
	Timestamp            time.Time `json:"timestamp"`
	Uptime               int64     `json:"uptime_seconds"`
	TotalMessages        int64     `json:"total_messages"`
	SuccessfulDeliveries int64     `json:"successful_deliveries"`
	FailedDeliveries     int64     `json:"failed_deliveries"`
	PendingRetries       int64     `json:"pending_retries"`
	AverageLatencyMs     int       `json:"average_latency_ms"`
	ActiveConnections    int       `json:"active_connections"`
	BlockedServers       int       `json:"blocked_servers"`
	RateLimitViolations  int64     `json:"rate_limit_violations"`
}
