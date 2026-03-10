package identity

import (
	"log"
)

func ApplyMigrations() {
	schemas := []string{

		// ── Extensions & helpers ───────────────────────────────────────────────────
		`CREATE EXTENSION IF NOT EXISTS pgcrypto;`,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`,

		// ── CORE BASE TABLES ──────────────────────────────────────────────────────
		// These tables MUST be created first — all other tables depend on them.
		// Previously they were only in SQL files that were never executed at startup.

		`CREATE TABLE IF NOT EXISTS identities (
			id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			did              TEXT,
			user_id          TEXT NOT NULL UNIQUE,
			home_server      TEXT NOT NULL,
			public_key       TEXT NOT NULL DEFAULT '',
			private_key      TEXT DEFAULT '',
			key_version      INTEGER DEFAULT 1,
			recovery_key_hash TEXT DEFAULT '',
			password_hash    TEXT DEFAULT '',
			client_public_key TEXT,
			allow_discovery  BOOLEAN DEFAULT true,
			created_at       TIMESTAMP DEFAULT NOW(),
			updated_at       TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS profiles (
			user_id              TEXT PRIMARY KEY,
			display_name         TEXT NOT NULL DEFAULT '',
			avatar_url           TEXT,
			banner_url           TEXT,
			bio                  TEXT,
			portfolio_url        TEXT,
			birth_date           DATE,
			location             TEXT,
			followers_visibility TEXT DEFAULT 'public',
			following_visibility TEXT DEFAULT 'public',
			version              INTEGER DEFAULT 1,
			created_at           TIMESTAMP DEFAULT NOW(),
			updated_at           TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS posts (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			author     TEXT NOT NULL,
			content    TEXT NOT NULL,
			image_url  TEXT,
			visibility TEXT NOT NULL DEFAULT 'PUBLIC',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS activities (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			actor_id    TEXT NOT NULL,
			verb        TEXT NOT NULL,
			object_type TEXT,
			object_id   TEXT,
			target_id   TEXT,
			payload     JSONB,
			created_at  TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS follows (
			follower_user_id    TEXT NOT NULL,
			follower_home_server TEXT NOT NULL DEFAULT 'localhost',
			followee_user_id    TEXT NOT NULL,
			followee_home_server TEXT NOT NULL DEFAULT 'localhost',
			created_at          TIMESTAMP DEFAULT NOW(),
			updated_at          TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (follower_user_id, followee_user_id)
		);`,

		`CREATE TABLE IF NOT EXISTS messages (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			sender_id      TEXT NOT NULL,
			recipient_id   TEXT NOT NULL,
			content        TEXT NOT NULL,
			image_url      TEXT,
			is_federated   BOOLEAN NOT NULL DEFAULT FALSE,
			origin_server  TEXT,
			created_at     TIMESTAMP DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_sender    ON messages(sender_id);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_recipient ON messages(recipient_id);`,

		// Idempotent rename: if legacy sender/receiver columns exist, rename them.
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='messages' AND column_name='sender' AND column_name NOT IN (SELECT column_name FROM information_schema.columns WHERE table_name='messages' AND column_name='sender_id')) THEN
				ALTER TABLE messages RENAME COLUMN sender TO sender_id;
			END IF;
		EXCEPTION WHEN others THEN NULL; END $$;`,
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='messages' AND column_name='receiver') THEN
				ALTER TABLE messages RENAME COLUMN receiver TO recipient_id;
			END IF;
		EXCEPTION WHEN others THEN NULL; END $$;`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_federated  BOOLEAN NOT NULL DEFAULT FALSE;`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS origin_server TEXT;`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS image_url     TEXT;`,

		`CREATE TABLE IF NOT EXISTS notifications (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			recipient_id   TEXT NOT NULL,
			actor_id       TEXT,
			type           TEXT NOT NULL,
			entity_id      TEXT,
			activity_stream JSONB,
			is_read        BOOLEAN DEFAULT FALSE,
			created_at     TIMESTAMP DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_recipient ON notifications(recipient_id);`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_is_read   ON notifications(is_read);`,

		`CREATE TABLE IF NOT EXISTS block_events (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			blocker_id TEXT NOT NULL,
			blocked_id TEXT NOT NULL,
			reason     TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(blocker_id, blocked_id)
		);`,

		`CREATE TABLE IF NOT EXISTS migration_status (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			from_db         TEXT NOT NULL,
			to_db           TEXT NOT NULL,
			status          TEXT NOT NULL,
			tables_migrated JSONB,
			error_message   TEXT,
			started_at      TIMESTAMP DEFAULT NOW(),
			completed_at    TIMESTAMP
		);`,

		`CREATE INDEX IF NOT EXISTS idx_posts_author  ON posts(author);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_visibility ON posts(visibility);`,
		`CREATE INDEX IF NOT EXISTS idx_activities_actor ON activities(actor_id);`,
		`CREATE INDEX IF NOT EXISTS idx_block_events_blocker ON block_events(blocker_id);`,

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

		// Moderation results for AI-flagged content
		`CREATE TABLE IF NOT EXISTS moderation_results (
			content_id      TEXT PRIMARY KEY,
			content_type    TEXT NOT NULL,
			toxicity_score  FLOAT NOT NULL,
			recommendation  TEXT NOT NULL,
			review_status   TEXT NOT NULL DEFAULT 'PENDING'
		);`,

		// Server snapshots for admin dashboard trend charts
		`CREATE TABLE IF NOT EXISTS server_snapshots (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			total_users   INT NOT NULL DEFAULT 0,
			total_posts   INT NOT NULL DEFAULT 0,
			total_follows INT NOT NULL DEFAULT 0,
			captured_at   TIMESTAMP DEFAULT NOW()
		);`,

		// Drop FK constraints on messages so cross-server messages can be stored.
		// Remote senders are not in the local identities table → FK violation on insert.
		`ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_sender_fkey;`,
		`ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_receiver_fkey;`,

		// Indexes for efficient conversation lookups (using correct renamed columns)
		`CREATE INDEX IF NOT EXISTS idx_messages_sender_id_recipient_id ON messages(sender_id, recipient_id);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_recipient_id ON messages(recipient_id);`,

		// Drop FK constraints on follows so cross-server follow rows can be stored.
		// When Server A follows Server B user, the followee_user_id (bob@server_b)
		// is not in server_a's identities table, and vice-versa on server_b.
		`ALTER TABLE follows DROP CONSTRAINT IF EXISTS follows_follower_user_id_fkey;`,
		`ALTER TABLE follows DROP CONSTRAINT IF EXISTS follows_followee_user_id_fkey;`,

		// ── server_config ─────────────────────────────────────────────────────────
		// Used by admin to store server name and other runtime settings.
		// MISSING from original migrations — caused GetServerConfig() to fail with
		// "relation server_config does not exist", returning 500 on /admin/config/server.
		`CREATE TABLE IF NOT EXISTS server_config (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_by TEXT,
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		// Insert a blank placeholder row — SeedServerConfig() (called from main)
		// will UPSERT the correct value from the SERVER_NAME env var.
		`INSERT INTO server_config (key, value, updated_by, updated_at)
		 VALUES ('server_name', '', 'system', NOW())
		 ON CONFLICT (key) DO NOTHING;`,

		// ── Reports (moderation package) ─────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS reports (
			id           BIGSERIAL PRIMARY KEY,
			reporter_id  TEXT NOT NULL,
			target_ref   TEXT NOT NULL,
			target_server TEXT,
			reason       TEXT NOT NULL,
			status       TEXT NOT NULL DEFAULT 'PENDING',
			created_at   TIMESTAMP DEFAULT NOW(),
			resolved_at  TIMESTAMP,
			resolved_by  TEXT
		);`,

		// ── Blocked servers (moderation package) ─────────────────────────────────
		`CREATE TABLE IF NOT EXISTS blocked_servers (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_url  TEXT NOT NULL UNIQUE,
			reason      TEXT,
			blocked_by  TEXT,
			blocked_at  TIMESTAMP DEFAULT NOW(),
			is_active   BOOLEAN DEFAULT TRUE,
			created_at  TIMESTAMP DEFAULT NOW(),
			updated_at  TIMESTAMP DEFAULT NOW()
		);`,

		// ── User blocks (moderation package) ─────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS user_blocks (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			blocker_user_id TEXT NOT NULL,
			blocked_user_id TEXT NOT NULL,
			reason          TEXT,
			expires_at      TIMESTAMP,
			is_active       BOOLEAN DEFAULT TRUE,
			created_at      TIMESTAMP DEFAULT NOW(),
			UNIQUE (blocker_user_id, blocked_user_id)
		);`,

		// ── Federation events queue (moderation package) ──────────────────────────
		`CREATE TABLE IF NOT EXISTS federation_events (
			id            BIGSERIAL PRIMARY KEY,
			event_type    TEXT NOT NULL,
			target_server TEXT NOT NULL,
			payload       JSONB NOT NULL DEFAULT '{}',
			retry_count   INT NOT NULL DEFAULT 0,
			last_tried_at TIMESTAMP,
			created_at    TIMESTAMP DEFAULT NOW()
		);`,

		// ── Backup metadata (moderation package) ─────────────────────────────────
		`CREATE TABLE IF NOT EXISTS backup_metadata (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			location   TEXT NOT NULL,
			created_by TEXT,
			created_at TIMESTAMP DEFAULT NOW()
		);`,

		// ── Invites system ────────────────────────────────────────────────────────
		// Required by invites.go; was MISSING from migrations — caused
		// "ERR > Failed to fetch invites" on admin dashboard.
		`CREATE TABLE IF NOT EXISTS invites (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			invite_code  TEXT NOT NULL UNIQUE,
			invite_type  TEXT NOT NULL DEFAULT 'user',
			created_by   TEXT NOT NULL,
			max_uses     INT NOT NULL DEFAULT 1,
			current_uses INT NOT NULL DEFAULT 0,
			expires_at   TIMESTAMP,
			revoked      BOOLEAN NOT NULL DEFAULT FALSE,
			metadata     TEXT,
			created_at   TIMESTAMP DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS invite_usage (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			invite_id   UUID NOT NULL REFERENCES invites(id) ON DELETE CASCADE,
			user_id     TEXT NOT NULL,
			ip_address  TEXT,
			user_agent  TEXT,
			used_at     TIMESTAMP DEFAULT NOW()
		);`,

		// ── Server identity (full schema) ─────────────────────────────────────────
		// init.go InsertServer uses: server_id, server_name, public_key, private_key_encrypted
		// init.go CheckInitializationStatus uses: initialized
		// invites QR uses: server_id, server_name, public_key, endpoint
		`CREATE TABLE IF NOT EXISTS server_identity (
			id                    INT PRIMARY KEY DEFAULT 1,
			server_id             TEXT NOT NULL DEFAULT '',
			server_name           TEXT NOT NULL DEFAULT '',
			public_key            TEXT NOT NULL DEFAULT '',
			private_key           TEXT,
			private_key_encrypted TEXT,
			endpoint              TEXT NOT NULL DEFAULT '',
			initialized           BOOLEAN NOT NULL DEFAULT FALSE,
			created_at            TIMESTAMP DEFAULT NOW(),
			updated_at            TIMESTAMP DEFAULT NOW(),
			CONSTRAINT server_identity_singleton CHECK (id = 1)
		);`,

		// Add missing columns to existing server_identity rows (idempotent)
		`ALTER TABLE server_identity ADD COLUMN IF NOT EXISTS initialized           BOOLEAN NOT NULL DEFAULT FALSE;`,
		`ALTER TABLE server_identity ADD COLUMN IF NOT EXISTS private_key           TEXT;`,
		`ALTER TABLE server_identity ADD COLUMN IF NOT EXISTS private_key_encrypted TEXT;`,
		`ALTER TABLE server_identity ADD COLUMN IF NOT EXISTS endpoint              TEXT NOT NULL DEFAULT '';`,

		// Ensure a default row exists for server_identity (singleton pattern)
		`INSERT INTO server_identity (id, server_id, server_name, public_key, endpoint, initialized)
		 VALUES (1, '', '', '', '', FALSE)
		 ON CONFLICT (id) DO NOTHING;`,

		// ── Admins table ──────────────────────────────────────────────────────────
		// Used by InitializeServer (init.go) to create the first admin account.
		// MISSING from migrations — caused InsertServer to fail on the admins INSERT.
		`CREATE TABLE IF NOT EXISTS admins (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username       TEXT NOT NULL UNIQUE,
			password_hash  TEXT NOT NULL,
			is_super_admin BOOLEAN NOT NULL DEFAULT FALSE,
			created_by     UUID,
			created_at     TIMESTAMP DEFAULT NOW(),
			updated_at     TIMESTAMP DEFAULT NOW()
		);`,

		// ── Moderator roles table ─────────────────────────────────────────────────
		// Used by ModeratorAuthMiddleware, AssignModeratorHandler, ListModeratorsHandler.
		// MISSING from migrations — caused all moderator route lookups to fail.
		`CREATE TABLE IF NOT EXISTS moderator_roles (
			user_id     TEXT PRIMARY KEY,
			username    TEXT NOT NULL DEFAULT '',
			assigned_by TEXT NOT NULL DEFAULT 'admin',
			assigned_at TIMESTAMP DEFAULT NOW()
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
