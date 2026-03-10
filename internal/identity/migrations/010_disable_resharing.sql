-- Migration 010: add disable_resharing to privacy_settings
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'privacy_settings') THEN
        ALTER TABLE privacy_settings ADD COLUMN IF NOT EXISTS disable_resharing BOOLEAN NOT NULL DEFAULT false;
    END IF;
END
$$;