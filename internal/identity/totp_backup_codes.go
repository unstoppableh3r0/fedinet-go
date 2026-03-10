package identity

// ---------------------------------------------------------------------------
// TOTP Backup Recovery Codes
// ---------------------------------------------------------------------------
// Backup codes allow account recovery when the authenticator device is lost.
//
// Endpoints:
//   POST /totp/backup-codes/generate  — (auth) generates 8 fresh codes
//   POST /login/totp/backup           — (public) logs in using a backup code
//
// Security model:
//   • Codes are stored SHA-256 hashed — never in plaintext.
//   • Each code is single-use; reuse is rejected.
//   • Generating new codes invalidates (deletes) all existing ones.
//   • The /login/totp/backup endpoint is rate-limited in main.go.
// ---------------------------------------------------------------------------

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	backupCodeCount = 8
	// Base32-like charset: no 0/O/1/I to avoid visual confusion
	backupCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// generateBackupCode returns a cryptographically random code in XXXX-XXXX format.
func generateBackupCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	chars := make([]byte, 9) // 4 + dash + 4
	for i := 0; i < 4; i++ {
		chars[i] = backupCodeCharset[int(b[i])%len(backupCodeCharset)]
	}
	chars[4] = '-'
	for i := 0; i < 4; i++ {
		chars[5+i] = backupCodeCharset[int(b[4+i])%len(backupCodeCharset)]
	}
	return string(chars), nil
}

// hashBackupCode returns the hex-encoded SHA-256 digest of a backup code.
func hashBackupCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(code)))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// POST /totp/backup-codes/generate — authenticated
// ---------------------------------------------------------------------------
// Deletes all existing backup codes for the user, generates 8 fresh ones,
// stores their SHA-256 hashes, and returns the plaintext codes once.
// TOTP must be enabled before calling this endpoint.
// ---------------------------------------------------------------------------
func TOTPGenerateBackupCodesHandler(w http.ResponseWriter, r *http.Request) {
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

	// TOTP must already be enabled
	var enabled bool
	if err := db.QueryRow(
		`SELECT COALESCE(totp_enabled, FALSE) FROM identities WHERE user_id = $1`, userID,
	).Scan(&enabled); err != nil || !enabled {
		RespondWithError(w, http.StatusBadRequest, "TOTP is not enabled")
		return
	}

	// Rotate: delete all existing backup codes for this user
	if _, err := db.Exec(`DELETE FROM totp_backup_codes WHERE user_id = $1`, userID); err != nil {
		log.Printf("backup codes delete error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Generate fresh codes
	plainCodes := make([]string, 0, backupCodeCount)
	for i := 0; i < backupCodeCount; i++ {
		code, err := generateBackupCode()
		if err != nil {
			log.Printf("backup code generation error: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to generate codes")
			return
		}
		plainCodes = append(plainCodes, code)

		if _, err := db.Exec(
			`INSERT INTO totp_backup_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, hashBackupCode(code),
		); err != nil {
			log.Printf("backup code insert error: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "database error")
			return
		}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"backup_codes": plainCodes,
	})
}

// ---------------------------------------------------------------------------
// GET /totp/backup-codes/count — authenticated
// ---------------------------------------------------------------------------
// Returns the number of unused backup codes remaining. Used by the frontend
// to show a warning when codes are running low.
// ---------------------------------------------------------------------------
func TOTPBackupCodesCountHandler(w http.ResponseWriter, r *http.Request) {
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

	var count int
	db.QueryRow(
		`SELECT COUNT(*) FROM totp_backup_codes WHERE user_id = $1 AND used = FALSE`, userID,
	).Scan(&count)

	RespondWithJSON(w, http.StatusOK, map[string]int{"remaining": count})
}

// ---------------------------------------------------------------------------
// POST /login/totp/backup — public (rate-limited in main.go)
// ---------------------------------------------------------------------------
// Second-factor login using a backup code when the authenticator is unavailable.
// Body: { "partial_token": "...", "backup_code": "XXXX-XXXX" }
// ---------------------------------------------------------------------------
func LoginTOTPBackupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		PartialToken string `json:"partial_token"`
		BackupCode   string `json:"backup_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PartialToken == "" || req.BackupCode == "" {
		RespondWithError(w, http.StatusBadRequest, "partial_token and backup_code required")
		return
	}

	// Validate partial token
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

	// Find and consume matching backup code
	codeHash := hashBackupCode(strings.TrimSpace(req.BackupCode))

	var codeID string
	err = db.QueryRow(`
		SELECT id FROM totp_backup_codes
		WHERE  user_id = $1 AND code_hash = $2 AND used = FALSE
	`, userID, codeHash).Scan(&codeID)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusUnauthorized, "invalid or already-used backup code")
		return
	}
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Mark backup code as used
	if _, err := db.Exec(
		`UPDATE totp_backup_codes SET used = TRUE, used_at = NOW() WHERE id = $1`, codeID,
	); err != nil {
		log.Printf("backup code used-update error: %v", err)
	}

	// Mark partial token as used
	if _, err := db.Exec(
		`UPDATE totp_partial_tokens SET used = TRUE WHERE token = $1`, req.PartialToken,
	); err != nil {
		log.Printf("partial token used-update error: %v", err)
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

	// Count remaining unused backup codes — client can warn user if low
	var remaining int
	db.QueryRow(
		`SELECT COUNT(*) FROM totp_backup_codes WHERE user_id = $1 AND used = FALSE`, userID,
	).Scan(&remaining)

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":                  ToExternalID(userID),
		"home_server":              externalURL,
		"access_token":             accessToken,
		"refresh_token":            refreshToken,
		"backup_codes_remaining":   remaining,
	})
}

// formatBackupCodesForDownload produces a plain-text file content for the
// "Download backup codes" feature.
func formatBackupCodesForDownload(codes []string, username, serverName string) string {
	now := time.Now().Format("2006-01-02 15:04 UTC")
	var sb strings.Builder
	fmt.Fprintf(&sb, "Backup Codes — %s\n", serverName)
	fmt.Fprintf(&sb, "Account: %s\n", username)
	fmt.Fprintf(&sb, "Generated: %s\n\n", now)
	sb.WriteString("Each code can only be used once.\n")
	sb.WriteString("Keep this file safe and offline.\n\n")
	for i, c := range codes {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, c)
	}
	sb.WriteString("\nOnce you use a code, regenerate new ones from Security Settings.\n")
	return sb.String()
}
