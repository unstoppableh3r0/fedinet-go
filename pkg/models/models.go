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
	PrivateKey  string       `json:"private_key"`        // User's encrypted private key for portability
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

// ===============================
// EPIC 4 — CONTENT & TIMELINE
// ===============================

// Post represents a federated social post
// Used by Epic 1, Epic 4, and Timeline service
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

	// Epic 4 additions (non-breaking)
	IsEdited   bool `json:"is_edited"`
	VersionNum int  `json:"version_num"`
}

// PostVersion represents a historical version of an edited post
type PostVersion struct {
	ID         uuid.UUID `json:"id"`
	PostID     string    `json:"post_id"`
	Version    int       `json:"version"`
	Content    string    `json:"content"`
	EditorID   string    `json:"editor_id"`
	EditedAt   time.Time `json:"edited_at"`
	ChangeNote *string   `json:"change_note,omitempty"`
}

// PostWithVersions bundles a post with its full edit history
type PostWithVersions struct {
	Post     Post          `json:"post"`
	Versions []PostVersion `json:"versions"`
}

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

// =================================================================
// TIMELINE & OPTIMIZATION MODELS
// =================================================================

// CachedTimeline represents cached timeline data for offline access
type CachedTimeline struct {
	ID        uuid.UUID `json:"id"`
	UserID    string    `json:"user_id"`
	PostData  string    `json:"post_data"` // JSON serialized []models.Post
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
		MaxCacheSizeBytes: 50 * 1024 * 1024, // 50MB
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

// ============================================================================
// FEDERATION MODELS (from internal/federation)
// ============================================================================

// ProtocolVersion represents the federation protocol version for backward compatibility
type ProtocolVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
}

// FederationMessage is the core message structure for all federation communications
type FederationMessage struct {
	ID             uuid.UUID              `json:"id"`
	Version        string                 `json:"version"`         // "1.0.0"
	MessageType    string                 `json:"message_type"`    // "activity", "ack", "capability", "health"
	SenderServer   string                 `json:"sender_server"`   // "https://server1.com"
	ReceiverServer string                 `json:"receiver_server"` // "https://server2.com"
	Timestamp      time.Time              `json:"timestamp"`
	Payload        map[string]interface{} `json:"payload"`
	Signature      *string                `json:"signature,omitempty"` // Crypto signature for verification
	CreatedAt      time.Time              `json:"created_at"`
}

// FederationRequest is the standard request envelope
type FederationRequest struct {
	Version string                 `json:"version"`
	Type    string                 `json:"type"`
	Sender  string                 `json:"sender"`
	Payload map[string]interface{} `json:"payload"`
}

// FederationResponse is the standard response envelope
type FederationResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Error   *ErrorResponse         `json:"error,omitempty"`
}

// ErrorResponse provides standardized error format
type ErrorResponse struct {
	Code    int    `json:"code"`              // HTTP status code
	Type    string `json:"type"`              // Error type: "validation", "auth", "rate_limit", "protocol", "internal"
	Message string `json:"message"`           // Human-readable error message
	Details string `json:"details,omitempty"` // Additional context
}

// DeliveryAttempt tracks retry attempts for failed message deliveries
type DeliveryAttempt struct {
	ID             uuid.UUID  `json:"id"`
	MessageID      uuid.UUID  `json:"message_id"`
	AttemptNumber  int        `json:"attempt_number"`
	Status         string     `json:"status"` // "pending", "success", "failed", "expired"
	ErrorMessage   *string    `json:"error_message,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	BackoffSeconds int        `json:"backoff_seconds"` // Current backoff duration
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// RetryConfig defines the retry strategy
type RetryConfig struct {
	MaxRetries     int           `json:"max_retries"`     // Default: 6
	InitialBackoff time.Duration `json:"initial_backoff"` // Default: 30s
	MaxBackoff     time.Duration `json:"max_backoff"`     // Default: 6h
	ExpirationTime time.Duration `json:"expiration_time"` // Default: 24h
}

// InboxActivity represents an inbound federated activity
type InboxActivity struct {
	ID           uuid.UUID  `json:"id"`
	ActivityType string     `json:"activity_type"`       // "Follow", "Like", "Post", "Message"
	ActorID      string     `json:"actor_id"`            // Remote user ID
	ActorServer  string     `json:"actor_server"`        // Remote server URL
	TargetID     *string    `json:"target_id,omitempty"` // Local user/object ID
	Payload      string     `json:"payload"`             // JSON string
	ReceivedAt   time.Time  `json:"received_at"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
	ProcessedBy  *string    `json:"processed_by,omitempty"` // Handler that processed it
	Status       string     `json:"status"`                 // "received", "processing", "processed", "failed"
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// OutboxActivity represents an outbound federated activity
type OutboxActivity struct {
	ID             uuid.UUID  `json:"id"`
	ActivityType   string     `json:"activity_type"`
	ActorID        string     `json:"actor_id"`            // Local user ID
	TargetServer   string     `json:"target_server"`       // Destination server URL
	TargetID       *string    `json:"target_id,omitempty"` // Remote user/object ID
	Payload        string     `json:"payload"`             // JSON string
	DeliveryStatus string     `json:"delivery_status"`     // "pending", "delivered", "failed", "expired"
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// InboxRequest for posting to inbox endpoint
type InboxRequest struct {
	ActivityType string                 `json:"activity_type"`
	Actor        string                 `json:"actor"`
	ActorServer  string                 `json:"actor_server"`
	Target       *string                `json:"target,omitempty"`
	Payload      map[string]interface{} `json:"payload"`
	Signature    *string                `json:"signature,omitempty"`
}

// DeliveryAcknowledgment represents a receipt confirmation
type DeliveryAcknowledgment struct {
	ID             uuid.UUID `json:"id"`
	MessageID      uuid.UUID `json:"message_id"`       // Original message ID
	SenderServer   string    `json:"sender_server"`    // Who sent the original message
	ReceiverServer string    `json:"receiver_server"`  // Who is acknowledging
	Status         string    `json:"status"`           // "received", "processed", "rejected"
	Reason         *string   `json:"reason,omitempty"` // If rejected, why
	CreatedAt      time.Time `json:"created_at"`
}

// AcknowledgmentRequest for sending acknowledgments
type AcknowledgmentRequest struct {
	MessageID uuid.UUID `json:"message_id"`
	Status    string    `json:"status"`
	Reason    *string   `json:"reason,omitempty"`
}

// RateLimit defines rate limiting configuration and state
type RateLimit struct {
	ID              uuid.UUID  `json:"id"`
	ServerURL       string     `json:"server_url"`       // Target server, or "*" for global
	Endpoint        string     `json:"endpoint"`         // Endpoint path, or "*" for all
	RequestsPerMin  int        `json:"requests_per_min"` // Max requests per minute
	BurstAllowance  int        `json:"burst_allowance"`  // Burst allowance
	CurrentCount    int        `json:"current_count"`    // Current request count
	WindowStartedAt time.Time  `json:"window_started_at"`
	LastRequestAt   *time.Time `json:"last_request_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// RateLimitConfig for setting rate limits
type RateLimitConfig struct {
	ServerURL      string `json:"server_url"`
	Endpoint       string `json:"endpoint"`
	RequestsPerMin int    `json:"requests_per_min"`
	BurstAllowance int    `json:"burst_allowance"`
}

// SerializedActivity represents a canonically serialized activity
type SerializedActivity struct {
	Version   string                 `json:"@version"` // Protocol version
	Type      string                 `json:"@type"`    // Activity type
	ID        string                 `json:"id"`
	Actor     string                 `json:"actor"`
	Published time.Time              `json:"published"`
	Object    map[string]interface{} `json:"object,omitempty"`
	Target    *string                `json:"target,omitempty"`
	Context   map[string]interface{} `json:"@context,omitempty"`
}

// ServerCapabilities advertises supported features
type ServerCapabilities struct {
	ID               uuid.UUID `json:"id"`
	ServerURL        string    `json:"server_url"`
	ProtocolVersions string    `json:"protocol_versions"` // JSON array: ["1.0.0", "1.1.0"]
	SupportedTypes   string    `json:"supported_types"`   // JSON array: ["Follow", "Like", "Post"]
	MaxMessageSize   int       `json:"max_message_size"`  // In bytes
	SupportsRetries  bool      `json:"supports_retries"`
	SupportsAcks     bool      `json:"supports_acks"`
	RateLimitInfo    *string   `json:"rate_limit_info,omitempty"` // JSON object with limits
	CustomFeatures   *string   `json:"custom_features,omitempty"` // JSON object
	LastDiscoveredAt time.Time `json:"last_discovered_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CapabilityRequest for capability discovery
type CapabilityRequest struct {
	ServerURL string `json:"server_url"`
}

// FederationBlockedServer represents a server on the federation blocklist
type FederationBlockedServer struct {
	ID        uuid.UUID  `json:"id"`
	ServerURL string     `json:"server_url"`
	Reason    string     `json:"reason"`     // Why blocked
	BlockedBy string     `json:"blocked_by"` // Admin who blocked
	BlockedAt time.Time  `json:"blocked_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Optional expiration
	IsActive  bool       `json:"is_active"`            // Can be temporarily disabled
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// BlockServerRequest for blocking a server
type BlockServerRequest struct {
	ServerURL string     `json:"server_url"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// FederationConfig stores federation mode and settings
type FederationConfig struct {
	ID                   uuid.UUID `json:"id"`
	Mode                 string    `json:"mode"`                   // "soft", "hard"
	AllowUnknownServers  bool      `json:"allow_unknown_servers"`  // Only in soft mode
	RequireCapabilityNeg bool      `json:"require_capability_neg"` // Required in hard mode
	StrictValidation     bool      `json:"strict_validation"`
	LogUnknownServers    bool      `json:"log_unknown_servers"`
	AutoBlockMalicious   bool      `json:"auto_block_malicious"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// FederationModeRequest for changing mode
type FederationModeRequest struct {
	Mode                 string `json:"mode"` // "soft" or "hard"
	AllowUnknownServers  *bool  `json:"allow_unknown_servers,omitempty"`
	RequireCapabilityNeg *bool  `json:"require_capability_neg,omitempty"`
	StrictValidation     *bool  `json:"strict_validation,omitempty"`
}

// InstanceHealth represents the health status of the federation instance
type InstanceHealth struct {
	ID                   uuid.UUID `json:"id"`
	Status               string    `json:"status"` // "healthy", "degraded", "unhealthy"
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

// HealthResponse for the health endpoint
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
