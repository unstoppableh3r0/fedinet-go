package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net/smtp"
	"os"
	"time"
)

// OTP Configuration
const (
	OTPLength     = 6
	OTPExpiry     = 5 * time.Minute
	MaxOTPAttempts = 3
)

// OTP represents an OTP code record
type OTP struct {
	ID          string
	Email       string
	OTPCode     string
	Purpose     string
	SessionID   string
	Verified    bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Attempts    int
	MaxAttempts int
}

// GenerateOTP generates a random 6-digit OTP code
func GenerateOTP() (string, error) {
	max := big.NewInt(1000000) // 0-999999
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	// Format as 6-digit string with leading zeros
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// StoreOTP saves an OTP code to the database
func StoreOTP(email, otpCode, purpose string) (string, error) {
	// Delete any existing unverified OTPs for this email and purpose
	_, err := db.Exec(`
		DELETE FROM otp_codes 
		WHERE email = $1 AND purpose = $2 AND verified = false
	`, email, purpose)
	if err != nil {
		log.Printf("Warning: failed to delete old OTPs: %v", err)
	}

	// Generate session ID
	var sessionID string
	expiresAt := time.Now().Add(OTPExpiry)

	err = db.QueryRow(`
		INSERT INTO otp_codes (email, otp_code, purpose, expires_at, max_attempts)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING session_id
	`, email, otpCode, purpose, expiresAt, MaxOTPAttempts).Scan(&sessionID)

	if err != nil {
		return "", fmt.Errorf("failed to store OTP: %w", err)
	}

	log.Printf("OTP stored for %s (purpose: %s, session: %s)", email, purpose, sessionID)
	return sessionID, nil
}

// VerifyOTP validates an OTP code and marks it as verified
func VerifyOTP(email, otpCode, sessionID string) (*OTP, error) {
	var otp OTP

	// Fetch OTP record
	err := db.QueryRow(`
		SELECT id, email, otp_code, purpose, session_id, verified, 
		       created_at, expires_at, attempts, max_attempts
		FROM otp_codes
		WHERE email = $1 AND session_id = $2 AND verified = false
	`, email, sessionID).Scan(
		&otp.ID, &otp.Email, &otp.OTPCode, &otp.Purpose, &otp.SessionID,
		&otp.Verified, &otp.CreatedAt, &otp.ExpiresAt, &otp.Attempts, &otp.MaxAttempts,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid session or OTP already verified")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Check if OTP is expired
	if time.Now().After(otp.ExpiresAt) {
		return nil, fmt.Errorf("OTP has expired")
	}

	// Check if max attempts exceeded
	if otp.Attempts >= otp.MaxAttempts {
		return nil, fmt.Errorf("maximum verification attempts exceeded")
	}

	// Verify OTP code
	if otp.OTPCode != otpCode {
		// Increment attempt counter
		db.Exec(`
			UPDATE otp_codes 
			SET attempts = attempts + 1 
			WHERE id = $1
		`, otp.ID)
		return nil, fmt.Errorf("invalid OTP code")
	}

	// Mark OTP as verified
	_, err = db.Exec(`
		UPDATE otp_codes 
		SET verified = true 
		WHERE id = $1
	`, otp.ID)

	if err != nil {
		return nil, fmt.Errorf("failed to mark OTP as verified: %w", err)
	}

	otp.Verified = true
	log.Printf("OTP verified successfully for %s (purpose: %s)", email, otp.Purpose)
	return &otp, nil
}

// SendOTPEmail sends an OTP code via email
func SendOTPEmail(email, otpCode, purpose string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUsername := os.Getenv("SMTP_USERNAME")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpFrom := os.Getenv("SMTP_FROM_EMAIL")

	// Validate SMTP configuration
	if smtpHost == "" || smtpPort == "" || smtpUsername == "" || smtpPassword == "" {
		return fmt.Errorf("SMTP configuration incomplete - check environment variables")
	}

	if smtpFrom == "" {
		smtpFrom = smtpUsername
	}

	// Determine email subject and body based on purpose
	var subject, body string
	switch purpose {
	case "login":
		subject = "Your Login Verification Code"
		body = fmt.Sprintf(`
Hello,

Your login verification code is: %s

This code will expire in 5 minutes.

If you didn't request this code, please ignore this email.

Best regards,
Federated Network Team
`, otpCode)
	case "registration":
		subject = "Complete Your Registration"
		body = fmt.Sprintf(`
Welcome to Federated Network!

Your registration verification code is: %s

This code will expire in 5 minutes.

Best regards,
Federated Network Team
`, otpCode)
	case "password_reset":
		subject = "Reset Your Password"
		body = fmt.Sprintf(`
Hello,

Your password reset verification code is: %s

This code will expire in 5 minutes.

If you didn't request this code, please ignore this email.

Best regards,
Federated Network Team
`, otpCode)
	default:
		return fmt.Errorf("invalid OTP purpose: %s", purpose)
	}

	// Compose email
	message := fmt.Sprintf("From: %s\r\n", smtpFrom) +
		fmt.Sprintf("To: %s\r\n", email) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"\r\n" +
		body

	// Send email
	auth := smtp.PlainAuth("", smtpUsername, smtpPassword, smtpHost)
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)

	err := smtp.SendMail(addr, auth, smtpFrom, []string{email}, []byte(message))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("OTP email sent to %s (purpose: %s)", email, purpose)
	return nil
}

// CleanupExpiredOTPs removes expired OTP codes from the database
func CleanupExpiredOTPs() {
	for {
		time.Sleep(10 * time.Minute)

		result, err := db.Exec(`
			DELETE FROM otp_codes 
			WHERE expires_at < NOW()
		`)

		if err != nil {
			log.Printf("Error cleaning up expired OTPs: %v", err)
			continue
		}

		rows, _ := result.RowsAffected()
		if rows > 0 {
			log.Printf("Cleaned up %d expired OTP codes", rows)
		}
	}
}

// CheckOTPRateLimit checks if an email has exceeded the OTP request rate limit
func CheckOTPRateLimit(email string) error {
	var count int
	tenMinutesAgo := time.Now().Add(-10 * time.Minute)

	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM otp_codes 
		WHERE email = $1 AND created_at > $2
	`, email, tenMinutesAgo).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}

	if count >= 3 {
		return fmt.Errorf("rate limit exceeded - maximum 3 OTP requests per 10 minutes")
	}

	return nil
}
