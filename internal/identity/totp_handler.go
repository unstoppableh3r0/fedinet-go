package identity

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"
	fedcrypto "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ---------------------------------------------------------------------------
// TOTP Setup  –  POST /totp/setup
// ---------------------------------------------------------------------------
// Generates a fresh TOTP secret, returns the otpauth:// URI and a PNG QR code
// (base64-encoded).  The secret is stored encrypted in the DB but TOTP is NOT
// yet marked as enabled until the client confirms the first code.
//
// Requires: Bearer token (authenticated user).
// ---------------------------------------------------------------------------
func TOTPSetupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, err := authFromRequest(r)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := ToInternalID(claims.UserID)

	// Generate a fresh TOTP secret
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuerName(),
		AccountName: ToExternalID(userID),
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		log.Printf("TOTP key generation failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate TOTP secret")
		return
	}

	rawSecret := key.Secret() // base32-encoded

	// Encrypt the secret before storing
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		RespondWithError(w, http.StatusInternalServerError, "server misconfiguration")
		return
	}
	encryptedSecret, err := fedcrypto.Encrypt(rawSecret, masterKey)
	if err != nil {
		log.Printf("TOTP secret encryption failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to store TOTP secret")
		return
	}

	// Persist (not yet enabled – just stored for confirmation step)
	_, err = db.Exec(`
		UPDATE identities
		SET    totp_secret_encrypted = $1,
		       totp_enabled          = FALSE
		WHERE  user_id = $2
	`, encryptedSecret, userID)
	if err != nil {
		log.Printf("TOTP DB write failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Generate QR code PNG (512px, High error-correction for reliable scanning)
	qrPNG, err := qrcode.Encode(key.URL(), qrcode.High, 512)
	if err != nil {
		log.Printf("QR code generation failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate QR code")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"secret":  rawSecret,
		"otpauth": key.URL(),
		"qr_png":  fmt.Sprintf("data:image/png;base64,%s", base64Encode(qrPNG)),
	})
}

// ---------------------------------------------------------------------------
// TOTP Enable  –  POST /totp/enable
// ---------------------------------------------------------------------------
// Verifies the user's first OTP code and marks TOTP as enabled.  Optionally
// accepts an encrypted client private key to store server-side as backup.
//
// Body: { "otp_code": "123456", "client_private_key_enc": "..." (optional) }
// ---------------------------------------------------------------------------
func TOTPEnableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, err := authFromRequest(r)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := ToInternalID(claims.UserID)

	var req struct {
		OTPCode             string `json:"otp_code"`
		ClientPrivateKeyEnc string `json:"client_private_key_enc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OTPCode == "" {
		RespondWithError(w, http.StatusBadRequest, "otp_code required")
		return
	}

	secret, err := loadTOTPSecret(userID)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "TOTP not configured – call /totp/setup first")
		return
	}

	if !totp.Validate(req.OTPCode, secret) {
		RespondWithError(w, http.StatusUnauthorized, "invalid OTP code")
		return
	}

	// Mark as enabled and optionally store the encrypted client private key
	_, err = db.Exec(`
		UPDATE identities
		SET    totp_enabled           = TRUE,
		       client_private_key_enc = COALESCE(NULLIF($1,''), client_private_key_enc)
		WHERE  user_id = $2
	`, req.ClientPrivateKeyEnc, userID)
	if err != nil {
		log.Printf("TOTP enable DB write failed: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "totp_enabled"})
}

// ---------------------------------------------------------------------------
// TOTP Status  –  GET /totp/status
// ---------------------------------------------------------------------------
// Returns whether TOTP is enabled for the authenticated user.
// ---------------------------------------------------------------------------
func TOTPStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, err := authFromRequest(r)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := ToInternalID(claims.UserID)

	var enabled bool
	err = db.QueryRow(`SELECT COALESCE(totp_enabled, FALSE) FROM identities WHERE user_id = $1`, userID).Scan(&enabled)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]bool{"totp_enabled": enabled})
}

// ---------------------------------------------------------------------------
// TOTP Verify  –  POST /totp/verify
// ---------------------------------------------------------------------------
// Verifies an OTP code for an already-authenticated user (e.g. to unlock the
// client private key).  On success returns the raw TOTP secret so the client
// can decrypt its locally-stored private key.
//
// Body: { "otp_code": "123456" }
// ---------------------------------------------------------------------------
func TOTPVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, err := authFromRequest(r)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := ToInternalID(claims.UserID)

	var req struct {
		OTPCode string `json:"otp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OTPCode == "" {
		RespondWithError(w, http.StatusBadRequest, "otp_code required")
		return
	}

	secret, err := loadTOTPSecret(userID)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "TOTP not configured")
		return
	}

	if !totp.Validate(req.OTPCode, secret) {
		RespondWithError(w, http.StatusUnauthorized, "invalid OTP code")
		return
	}

	// Return the raw secret so the client can derive its AES key and decrypt
	// the locally-stored private key.  This is safe because:
	//   - The channel is HTTPS
	//   - The user just proved possession of the authenticator device
	//   - The secret alone doesn't give access without the encrypted blob in localStorage
	RespondWithJSON(w, http.StatusOK, map[string]string{"totp_secret": secret})
}

// ---------------------------------------------------------------------------
// TOTP Disable  –  POST /totp/disable
// ---------------------------------------------------------------------------
// Requires a valid current OTP code.  Clears the secret and marks TOTP off.
// ---------------------------------------------------------------------------
func TOTPDisableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, err := authFromRequest(r)
	if err != nil {
		RespondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := ToInternalID(claims.UserID)

	var req struct {
		OTPCode string `json:"otp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OTPCode == "" {
		RespondWithError(w, http.StatusBadRequest, "otp_code required")
		return
	}

	secret, err := loadTOTPSecret(userID)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "TOTP not configured")
		return
	}

	if !totp.Validate(req.OTPCode, secret) {
		RespondWithError(w, http.StatusUnauthorized, "invalid OTP code")
		return
	}

	_, err = db.Exec(`
		UPDATE identities
		SET    totp_secret_encrypted = NULL,
		       totp_enabled          = FALSE,
		       client_private_key_enc = NULL
		WHERE  user_id = $1
	`, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "totp_disabled"})
}

// ---------------------------------------------------------------------------
// Login/TOTP  –  POST /login/totp
// ---------------------------------------------------------------------------
// Second step of TOTP-enabled login.  Accepts partial_token + otp_code,
// returns full access/refresh tokens and the raw TOTP secret.
//
// Body: { "partial_token": "...", "otp_code": "123456" }
// ---------------------------------------------------------------------------
func LoginTOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		PartialToken string `json:"partial_token"`
		OTPCode      string `json:"otp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PartialToken == "" || req.OTPCode == "" {
		RespondWithError(w, http.StatusBadRequest, "partial_token and otp_code required")
		return
	}

	// Look up and validate partial token
	var userID string
	var expiresAt time.Time
	var used bool
	err := db.QueryRow(`
		SELECT user_id, expires_at, used
		FROM   totp_partial_tokens
		WHERE  token = $1
	`, req.PartialToken).Scan(&userID, &expiresAt, &used)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusUnauthorized, "invalid partial token")
		return
	}
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}
	if used || time.Now().After(expiresAt) {
		RespondWithError(w, http.StatusUnauthorized, "partial token expired or already used")
		return
	}

	// Verify OTP
	secret, err := loadTOTPSecret(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "TOTP configuration error")
		return
	}
	if !totp.Validate(req.OTPCode, secret) {
		RespondWithError(w, http.StatusUnauthorized, "invalid OTP code")
		return
	}

	// Mark token as used (single-use)
	_, err = db.Exec(`UPDATE totp_partial_tokens SET used = TRUE WHERE token = $1`, req.PartialToken)
	if err != nil {
		log.Printf("Failed to mark partial token used: %v", err)
	}

	// Issue full tokens
	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}
	accessToken, refreshToken, err := GenerateTokenPair(userID, externalURL)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"user_id":       ToExternalID(userID),
		"home_server":   externalURL,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"totp_secret":   secret, // client uses this to decrypt local private key
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func issuerName() string {
	name := os.Getenv("SERVER_NAME")
	if name == "" {
		return "Fedinet"
	}
	return name
}

func loadTOTPSecret(userID string) (string, error) {
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		return "", fmt.Errorf("SERVER_MASTER_KEY not set")
	}

	var encryptedSecret sql.NullString
	err := db.QueryRow(`
		SELECT totp_secret_encrypted FROM identities WHERE user_id = $1
	`, userID).Scan(&encryptedSecret)
	if err != nil || !encryptedSecret.Valid || encryptedSecret.String == "" {
		return "", fmt.Errorf("no TOTP secret for user %s", userID)
	}

	return fedcrypto.Decrypt(encryptedSecret.String, masterKey)
}

// createPartialToken inserts a single-use totp_partial_tokens row and returns
// the token string.
func createPartialToken(userID string) (string, error) {
	token := strings.ReplaceAll(uuid.New().String(), "-", "") +
		strings.ReplaceAll(uuid.New().String(), "-", "")

	_, err := db.Exec(`
		INSERT INTO totp_partial_tokens (token, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, token, userID, time.Now().Add(5*time.Minute))
	if err != nil {
		return "", err
	}
	return token, nil
}

// base64Encode returns the standard base64 encoding of b.
func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// authFromRequest extracts and validates the Bearer JWT from the request,
// returning the parsed claims.  Mirrors the pattern used in account_link_handlers.go.
func authFromRequest(r *http.Request) (*UserClaims, error) {
	authHeader := r.Header.Get("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, fmt.Errorf("missing or invalid authorization header")
	}
	return ValidateUserToken(parts[1])
}
