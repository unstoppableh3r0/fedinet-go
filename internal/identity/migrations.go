package identity

import (
	"embed"
	"log"
	"path/filepath"
	"sort"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func ApplyMigrations() {
	// Execute embedded SQL files first
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		log.Printf("Warning: Failed to read migration directory: %v", err)
	} else {
		var files []string
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
				files = append(files, entry.Name())
			}
		}
		sort.Strings(files)

		for _, file := range files {
			content, err := migrationFiles.ReadFile("migrations/" + file)
			if err != nil {
				log.Printf("Warning: Failed to read migration %s: %v", file, err)
				continue
			}
			log.Printf("Applying identity migration: %s", file)
			if _, err := db.Exec(string(content)); err != nil {
				log.Printf("Migration Warning (%s) - might already exist: %v", file, err)
			}
		}
	}
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
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS client_public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS allow_discovery BOOLEAN DEFAULT true;`,

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

		// Trusted servers for federation
		`CREATE TABLE IF NOT EXISTS trusted_servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_id TEXT NOT NULL UNIQUE,
			server_name TEXT NOT NULL,
			public_key TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL,
			trusted_at TIMESTAMP DEFAULT NOW()
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

		// ActivityStreams 2.0 payload column (added after initial schema)
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS activity_stream JSONB;`,

		// Image URL support for posts and messages
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS image_url TEXT;`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS image_url TEXT;`,

		// Cache table for profile data received from federated (remote) servers.
		// Keyed by the internal user_id (e.g. alice@server_b).
		// Federated messaging columns — added after initial messages schema
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_federated BOOLEAN DEFAULT FALSE;`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS origin_server TEXT;`,

		`CREATE TABLE IF NOT EXISTS remote_profiles (
			user_id       TEXT PRIMARY KEY,
			display_name  TEXT NOT NULL DEFAULT '',
			bio           TEXT NOT NULL DEFAULT '',
			avatar_url    TEXT NOT NULL DEFAULT '',
			banner_url    TEXT NOT NULL DEFAULT '',
			location      TEXT NOT NULL DEFAULT '',
			portfolio_url TEXT NOT NULL DEFAULT '',
			version       INT  NOT NULL DEFAULT 0,
			updated_at    TIMESTAMP DEFAULT NOW()
		);`,

		// Moderator role management table
		`CREATE TABLE IF NOT EXISTS moderator_roles (
			user_id     TEXT PRIMARY KEY,
			username    TEXT NOT NULL,
			assigned_by TEXT NOT NULL,
			assigned_at TIMESTAMP DEFAULT NOW()
		);`,

		// Ensure posts have a visibility column (PUBLIC | HIDDEN | PENDING_REVIEW)
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'PUBLIC';`,

		// Index for quickly querying posts pending moderation review
		`CREATE INDEX IF NOT EXISTS idx_posts_visibility ON posts(visibility);`,

		// Stats snapshots for admin dashboard trend charts
		`CREATE TABLE IF NOT EXISTS server_stats_snapshots (
			id          BIGSERIAL PRIMARY KEY,
			total_users INT NOT NULL DEFAULT 0,
			total_posts INT NOT NULL DEFAULT 0,
			total_follows INT NOT NULL DEFAULT 0,
			created_at  TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_stats_snapshots_created_at ON server_stats_snapshots(created_at DESC);`,

		// User privacy settings
		`CREATE TABLE IF NOT EXISTS privacy_settings (
			user_id                   TEXT PRIMARY KEY,
			search_local              TEXT NOT NULL DEFAULT 'everyone',
			search_federated          TEXT NOT NULL DEFAULT 'everyone',
			posts_visibility          TEXT NOT NULL DEFAULT 'public',
			likes_visibility          TEXT NOT NULL DEFAULT 'public',
			replies_visibility        TEXT NOT NULL DEFAULT 'public',
			following_list_visibility TEXT NOT NULL DEFAULT 'public',
			followers_list_visibility TEXT NOT NULL DEFAULT 'public',
			created_at                TIMESTAMP DEFAULT NOW(),
			updated_at                TIMESTAMP DEFAULT NOW(),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Account links: bidirectional connection requests (friend-request style)
		`CREATE TABLE IF NOT EXISTS account_links (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			requester_id TEXT NOT NULL,
			target_id    TEXT NOT NULL,
			status       TEXT NOT NULL DEFAULT 'pending',
			created_at   TIMESTAMP DEFAULT NOW(),
			updated_at   TIMESTAMP DEFAULT NOW(),
			UNIQUE (requester_id, target_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_account_links_requester ON account_links(requester_id);`,
		`CREATE INDEX IF NOT EXISTS idx_account_links_target ON account_links(target_id);`,

		// User badge column: 'user' | 'moderator' | 'admin'
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS badge TEXT NOT NULL DEFAULT 'user'`,

		// Moderation feature toggle stored in server_config
		`INSERT INTO server_config (key, value, updated_by, updated_at)
		 VALUES ('moderation_enabled', 'true', 'system', NOW())
		 ON CONFLICT (key) DO NOTHING`,

		// Reply visibility for moderation (HIDDEN while under review, PUBLIC/REJECTED after decision)
		`ALTER TABLE replies ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'PUBLIC'`,
		`CREATE INDEX IF NOT EXISTS idx_replies_visibility ON replies(visibility)`,

		// Encrypted group messaging — each group has a unique AES-256 key stored
		// wrapped with SERVER_MASTER_KEY so plaintext never appears in the DB.
		`CREATE TABLE IF NOT EXISTS group_chats (
			id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name                TEXT NOT NULL,
			created_by          TEXT NOT NULL,
			encrypted_group_key TEXT NOT NULL,
			created_at          TIMESTAMP DEFAULT now(),
			updated_at          TIMESTAMP DEFAULT now()
		)`,

		`CREATE TABLE IF NOT EXISTS group_members (
			group_id  UUID NOT NULL REFERENCES group_chats(id) ON DELETE CASCADE,
			user_id   TEXT NOT NULL,
			role      TEXT NOT NULL DEFAULT 'member',
			joined_at TIMESTAMP DEFAULT now(),
			PRIMARY KEY (group_id, user_id)
		)`,

		`CREATE TABLE IF NOT EXISTS group_messages (
			id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			group_id          UUID NOT NULL REFERENCES group_chats(id) ON DELETE CASCADE,
			sender_id         TEXT NOT NULL,
			encrypted_content TEXT NOT NULL,
			created_at        TIMESTAMP DEFAULT now()
		)`,

		`CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_messages_group_time ON group_messages(group_id, created_at DESC)`,

		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS content_warning TEXT`,

		// Linked multi-server posting: group identity for replica posts
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS group_id TEXT`,
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS origin_post TEXT`,
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS origin_server TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_posts_group ON posts(group_id)`,

		`CREATE TABLE IF NOT EXISTS privacy_audit_logs (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_type TEXT        NOT NULL,
			actor_id   TEXT        NOT NULL,
			target_id  TEXT        NOT NULL DEFAULT '',
			detail     TEXT        NOT NULL DEFAULT '{}',
			ip_addr    TEXT        NOT NULL DEFAULT '',
			created_at TIMESTAMP   NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_privacy_audit_logs_actor ON privacy_audit_logs(actor_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_privacy_audit_logs_time  ON privacy_audit_logs(created_at DESC)`,

		// DM at-rest encryption flag and group encryption policy
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_encrypted BOOLEAN NOT NULL DEFAULT false`,
		`INSERT INTO server_config (key, value, updated_by, updated_at)
		 VALUES ('dm_encryption_at_rest', 'false', 'system', NOW())
		 ON CONFLICT (key) DO NOTHING`,
		`INSERT INTO server_config (key, value, updated_by, updated_at)
		 VALUES ('require_encrypted_groups', 'true', 'system', NOW())
		 ON CONFLICT (key) DO NOTHING`,

		// Zero-knowledge identity verification
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS zkp_public_key TEXT`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS zkp_last_proved_at TIMESTAMPTZ`,
		`CREATE TABLE IF NOT EXISTS zkp_challenges (
			id         TEXT        PRIMARY KEY,
			user_id    TEXT        NOT NULL,
			challenge  TEXT        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL,
			used       BOOLEAN     NOT NULL DEFAULT FALSE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_zkp_challenges_user ON zkp_challenges(user_id, expires_at)`,

		// Remote post cache: stores feed slices fetched from other servers so that
		// timeline requests are served locally without a live HTTP round-trip.
		`CREATE TABLE IF NOT EXISTS remote_post_cache (
			user_id       TEXT        NOT NULL,
			remote_server TEXT        NOT NULL,
			posts_json    JSONB       NOT NULL DEFAULT '[]',
			fetched_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at    TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (user_id, remote_server)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_remote_post_cache_expires ON remote_post_cache(expires_at)`,

		// Passkeys (WebAuthn) — primary passwordless login
		`CREATE TABLE IF NOT EXISTS passkeys (
			id            BIGSERIAL    PRIMARY KEY,
			user_id       TEXT         NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
			credential_id BYTEA        NOT NULL UNIQUE,
			public_key    BYTEA        NOT NULL,
			sign_count    BIGINT       NOT NULL DEFAULT 0,
			aaguid        TEXT         NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_passkeys_user_id ON passkeys(user_id)`,

		// Passkey recovery audit log
		`CREATE TABLE IF NOT EXISTS passkey_recovery_attempts (
			id           BIGSERIAL    PRIMARY KEY,
			user_id      TEXT         NOT NULL,
			ip_address   TEXT         NOT NULL DEFAULT '',
			attempted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			succeeded    BOOLEAN      NOT NULL DEFAULT FALSE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recovery_attempts_user_time ON passkey_recovery_attempts(user_id, attempted_at)`,

		// Group join policy: anyone, followers, invite_only (default)
		`ALTER TABLE group_chats ADD COLUMN IF NOT EXISTS join_policy TEXT NOT NULL DEFAULT 'invite_only'`,

		// Expiring posts: allow posts to auto-delete after a set time
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ`,

		// Track activities count in admin dashboard trend chart
		`ALTER TABLE server_stats_snapshots ADD COLUMN IF NOT EXISTS total_activities INT NOT NULL DEFAULT 0`,
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			log.Printf("Migration Warning (might already exist): %v\nQuery: %s", err, schema)
		}
	}

	log.Println("Database migrations applied successfully.")
}
