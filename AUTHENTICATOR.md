# Authenticator App (TOTP) — How It Works

This document explains the full end-to-end implementation of the TOTP (Time-based One-Time Password) authenticator app integration in Fedinet.

---

## Overview

TOTP serves two purposes in Fedinet:

1. **Login second factor** — after password login, a 6-digit code from an app like Google Authenticator or Authy is required
2. **Private key protection** — your Ed25519 signing key is encrypted at rest using the TOTP secret; no one (not even the server) can decrypt it without your authenticator code

---

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                     User Device                            │
│  Authenticator App (TOTP codes) ◄──── TOTP secret         │
│  Browser localStorage: encrypted_private_key              │
└───────────────────────────┬────────────────────────────────┘
                            │ HTTPS
┌───────────────────────────▼────────────────────────────────┐
│                  Identity Server (Go)                      │
│  - TOTP secret stored AES-GCM encrypted (SERVER_MASTER_KEY)│
│  - encrypted_client_key stored (server cannot decrypt)     │
│  - totp_partial_tokens table (5-min TTL for login flow)    │
│  - totp_backup_codes table (hashed, single-use)            │
└────────────────────────────────────────────────────────────┘
```

---

## Setup Flow

### Step 1 — Generate secret (`POST /totp/setup`)

- Server generates a random 160-bit TOTP secret using `pquerna/otp`
- Secret is base32-encoded (RFC 4648)
- Stored in the database encrypted with AES-GCM (`SERVER_MASTER_KEY`)
- Server returns:
  - `qr_png`: base64-encoded 512×512 PNG showing an `otpauth://` URI
  - `secret`: raw base32 string for manual entry

### Step 2 — Scan QR code

- User opens Google Authenticator / Authy / 1Password / Bitwarden etc.
- Scans the QR or types the manual entry key
- App starts producing 6-digit TOTP codes every 30 seconds

### Step 3 — Verify first code + enable (`POST /totp/enable`)

- User types the 6-digit code from their app — this confirms they scanned correctly
- Server validates the code with ±30s time window (allows ±1 period of clock drift)
- **Key derivation**: browser generates a fresh Ed25519 key pair, encrypts the private key with the TOTP secret using PBKDF2 + AES-GCM
- `client_private_key_enc` is sent to and stored by the server (server never sees the plaintext key)
- Encrypted private key is also stored in browser `localStorage`
- TOTP is now enabled — subsequent logins require the code

### Step 4 — Backup codes generated automatically

- Immediately after enabling, 8 single-use recovery codes are generated
- Format: `XXXX-XXXX` using an unambiguous charset (no 0/O/1/I confusion)
- Codes are hashed (SHA-256) before storage — the server never stores plaintext codes
- User is shown the codes once with a warning and download/copy options

---

## Login Flow

```
Password → partial_token (5 min) → TOTP code → full JWT tokens
```

### Step 1 — Password login (`POST /login`)

- If TOTP is enabled: returns `{ totp_required: true, partial_token: "..." }`
- `partial_token` is a short-lived signed JWT (5 minutes) stored in `totp_partial_tokens`

### Step 2a — TOTP code (`POST /login/totp`)

- Body: `{ partial_token, otp_code }`
- Server verifies the partial_token is valid and unexpired
- Validates the 6-digit code with ±30s window
- Returns: `{ access_token, refresh_token, user_id, home_server }`

### Step 2b — Backup code (`POST /login/totp/backup`)

- Body: `{ partial_token, backup_code }`
- Looks up SHA-256 hash of the code in `totp_backup_codes`
- Marks the code as used (single-use)
- Returns: `{ access_token, refresh_token, backup_codes_remaining }`
- Frontend warns if fewer than 3 codes remain

---

## Private Key Unlock

Once logged in, some actions (signing posts, ZKP proofs) need the Ed25519 private key:

1. Frontend calls `POST /totp/verify` with the current 6-digit code
2. Server returns the raw TOTP secret (decrypting from AES-GCM storage)
3. Browser uses the secret + PBKDF2 to decrypt the stored `encrypted_private_key`
4. Decrypted key lives only in memory for the duration of the operation — never written back

---

## Disabling TOTP

- `POST /totp/disable` requires a valid 6-digit code
- Clears the TOTP secret, encrypted key, and all backup codes from the database
- Browser clears the local encrypted keypair from `localStorage`

---

## Rate Limiting

All TOTP endpoints are protected by an in-memory sliding-window rate limiter:

| Endpoint | Limit |
|---|---|
| `/totp/setup`, `/totp/enable`, `/totp/verify`, `/totp/disable`, `/totp/backup-codes/generate` | 10 req / minute per IP |
| `/totp/status`, `/totp/backup-codes/count` | 120 req / minute per IP |
| `/login/totp`, `/login/totp/backup` | 10 req / minute per IP |

---

## Database Schema

```sql
-- Main TOTP state on the user record
ALTER TABLE identities ADD COLUMN totp_enabled        BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE identities ADD COLUMN totp_secret_enc     TEXT;          -- AES-GCM(base32 secret)
ALTER TABLE identities ADD COLUMN client_private_key_enc TEXT;       -- AES-GCM(Ed25519 private key)

-- Short-lived tokens bridging password → TOTP step
CREATE TABLE totp_partial_tokens (
    token      TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used       BOOLEAN NOT NULL DEFAULT FALSE
);

-- Single-use backup recovery codes
CREATE TABLE totp_backup_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,   -- SHA-256 hex of XXXX-XXXX plaintext
    used       BOOLEAN NOT NULL DEFAULT FALSE,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Cryptographic Details

| Item | Algorithm |
|---|---|
| TOTP algorithm | RFC 6238 (HMAC-SHA1, 6 digits, 30s period) |
| Secret size | 160 bits (20 bytes) |
| Time tolerance | ±1 period = ±30 seconds |
| Server-side encryption | AES-256-GCM (key = `SERVER_MASTER_KEY` env var) |
| Client key derivation | PBKDF2-SHA256, 100,000 iterations, 32-byte output |
| Client key encryption | AES-256-GCM |
| Backup code entropy | `ABCDEFGHJKLMNPQRSTUVWXYZ23456789` charset, 8 chars (≈41 bits) |
| Backup code storage | SHA-256 hex hash only |

---

## Supported Authenticator Apps

Any RFC 6238-compliant app works:

| App | Platform | Notes |
|---|---|---|
| Google Authenticator | iOS, Android | Most common |
| Authy | iOS, Android, Desktop | Supports cloud backup |
| Microsoft Authenticator | iOS, Android | Enterprise-friendly |
| 1Password | All | Integrated with password manager |
| Bitwarden | All | Open source |
| Apple Passwords (iOS 18+) | iOS / macOS | Built into the OS |

---

## Common Issues & Fixes

### "Invalid authenticator code" on first entry

- Make sure you scanned the **same** QR code that the setup screen showed
- Wait for the code to refresh (codes expire every 30 seconds — the timer in the app shows how long is left)
- If you waited too long during setup, click "Set up authenticator app" again to get a fresh QR code

### "Invalid authenticator code" on login after code looks right

- **Clock drift**: TOTP depends on your device clock being accurate. Check that your phone time is set to "automatic / network time"
- The server allows ±30 seconds of drift (one full period). If your device clock is more than 60 seconds off, codes will always fail

### OTP boxes still filled after entering wrong code

- Fixed in current version — after a failed submission the boxes auto-clear so you can type the next code immediately

### Authenticator app codes copied with spaces ("123 456")

- Fixed in current version — the OTP paste handler strips spaces automatically

---

## Known Bugs Fixed

| Date | Bug | Fix |
|---|---|---|
| 2026-03-10 | go-webauthn v0.16: "Backup Eligible flag inconsistency" on passkey login | Migration 014: added `backup_eligible` + `backup_state` columns to `passkeys` table; now stored and restored on every register/login |
| 2026-03-10 | TOTP endpoints had no rate limiting | All `/totp/*` and `/login/totp*` routes now use `rlAuth` / `rlRead` middleware |
| 2026-03-10 | OTPInput didn't reset after wrong code | Added `resetKey` prop; parent components increment on error to force-clear boxes |
