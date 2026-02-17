package identity

import (
	"log"
)

func ApplyMigrations() {
	schemas := []string{

		`CREATE TABLE IF NOT EXISTS likes (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS replies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			post_id UUID NOT NULL,
			user_id TEXT NOT NULL,
			content TEXT NOT NULL,
			parent_id UUID,
			created_at TIMESTAMP DEFAULT NOW(),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (parent_id) REFERENCES replies(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS reposts (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

		`CREATE INDEX IF NOT EXISTS idx_reposts_post_id ON reposts(post_id);`,
		`CREATE INDEX IF NOT EXISTS idx_reposts_user_id ON reposts(user_id);`,

		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS private_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS key_version INT DEFAULT 1;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS recovery_key_hash TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS signature TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS metadata JSONB;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS did TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS password_hash TEXT;`,

		`ALTER TABLE profiles ADD COLUMN IF NOT EXISTS version INT DEFAULT 1;`,

		`CREATE TABLE IF NOT EXISTS key_revocations (
			key_id TEXT PRIMARY KEY,
			identity_id UUID NOT NULL,
			reason TEXT,
			revoked_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			FOREIGN KEY (identity_id) REFERENCES identities(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS block_events (
			blocker_id TEXT NOT NULL,
			blocked_id TEXT NOT NULL,
			reason TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			PRIMARY KEY (blocker_id, blocked_id),
			FOREIGN KEY (blocker_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS outbox_activities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			activity_type TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			target_server TEXT NOT NULL,
			target_id TEXT,
			payload JSONB NOT NULL,
			delivery_status TEXT DEFAULT 'pending', -- pending, sent, failed
			delivered_at TIMESTAMP,
			acknowledged_at TIMESTAMP,
			error_message TEXT,
			attempt_count INT DEFAULT 0,
			last_attempt_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS inbox_activities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			activity_type TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			actor_server TEXT NOT NULL,
			target_id TEXT,
			payload JSONB NOT NULL,
			received_at TIMESTAMP DEFAULT NOW(),
			processed_at TIMESTAMP,
			processed_by TEXT,
			status TEXT DEFAULT 'received', -- received, processing, processed, failed
			error_message TEXT,
			created_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS delivery_acknowledgments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL,
			sender_server TEXT NOT NULL,
			receiver_server TEXT NOT NULL,
			status TEXT NOT NULL, -- received, processed, rejected
			reason TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT acknowledgments_message_receiver UNIQUE(message_id, receiver_server)
		);`,

		`CREATE TABLE IF NOT EXISTS rate_limits (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_url TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			requests_per_min INT NOT NULL,
			burst_allowance INT NOT NULL,
			current_count INT DEFAULT 0,
			window_started_at TIMESTAMP DEFAULT NOW(),
			last_request_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT rate_limits_server_endpoint UNIQUE(server_url, endpoint)
		);`,

		`CREATE TABLE IF NOT EXISTS server_capabilities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_url TEXT NOT NULL UNIQUE,
			protocol_versions JSONB NOT NULL,
			supported_types JSONB NOT NULL,
			max_message_size INT NOT NULL,
			supports_retries BOOLEAN DEFAULT false,
			supports_acks BOOLEAN DEFAULT false,
			rate_limit_info JSONB,
			custom_features JSONB,
			last_discovered_at TIMESTAMP DEFAULT NOW(),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS blocked_servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_url TEXT NOT NULL UNIQUE,
			reason TEXT NOT NULL,
			blocked_by TEXT NOT NULL,
			blocked_at TIMESTAMP DEFAULT NOW(),
			expires_at TIMESTAMP,
			is_active BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS federation_config (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			mode TEXT NOT NULL DEFAULT 'soft', -- soft, hard
			allow_unknown_servers BOOLEAN DEFAULT true,
			require_capability_neg BOOLEAN DEFAULT false,
			strict_validation BOOLEAN DEFAULT false,
			log_unknown_servers BOOLEAN DEFAULT true,
			auto_block_malicious BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`INSERT INTO federation_config (mode, allow_unknown_servers, require_capability_neg, strict_validation, log_unknown_servers)
		 VALUES ('soft', true, false, false, true)
		 ON CONFLICT DO NOTHING;`,

		`CREATE TABLE IF NOT EXISTS instance_health (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			status TEXT NOT NULL DEFAULT 'healthy', -- healthy, degraded, unhealthy
			total_messages BIGINT DEFAULT 0,
			successful_deliveries BIGINT DEFAULT 0,
			failed_deliveries BIGINT DEFAULT 0,
			pending_retries BIGINT DEFAULT 0,
			average_latency_ms INT DEFAULT 0,
			active_connections INT DEFAULT 0,
			blocked_servers_count INT DEFAULT 0,
			rate_limit_violations BIGINT DEFAULT 0,
			uptime_seconds BIGINT DEFAULT 0,
			last_health_check_at TIMESTAMP DEFAULT NOW(),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`INSERT INTO instance_health (status) VALUES ('healthy') ON CONFLICT DO NOTHING;`,

		`CREATE TABLE IF NOT EXISTS trusted_servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_id TEXT NOT NULL UNIQUE,
			server_name TEXT NOT NULL,
			public_key TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			trusted_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS federation_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			version TEXT NOT NULL,
			message_type TEXT NOT NULL,
			sender_server TEXT NOT NULL,
			receiver_server TEXT NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			payload JSONB NOT NULL,
			signature TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS delivery_attempts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL REFERENCES federation_messages(id) ON DELETE CASCADE,
			attempt_number INT NOT NULL,
			status TEXT NOT NULL, -- pending, success, failed, expired
			error_message TEXT,
			next_retry_at TIMESTAMP,
			backoff_seconds INT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT delivery_attempts_message_attempt UNIQUE(message_id, attempt_number)
		);`,

		`GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO postgres;`,
		`GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO postgres;`,

		`CREATE INDEX IF NOT EXISTS idx_inbox_activities_actor ON inbox_activities(actor_id, actor_server);`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_activities_target ON inbox_activities(target_id);`,

		`ALTER TABLE outbox_activities ADD COLUMN IF NOT EXISTS target_id TEXT;`,
		`ALTER TABLE outbox_activities ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMP;`,
		`ALTER TABLE outbox_activities ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMP;`,
		`ALTER TABLE outbox_activities ADD COLUMN IF NOT EXISTS error_message TEXT;`,
		`ALTER TABLE outbox_activities ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT NOW();`,

		`CREATE TABLE IF NOT EXISTS notifications (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			recipient_id TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			type TEXT NOT NULL, -- FOLLOW, LIKE, REPLY, REPOST, MESSAGE
			entity_id TEXT, -- ID of the post or user involved
			is_read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			FOREIGN KEY (recipient_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			log.Printf("Migration Warning (might already exist): %v\nQuery: %s", err, schema)
		}
	}

	log.Println("Database migrations applied successfully.")
}
