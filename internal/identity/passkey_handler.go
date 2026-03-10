package identity

// ---------------------------------------------------------------------------
// Passkey (WebAuthn) handlers
// ---------------------------------------------------------------------------
// Design:
//   • /passkey/register/begin    POST (auth)   — enroll a new passkey
//   • /passkey/register/complete POST (auth)   — confirm enrollment
//   • /passkey/login/begin       POST (public) — start passkey login
//   • /passkey/login/complete    POST (public) — finish passkey login → JWT
//   • /passkey/recover/begin     POST (public) — TOTP+recovery key → re-enroll challenge
//   • /passkey/recover/complete  POST (public) — finish re-enrollment → new JWT + recovery key
//   • /passkey/status            GET  (auth)   — list enrolled passkeys
//   • /passkey/remove            POST (auth)   — delete a passkey
//
// Begin/Complete pairs pass state via a short-lived in-memory session keyed by
// a random `session_id` returned to the client and sent back as ?session_id=.
// ---------------------------------------------------------------------------

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	fedcrypto "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ---------------------------------------------------------------------------
// WebAuthn singleton — lazy-initialized from env vars.
// ---------------------------------------------------------------------------

var (
	waOnce sync.Once
	wa     *webauthn.WebAuthn
	waErr  error
)

func getWA() (*webauthn.WebAuthn, error) {
	waOnce.Do(func() {
		rpID := os.Getenv("WEBAUTHN_RPID")
		if rpID == "" {
			rpID = "localhost"
		}
		originsEnv := os.Getenv("WEBAUTHN_ORIGIN")
		if originsEnv == "" {
			originsEnv = "http://localhost:3000"
		}
		origins := strings.Split(originsEnv, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		wa, waErr = webauthn.New(&webauthn.Config{
			RPDisplayName: issuerName(),
			RPID:          rpID,
			RPOrigins:     origins,
		})
	})
	return wa, waErr
}

// ---------------------------------------------------------------------------
// In-memory WebAuthn session store (TTL = 5 min).
// ---------------------------------------------------------------------------

type waSession struct {
	data           *webauthn.SessionData
	forUserID      string
	isRecoveryFlow bool
	expiresAt      time.Time
}

var waSessions sync.Map

func init() {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			now := time.Now()
			waSessions.Range(func(k, v any) bool {
				if s, ok := v.(waSession); ok && now.After(s.expiresAt) {
					waSessions.Delete(k)
				}
				return true
			})
		}
	}()
}

func putWASession(id string, s waSession) {
	s.expiresAt = time.Now().Add(5 * time.Minute)
	waSessions.Store(id, s)
}

func popWASession(id string) (waSession, bool) {
	v, ok := waSessions.LoadAndDelete(id)
	if !ok {
		return waSession{}, false
	}
	s := v.(waSession)
	if time.Now().After(s.expiresAt) {
		return waSession{}, false
	}
	return s, true
}

func genSessionID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// ---------------------------------------------------------------------------
// waUser — implements webauthn.User.
// ---------------------------------------------------------------------------

type waUser struct {
	id          string
	displayName string
	creds       []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return []byte(u.id) }
func (u *waUser) WebAuthnName() string                       { return u.id }
func (u *waUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func loadWAUser(userID string) (*waUser, error) {
	var dn string
	_ = db.QueryRow(
		`SELECT COALESCE(display_name, user_id) FROM identities WHERE user_id = $1`,
		userID,
	).Scan(&dn)
	if dn == "" {
		dn = userID
	}

	rows, err := db.Query(
		`SELECT credential_id, public_key, sign_count FROM passkeys WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var cid, pk []byte
		var sc uint32
		if err2 := rows.Scan(&cid, &pk, &sc); err2 == nil {
			creds = append(creds, webauthn.Credential{
				ID:        cid,
				PublicKey: pk,
				Authenticator: webauthn.Authenticator{
					SignCount: sc,
				},
			})
		}
	}
	return &waUser{id: userID, displayName: dn, creds: creds}, nil
}

// ---------------------------------------------------------------------------
// POST /passkey/register/begin — authenticated: enroll a passkey
// ---------------------------------------------------------------------------

func PasskeyRegisterBeginHandler(w http.ResponseWriter, r *http.Request) {
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

	instance, err := getWA()
	if err != nil {
		log.Printf("WebAuthn init error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "webauthn unavailable")
		return
	}
	user, err := loadWAUser(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}

	creation, sessionData, err := instance.BeginRegistration(user)
	if err != nil {
		log.Printf("BeginRegistration error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to begin registration")
		return
	}

	sid := genSessionID()
	putWASession(sid, waSession{data: sessionData, forUserID: userID})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sid,
		"options":    creation,
	})
}

// ---------------------------------------------------------------------------
// POST /passkey/register/complete?session_id=xxx
// Body: standard PublicKeyCredential JSON from navigator.credentials.create()
// ---------------------------------------------------------------------------

func PasskeyRegisterCompleteHandler(w http.ResponseWriter, r *http.Request) {
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

	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		RespondWithError(w, http.StatusBadRequest, "session_id query param required")
		return
	}
	sess, ok := popWASession(sid)
	if !ok {
		RespondWithError(w, http.StatusBadRequest, "session expired or not found")
		return
	}
	if sess.forUserID != userID {
		RespondWithError(w, http.StatusForbidden, "session user mismatch")
		return
	}

	instance, _ := getWA()
	user, err := loadWAUser(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}

	cred, err := instance.FinishRegistration(user, *sess.data, r)
	if err != nil {
		log.Printf("FinishRegistration error: %v", err)
		RespondWithError(w, http.StatusBadRequest, "passkey registration failed: "+err.Error())
		return
	}

	_, err = db.Exec(`
		INSERT INTO passkeys (user_id, credential_id, public_key, sign_count, aaguid)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (credential_id) DO UPDATE SET sign_count = EXCLUDED.sign_count
	`, userID, cred.ID, cred.PublicKey, cred.Authenticator.SignCount, hex.EncodeToString(cred.Authenticator.AAGUID[:]))
	if err != nil {
		log.Printf("Passkey store error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to save passkey")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "passkey_registered"})
}

// ---------------------------------------------------------------------------
// POST /passkey/login/begin — public
// Body: { "user_id": "alice@server_a" }
// ---------------------------------------------------------------------------

func PasskeyLoginBeginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}
	userID := ToInternalID(req.UserID)

	instance, err := getWA()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "webauthn unavailable")
		return
	}
	user, err := loadWAUser(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	if len(user.creds) == 0 {
		RespondWithError(w, http.StatusNotFound, "no passkeys registered for this user")
		return
	}

	assertion, sessionData, err := instance.BeginLogin(user)
	if err != nil {
		log.Printf("BeginLogin error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to begin login")
		return
	}

	sid := genSessionID()
	putWASession(sid, waSession{data: sessionData, forUserID: userID})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sid,
		"options":    assertion,
	})
}

// ---------------------------------------------------------------------------
// POST /passkey/login/complete?session_id=xxx
// Body: standard PublicKeyCredential JSON from navigator.credentials.get()
// ---------------------------------------------------------------------------

func PasskeyLoginCompleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		RespondWithError(w, http.StatusBadRequest, "session_id query param required")
		return
	}
	sess, ok := popWASession(sid)
	if !ok {
		RespondWithError(w, http.StatusBadRequest, "session expired or not found")
		return
	}

	instance, _ := getWA()
	user, err := loadWAUser(sess.forUserID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}

	cred, err := instance.FinishLogin(user, *sess.data, r)
	if err != nil {
		log.Printf("FinishLogin error: %v", err)
		RespondWithError(w, http.StatusUnauthorized, "passkey verification failed")
		return
	}

	// Update sign count to detect cloned authenticators
	if _, dbErr := db.Exec(
		`UPDATE passkeys SET sign_count = $1 WHERE credential_id = $2`,
		cred.Authenticator.SignCount, cred.ID,
	); dbErr != nil {
		log.Printf("sign_count update error: %v", dbErr)
	}

	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}
	accessToken, refreshToken, err := GenerateTokenPair(sess.forUserID, externalURL)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"user_id":       ToExternalID(sess.forUserID),
		"home_server":   externalURL,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}

// ---------------------------------------------------------------------------
// POST /passkey/recover/begin — public
// Two-factor recovery: TOTP code + recovery key → passkey re-enroll challenge.
// Rate-limited to 5 attempts per hour per user via DB audit table.
// Body: { "user_id": "alice@server_a", "totp_code": "123456", "recovery_key": "R7G3-..." }
// ---------------------------------------------------------------------------

func PasskeyRecoverBeginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		UserID      string `json:"user_id"`
		TOTPCode    string `json:"totp_code"`
		RecoveryKey string `json:"recovery_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" || req.TOTPCode == "" || req.RecoveryKey == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id, totp_code, and recovery_key are required")
		return
	}
	userID := ToInternalID(req.UserID)

	// Rate limit: max 5 recovery attempts per hour per user
	var attempts int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM passkey_recovery_attempts
		WHERE user_id = $1 AND attempted_at > NOW() - INTERVAL '1 hour' AND succeeded = FALSE
	`, userID).Scan(&attempts)
	if attempts >= 5 {
		RespondWithError(w, http.StatusTooManyRequests, "too many recovery attempts — try again in 1 hour")
		return
	}

	ipAddr := strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
	if ipAddr == "" {
		ipAddr = r.RemoteAddr
	}

	// Log this attempt (whether it succeeds or not, recorded later)
	var attemptID int64
	_ = db.QueryRow(`
		INSERT INTO passkey_recovery_attempts (user_id, ip_address) VALUES ($1, $2) RETURNING id
	`, userID, ipAddr).Scan(&attemptID)

	// Verify TOTP
	secret, err := loadTOTPSecret(userID)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "TOTP not configured for this account")
		return
	}
	if !totp.Validate(req.TOTPCode, secret) {
		RespondWithError(w, http.StatusUnauthorized, "invalid authenticator code")
		return
	}

	// Verify recovery key
	identity, err := GetIdentityByUserID(userID)
	if err != nil || identity == nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}
	inputHash := fedcrypto.HashString(req.RecoveryKey)
	if inputHash != identity.RecoveryKeyHash {
		RespondWithError(w, http.StatusUnauthorized, "invalid recovery key")
		return
	}

	// Both factors verified — mark attempt as succeeded
	if attemptID > 0 {
		_, _ = db.Exec(
			`UPDATE passkey_recovery_attempts SET succeeded = TRUE WHERE id = $1`,
			attemptID,
		)
	}

	// Begin passkey registration for this user (no JWT required — recovery flow)
	instance, err := getWA()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "webauthn unavailable")
		return
	}
	user, err := loadWAUser(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}

	creation, sessionData, err := instance.BeginRegistration(user)
	if err != nil {
		log.Printf("PasskeyRecoverBegin BeginRegistration error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to generate recovery challenge")
		return
	}

	sid := genSessionID()
	putWASession(sid, waSession{data: sessionData, forUserID: userID, isRecoveryFlow: true})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sid,
		"options":    creation,
	})
}

// ---------------------------------------------------------------------------
// POST /passkey/recover/complete?session_id=xxx
// Revokes old passkeys, stores new one, rotates recovery key, issues tokens.
// ---------------------------------------------------------------------------

func PasskeyRecoverCompleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		RespondWithError(w, http.StatusBadRequest, "session_id required")
		return
	}
	sess, ok := popWASession(sid)
	if !ok || !sess.isRecoveryFlow {
		RespondWithError(w, http.StatusBadRequest, "recovery session expired or invalid")
		return
	}

	instance, _ := getWA()
	user, err := loadWAUser(sess.forUserID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}

	cred, err := instance.FinishRegistration(user, *sess.data, r)
	if err != nil {
		log.Printf("PasskeyRecoverComplete FinishRegistration error: %v", err)
		RespondWithError(w, http.StatusBadRequest, "passkey verification failed: "+err.Error())
		return
	}

	newRecoveryKey, newRecoveryHash, err := fedcrypto.GenerateRecoveryKey()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "recovery key generation failed")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	// Revoke all previous passkeys
	if _, err = tx.Exec(`DELETE FROM passkeys WHERE user_id = $1`, sess.forUserID); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to revoke old passkeys")
		return
	}
	// Store new passkey
	if _, err = tx.Exec(`
		INSERT INTO passkeys (user_id, credential_id, public_key, sign_count, aaguid)
		VALUES ($1, $2, $3, $4, $5)
	`, sess.forUserID, cred.ID, cred.PublicKey, cred.Authenticator.SignCount,
		string(cred.Authenticator.AAGUID)); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to store new passkey")
		return
	}
	// Rotate recovery key
	if _, err = tx.Exec(`
		UPDATE identities SET recovery_key_hash = $1, updated_at = NOW() WHERE user_id = $2
	`, newRecoveryHash, sess.forUserID); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to rotate recovery key")
		return
	}

	if err = tx.Commit(); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	externalURL := os.Getenv("SERVER_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8080"
	}
	accessToken, refreshToken, err := GenerateTokenPair(sess.forUserID, externalURL)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"user_id":          ToExternalID(sess.forUserID),
		"home_server":      externalURL,
		"access_token":     accessToken,
		"refresh_token":    refreshToken,
		"new_recovery_key": newRecoveryKey,
	})
}

// ---------------------------------------------------------------------------
// GET /passkey/status — list enrolled passkeys for the authenticated user
// ---------------------------------------------------------------------------

func PasskeyStatusHandler(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(
		`SELECT id, aaguid, created_at FROM passkeys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type info struct {
		ID        int64  `json:"id"`
		AAGUID    string `json:"aaguid"`
		CreatedAt string `json:"created_at"`
	}
	var result []info
	for rows.Next() {
		var p info
		var t time.Time
		if err2 := rows.Scan(&p.ID, &p.AAGUID, &t); err2 == nil {
			p.CreatedAt = t.Format(time.RFC3339)
			result = append(result, p)
		}
	}
	if result == nil {
		result = []info{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]any{
		"passkeys": result,
		"count":    len(result),
	})
}

// ---------------------------------------------------------------------------
// POST /passkey/remove — delete an enrolled passkey
// Body: { "passkey_id": 123 }
// ---------------------------------------------------------------------------

func PasskeyRemoveHandler(w http.ResponseWriter, r *http.Request) {
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
		PasskeyID int64 `json:"passkey_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PasskeyID == 0 {
		RespondWithError(w, http.StatusBadRequest, "passkey_id required")
		return
	}

	res, err := db.Exec(
		`DELETE FROM passkeys WHERE id = $1 AND user_id = $2`,
		req.PasskeyID, userID,
	)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		RespondWithError(w, http.StatusNotFound, "passkey not found")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
