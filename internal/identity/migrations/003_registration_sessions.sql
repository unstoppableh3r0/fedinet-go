-- Registration Sessions table for OTP verification
CREATE TABLE IF NOT EXISTS registration_sessions (
    session_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    otp_code TEXT NOT NULL,
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    invite_code TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_registration_sessions_email ON registration_sessions(email);
