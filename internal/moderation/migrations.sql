-- REPORTS
CREATE TABLE IF NOT EXISTS reports (
    id BIGSERIAL PRIMARY KEY,
    reporter_id TEXT NOT NULL,
    target_ref TEXT NOT NULL,
    target_server TEXT NOT NULL,
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
    resolved_at TIMESTAMP WITHOUT TIME ZONE,
    resolved_by TEXT
);

CREATE INDEX IF NOT EXISTS idx_reports_status
    ON reports(status);

CREATE INDEX IF NOT EXISTS idx_reports_target_server
    ON reports(target_server);

CREATE INDEX IF NOT EXISTS idx_reports_created_at
    ON reports(created_at DESC);


-- FEDERATION EVENTS QUEUE
CREATE TABLE IF NOT EXISTS federation_events (
    id BIGSERIAL PRIMARY KEY,
    event_type TEXT NOT NULL,
    target_server TEXT NOT NULL,
    payload BYTEA NOT NULL,
    retry_count INTEGER DEFAULT 0,
    created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
    last_tried_at TIMESTAMP WITHOUT TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_federation_events_target
    ON federation_events(target_server);

CREATE INDEX IF NOT EXISTS idx_federation_events_retry
    ON federation_events(retry_count);


-- BACKUP METADATA
CREATE TABLE IF NOT EXISTS backup_metadata (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
    location TEXT NOT NULL,
    created_by TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_backup_created_at
    ON backup_metadata(created_at DESC);


-- BLOCKED SERVERS
CREATE TABLE IF NOT EXISTS blocked_servers (
    id BIGSERIAL PRIMARY KEY,
    server_url TEXT NOT NULL UNIQUE,
    reason TEXT NOT NULL,
    blocked_by TEXT NOT NULL,
    blocked_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_blocked_servers_url
    ON blocked_servers(server_url);
