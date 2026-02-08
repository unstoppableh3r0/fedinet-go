package main

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// User Story 2.1: Federation Protocol Design
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

// ============================================================================
// User Story 2.3: Secure Delivery with Retries
// ============================================================================

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

// ============================================================================
// User Story 2.4: Inbox / Outbox Architecture
// ============================================================================

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

// ============================================================================
// User Story 2.7: Delivery Acknowledgment
// ============================================================================

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

// ============================================================================
// User Story 2.8: Rate Limiting
// ============================================================================

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

// ============================================================================
// User Story 2.10: Content Serialization Format
// ============================================================================

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

// ============================================================================
// User Story 2.11: Capability Negotiation
// ============================================================================

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

// ============================================================================
// User Story 2.12: Blocked Server Lists
// ============================================================================

// BlockedServer represents a server on the blocklist
type BlockedServer struct {
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

// ============================================================================
// User Story 2.13: Soft / Hard Federation Modes
// ============================================================================

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

// ============================================================================
// User Story 2.14: Instance Health API
// ============================================================================

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
