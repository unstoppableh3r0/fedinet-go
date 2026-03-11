package identity

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net/smtp"
	"os"
)

// ============================================================================
// AUTHENTICATION SECURITY: OTP & NOTIFICATION DELIVERY
// ============================================================================

// SendOTP handles the delivery of a one-time password via the SMTP protocol.
// This function supports standard LOGIN/PLAIN authentication and provides
// an HTML-formatted message for improved user experience.
func SendOTP(toEmail, code string) error {
	// 1. CONFIGURATION RETRIEVAL
	// Values are pulled from environment variables to ensure secrets (passwords)
	// are never hardcoded in the binary.
	from := os.Getenv("SMTP_FROM")
	password := os.Getenv("SMTP_PASSWORD")
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	username := os.Getenv("SMTP_USER")

	// 2. CONFIGURATION VALIDATION & DEV-FALLBACK
	// If SMTP is not configured, we do not halt the application. Instead,
	// we log the OTP to stdout. This is critical for local development
	// environments that don't have access to a mail relay.
	if host == "" || port == "" {
		log.Printf("SMTP not configured. OTP for %s is: %s", toEmail, code)
		return nil
	}

	// 3. AUTHENTICATION SETUP
	// Standard Plain Authentication. In production, 'host' should use TLS/SSL
	// (Port 465 or 587 with STARTTLS) to prevent credential sniffing.
	auth := smtp.PlainAuth("", username, password, host)

	// 4. RFC 5322 MESSAGE CONSTRUCTION
	// We manually construct the email headers. Content-Type is set to text/html
	// to allow for the styled branding of the verification code.
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = toEmail
	headers["Subject"] = "Your Verification Code - Fedinet"
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=\"utf-8\""

	// Assemble headers into a valid SMTP message string.
	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}

	// 5. HTML TEMPLATE INJECTION
	// The message includes inline CSS for cross-client compatibility.
	// The F5C518 color code maintains the 'Gotham Social' branding.
	message += "\r\n" + fmt.Sprintf(`
        <div style="font-family: Arial, sans-serif; padding: 20px;">
            <h2>Account Verification</h2>
            <p>Your verification code is:</p>
            <h1 style="color: #F5C518; letter-spacing: 5px;">%s</h1>
            <p>This code expires in 10 minutes.</p>
        </div>
    `, code)

	// 6. NETWORK DELIVERY
	// Dial the remote SMTP server and transmit the data.
	addr := fmt.Sprintf("%s:%s", host, port)
	err := smtp.SendMail(addr, auth, from, []string{toEmail}, []byte(message))
	if err != nil {
		// We log the error but return it to the caller so the API can
		// provide appropriate feedback (e.g., HTTP 500) to the user.
		log.Printf("Failed to send email: %v", err)
		return err
	}

	return nil
}

// GenerateOTP creates a cryptographically secure random numeric string.
//
// SECURITY NOTE:
// Unlike math/rand, which is deterministic and predictable, this uses
// crypto/rand (rand.Reader). This ensures that the OTP cannot be guessed
// by an attacker observing previous codes.
func GenerateOTP(length int) (string, error) {
	// We strictly use digits to ensure the code is easy to type on mobile devices.
	const digits = "0123456789"
	result := make([]byte, length)

	// 1. ENTROPY SOURCE
	// rand.Reader on Linux/Unix systems typically reads from /dev/urandom.
	for i := range result {
		// 2. UNBIASED SELECTION
		// rand.Int generates a random big.Int in the range [0, n).
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}

		// Map the random integer back to our character set.
		result[i] = digits[num.Int64()]
	}

	// Returns the finished code (e.g., "582910" for a length of 6).
	return string(result), nil
}
