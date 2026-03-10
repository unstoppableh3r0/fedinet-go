-- 014: Store WebAuthn credential flags (BackupEligible, BackupState)
--
-- go-webauthn v0.10+ checks that the BackupEligible flag stored on a credential
-- matches what the authenticator reports at login time.  Previously we were not
-- persisting these flags, so they defaulted to false.  Modern synced passkeys
-- (iOS Keychain, Google Password Manager, etc.) report BackupEligible=true,
-- which caused FinishLogin to fail with
-- "Backup Eligible flag inconsistency detected during login validation".
--
-- We set existing rows to backup_eligible=TRUE because any passkey that was
-- causing this error must have been registered with BackupEligible=true.
-- New registrations will store the correct value from the authenticator.

ALTER TABLE passkeys ADD COLUMN IF NOT EXISTS backup_eligible BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE passkeys ADD COLUMN IF NOT EXISTS backup_state   BOOLEAN NOT NULL DEFAULT FALSE;

-- Assume all previously registered passkeys were BackupEligible so they
-- continue to work without requiring re-registration.
UPDATE passkeys SET backup_eligible = TRUE WHERE backup_eligible = FALSE;
