-- Federation System Database Schema
-- Run this with: psql $DATABASE_URL < migrations.sql

-- ============================================================================
-- User Story 2.1: Federation Protocol Design
-- ============================================================================

CREATE TABLE IF NOT EXISTS federation_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version VARCHAR(20) NOT NULL,
    message_type VARCHAR(50) NOT NULL,
    sender_server VARCHAR(255) NOT NULL,
    receiver_server VARCHAR(255) NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    signature TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_federation_messages_sender ON federation_messages(sender_server);
CREATE INDEX idx_federation_messages_receiver ON federation_messages(receiver_server);
CREATE INDEX idx_federation_messages_type ON federation_messages(message_type);
CREATE INDEX idx_federation_messages_timestamp ON federation_messages(timestamp DESC);

-- ============================================================================
-- User Story 2.3: Secure Delivery with Retries
-- ============================================================================

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES federation_messages(id) ON DELETE CASCADE,
    attempt_number INT NOT NULL,
    status VARCHAR(20) NOT NULL, -- 'pending', 'success', 'failed', 'expired'
    error_message TEXT,
    next_retry_at TIMESTAMPTZ,
    backoff_seconds INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT delivery_attempts_message_attempt UNIQUE(message_id, attempt_number)
);

CREATE INDEX idx_delivery_attempts_message ON delivery_attempts(message_id);
CREATE INDEX idx_delivery_attempts_status ON delivery_attempts(status);
CREATE INDEX idx_delivery_attempts_next_retry ON delivery_attempts(next_retry_at) WHERE status = 'pending';

-- ============================================================================
-- User Story 2.4: Inbox / Outbox Architecture
-- ============================================================================

CREATE TABLE IF NOT EXISTS inbox_activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_type VARCHAR(50) NOT NULL,
    actor_id VARCHAR(255) NOT NULL,
    actor_server VARCHAR(255) NOT NULL,
    target_id VARCHAR(255),
    payload JSONB NOT NULL,
    received_at TIMESTAMPTZ DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    processed_by VARCHAR(100),
    status VARCHAR(20) NOT NULL DEFAULT 'received', -- 'received', 'processing', 'processed', 'failed'
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_inbox_activities_actor ON inbox_activities(actor_id, actor_server);
CREATE INDEX idx_inbox_activities_target ON inbox_activities(target_id);
CREATE INDEX idx_inbox_activities_status ON inbox_activities(status);
CREATE INDEX idx_inbox_activities_received ON inbox_activities(received_at DESC);

CREATE TABLE IF NOT EXISTS outbox_activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_type VARCHAR(50) NOT NULL,
    actor_id VARCHAR(255) NOT NULL,
    target_server VARCHAR(255) NOT NULL,
    target_id VARCHAR(255),
    payload JSONB NOT NULL,
    delivery_status VARCHAR(20) NOT NULL DEFAULT 'pending', -- 'pending', 'delivered', 'failed', 'expired'
    delivered_at TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_outbox_activities_actor ON outbox_activities(actor_id);
CREATE INDEX idx_outbox_activities_target ON outbox_activities(target_server);
CREATE INDEX idx_outbox_activities_status ON outbox_activities(delivery_status);
CREATE INDEX idx_outbox_activities_created ON outbox_activities(created_at DESC);

-- ============================================================================
-- User Story 2.7: Delivery Acknowledgment
-- ============================================================================

CREATE TABLE IF NOT EXISTS delivery_acknowledgments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL,
    sender_server VARCHAR(255) NOT NULL,
    receiver_server VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL, -- 'received', 'processed', 'rejected'
    reason TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT acknowledgments_message_receiver UNIQUE(message_id, receiver_server)
);

CREATE INDEX idx_acks_message ON delivery_acknowledgments(message_id);
CREATE INDEX idx_acks_sender ON delivery_acknowledgments(sender_server);
CREATE INDEX idx_acks_status ON delivery_acknowledgments(status);

-- ============================================================================
-- User Story 2.8: Rate Limiting
-- ============================================================================

CREATE TABLE IF NOT EXISTS rate_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_url VARCHAR(255) NOT NULL,
    endpoint VARCHAR(100) NOT NULL,
    requests_per_min INT NOT NULL,
    burst_allowance INT NOT NULL,
    current_count INT DEFAULT 0,
    window_started_at TIMESTAMPTZ DEFAULT NOW(),
    last_request_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT rate_limits_server_endpoint UNIQUE(server_url, endpoint)
);

CREATE INDEX idx_rate_limits_server ON rate_limits(server_url);
CREATE INDEX idx_rate_limits_window ON rate_limits(window_started_at);

-- ============================================================================
-- User Story 2.11: Capability Negotiation
-- ============================================================================

CREATE TABLE IF NOT EXISTS server_capabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_url VARCHAR(255) NOT NULL UNIQUE,
    protocol_versions JSONB NOT NULL, -- ["1.0.0", "1.1.0"]
    supported_types JSONB NOT NULL,   -- ["Follow", "Like", "Post"]
    max_message_size INT NOT NULL,
    supports_retries BOOLEAN DEFAULT false,
    supports_acks BOOLEAN DEFAULT false,
    rate_limit_info JSONB,
    custom_features JSONB,
    last_discovered_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_capabilities_server ON server_capabilities(server_url);
CREATE INDEX idx_capabilities_discovered ON server_capabilities(last_discovered_at DESC);

-- ============================================================================
-- User Story 2.12: Blocked Server Lists
-- ============================================================================

CREATE TABLE IF NOT EXISTS blocked_servers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_url VARCHAR(255) NOT NULL UNIQUE,
    reason TEXT NOT NULL,
    blocked_by VARCHAR(100) NOT NULL,
    blocked_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_blocked_servers_url ON blocked_servers(server_url);
CREATE INDEX idx_blocked_servers_active ON blocked_servers(is_active) WHERE is_active = true;
CREATE INDEX idx_blocked_servers_expires ON blocked_servers(expires_at) WHERE expires_at IS NOT NULL;

-- ============================================================================
-- User Story 2.13: Soft / Hard Federation Modes
-- ============================================================================

CREATE TABLE IF NOT EXISTS federation_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode VARCHAR(20) NOT NULL DEFAULT 'soft', -- 'soft', 'hard'
    allow_unknown_servers BOOLEAN DEFAULT true,
    require_capability_neg BOOLEAN DEFAULT false,
    strict_validation BOOLEAN DEFAULT false,
    log_unknown_servers BOOLEAN DEFAULT true,
    auto_block_malicious BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default configuration
INSERT INTO federation_config (mode, allow_unknown_servers, require_capability_neg, strict_validation, log_unknown_servers)
VALUES ('soft', true, false, false, true)
ON CONFLICT DO NOTHING;

-- ============================================================================
-- User Story 2.14: Instance Health API
-- ============================================================================

CREATE TABLE IF NOT EXISTS instance_health (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status VARCHAR(20) NOT NULL DEFAULT 'healthy', -- 'healthy', 'degraded', 'unhealthy'
    total_messages BIGINT DEFAULT 0,
    successful_deliveries BIGINT DEFAULT 0,
    failed_deliveries BIGINT DEFAULT 0,
    pending_retries BIGINT DEFAULT 0,
    average_latency_ms INT DEFAULT 0,
    active_connections INT DEFAULT 0,
    blocked_servers_count INT DEFAULT 0,
    rate_limit_violations BIGINT DEFAULT 0,
    uptime_seconds BIGINT DEFAULT 0,
    last_health_check_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert initial health record
INSERT INTO instance_health (status)
VALUES ('healthy')
ON CONFLICT DO NOTHING;

-- ============================================================================
-- Insert default rate limits
-- ============================================================================

INSERT INTO rate_limits (server_url, endpoint, requests_per_min, burst_allowance)
VALUES 
    ('*', '*', 100, 20),           -- Global default
    ('*', '/federation/inbox', 50, 10),
    ('*', '/federation/send', 50, 10)
ON CONFLICT DO NOTHING;
