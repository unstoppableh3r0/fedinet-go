package identity

// Zero-Knowledge Identity Proofs
//
// A user with an Ed25519 client key registered (zkp_public_key) can prove
// ownership of their account to any verifier WITHOUT disclosing any credential:
//
//   1.  POST /zkp/register-key   – register the client's raw Ed25519 public key
//   2.  POST /zkp/challenge       – server issues a one-time random nonce
//   3.  POST /zkp/prove           – client submits sig(nonce|user_id) signed with
//                                   their private key; server verifies + returns a
//                                   signed, time-limited proof token
//   4.  GET  /zkp/status          – public: check whether a user has ZKP active
//   5.  POST /zkp/verify-token    – public: verify a proof token (audience: peers)
//
// Security properties
//   • The private key never leaves the client.
//   • Each challenge is single-use with a 10-minute TTL.
//   • The proof token carries a server signature so third parties can verify
//     authenticity without contacting the server again.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	fedcrypto "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// ZKPProofToken is the self-contained, server-signed receipt that a client can
// share with third parties to prove they own their account.
type ZKPProofToken struct {
	UserID      string `json:"user_id"`
	ServerID    string `json:"server_id"`
	Challenge   string `json:"challenge"`
	UserSig     string `json:"user_sig"`
	ServerSig   string `json:"server_sig"` // sig over canonical(user_id+challenge+user_sig+issued_at)
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func zkpChallengeMessage(challenge, userID string) string {
	return "zkp-challenge:" + challenge + ":user:" + userID
}

func zkpTokenCanonical(t ZKPProofToken) string {
	return t.UserID + ":" + t.Challenge + ":" + t.UserSig + ":" + t.IssuedAt
}

func generateChallengeID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateChallenge() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ─── 1. Register ZKP public key ──────────────────────────────────────────────
// POST /zkp/register-key
// Authenticated. Body: {"zkp_public_key": "<hex Ed25519 raw public key>"}
// Idempotent – calling again replaces the previous key.

func ZKPRegisterKeyHandler(w http.ResponseWriter, r *http.Request) {
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
		ZKPPublicKey string `json:"zkp_public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ZKPPublicKey == "" {
		RespondWithError(w, http.StatusBadRequest, "zkp_public_key required")
		return
	}

	// Basic validation: must be valid hex and 32 bytes (raw Ed25519 pubkey)
	pubKeyBytes, err := hex.DecodeString(req.ZKPPublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		RespondWithError(w, http.StatusBadRequest, "zkp_public_key must be a 32-byte hex-encoded Ed25519 public key")
		return
	}

	_, err = db.Exec(`
		UPDATE identities
		SET zkp_public_key = $1, updated_at = NOW()
		WHERE user_id = $2
	`, req.ZKPPublicKey, userID)
	if err != nil {
		log.Printf("ZKPRegisterKeyHandler: db error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "zkp_key_registered"})
}

// ─── 2. Issue challenge ───────────────────────────────────────────────────────
// POST /zkp/challenge
// Authenticated. Returns a single-use nonce with a 10-minute TTL.

func ZKPChallengeHandler(w http.ResponseWriter, r *http.Request) {
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

	// Ensure user has a registered ZKP key
	var zkpKey string
	err = db.QueryRow(`SELECT COALESCE(zkp_public_key,'') FROM identities WHERE user_id = $1`, userID).Scan(&zkpKey)
	if err != nil || zkpKey == "" {
		RespondWithError(w, http.StatusConflict, "ZKP not enabled – register a zkp_public_key first")
		return
	}

	challengeID, err := generateChallengeID()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to generate challenge id")
		return
	}
	challenge, err := generateChallenge()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to generate challenge")
		return
	}

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	_, err = db.Exec(`
		INSERT INTO zkp_challenges (id, user_id, challenge, expires_at)
		VALUES ($1, $2, $3, $4)
	`, challengeID, userID, challenge, expiresAt)
	if err != nil {
		log.Printf("ZKPChallengeHandler: db insert error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "database error")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"challenge_id": challengeID,
		"challenge":    challenge,
		"expires_at":   expiresAt.Format(time.RFC3339),
	})
}

// ─── 3. Submit proof ──────────────────────────────────────────────────────────
// POST /zkp/prove
// Authenticated.
// Body: {"challenge_id": "...", "user_sig": "<hex sig of zkp-challenge:<challenge>:user:<user_id>"}
// Returns a signed proof token valid for 1 hour.

func ZKPProveHandler(w http.ResponseWriter, r *http.Request) {
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
	externalUserID := ToExternalID(userID)

	var req struct {
		ChallengeID string `json:"challenge_id"`
		UserSig     string `json:"user_sig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChallengeID == "" || req.UserSig == "" {
		RespondWithError(w, http.StatusBadRequest, "challenge_id and user_sig required")
		return
	}

	// ── Load and consume the challenge ──────────────────────────────────────
	var challenge string
	var expiresAt time.Time
	var used bool
	err = db.QueryRow(`
		SELECT challenge, expires_at, used
		FROM zkp_challenges
		WHERE id = $1 AND user_id = $2
	`, req.ChallengeID, userID).Scan(&challenge, &expiresAt, &used)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "challenge not found")
		return
	}
	if used {
		RespondWithError(w, http.StatusConflict, "challenge already used")
		return
	}
	if time.Now().After(expiresAt) {
		RespondWithError(w, http.StatusGone, "challenge expired")
		return
	}

	// Mark used immediately (even if subsequent steps fail) – prevents replay.
	_, err = db.Exec(`UPDATE zkp_challenges SET used = TRUE WHERE id = $1`, req.ChallengeID)
	if err != nil {
		log.Printf("ZKPProveHandler: failed to mark challenge used: %v", err)
	}

	// ── Verify user signature ────────────────────────────────────────────────
	var zkpPublicKey string
	err = db.QueryRow(`SELECT COALESCE(zkp_public_key,'') FROM identities WHERE user_id = $1`, userID).Scan(&zkpPublicKey)
	if err != nil || zkpPublicKey == "" {
		RespondWithError(w, http.StatusConflict, "ZKP public key not registered")
		return
	}

	message := zkpChallengeMessage(challenge, externalUserID)
	valid, err := fedcrypto.VerifySignature([]byte(message), req.UserSig, zkpPublicKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid proof signature")
		return
	}

	// ── Build and server-sign the proof token ───────────────────────────────
	serverID, _ := getServerIDAndKey()
	issuedAt := time.Now().UTC()
	tokenExpiresAt := issuedAt.Add(1 * time.Hour)

	token := ZKPProofToken{
		UserID:    externalUserID,
		ServerID:  serverID,
		Challenge: challenge,
		UserSig:   req.UserSig,
		IssuedAt:  issuedAt.Format(time.RFC3339),
		ExpiresAt: tokenExpiresAt.Format(time.RFC3339),
	}

	canonical := zkpTokenCanonical(token)
	// Sign the canonical token payload directly with the server's private key.
	_, privKey := getServerIDAndKey()
	tokenSig, err := fedcrypto.SignData([]byte(canonical), privKey)
	if err != nil {
		log.Printf("ZKPProveHandler: server sign error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "server signing failed")
		return
	}
	token.ServerSig = tokenSig

	// Encode as base64 JSON
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "token serialization failed")
		return
	}
	tokenB64 := base64.StdEncoding.EncodeToString(tokenJSON)

	// Record last proof time
	_, _ = db.Exec(`
		UPDATE identities SET zkp_last_proved_at = NOW() WHERE user_id = $1
	`, userID)

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"proof_token": tokenB64,
		"expires_at":  tokenExpiresAt.Format(time.RFC3339),
	})
}

// ─── 4. ZKP status (public) ───────────────────────────────────────────────────
// GET /zkp/status?user_id=...
// Returns whether the user has ZKP enabled and when they last proved.

func ZKPStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := ToInternalID(r.URL.Query().Get("user_id"))
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	var zkpKey string
	var lastProvedAt *time.Time
	err := db.QueryRow(`
		SELECT COALESCE(zkp_public_key,''), zkp_last_proved_at
		FROM identities WHERE user_id = $1
	`, userID).Scan(&zkpKey, &lastProvedAt)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	resp := map[string]interface{}{
		"zkp_enabled": zkpKey != "",
	}
	if lastProvedAt != nil {
		resp["last_proved_at"] = lastProvedAt.Format(time.RFC3339)
	}
	RespondWithJSON(w, http.StatusOK, resp)
}

// ─── 5. Verify proof token (public) ──────────────────────────────────────────
// POST /zkp/verify-token
// Body: {"proof_token": "<base64 JSON>"}
// Verifies the server signature + checks expiry. Anyone can call this.

func ZKPVerifyTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ProofToken string `json:"proof_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProofToken == "" {
		RespondWithError(w, http.StatusBadRequest, "proof_token required")
		return
	}

	tokenJSON, err := base64.StdEncoding.DecodeString(req.ProofToken)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid proof_token encoding")
		return
	}

	var token ZKPProofToken
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid proof_token structure")
		return
	}

	// Check expiry
	exp, err := time.Parse(time.RFC3339, token.ExpiresAt)
	if err != nil || time.Now().After(exp) {
		RespondWithError(w, http.StatusGone, "proof token expired")
		return
	}

	// Verify server signature
	serverPubKey, err := getLocalServerPublicKey()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "cannot load server public key")
		return
	}
	canonical := zkpTokenCanonical(token)
	valid, err := fedcrypto.VerifySignature([]byte(canonical), token.ServerSig, serverPubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid server signature on token")
		return
	}

	// Verify user signature
	userID := ToInternalID(token.UserID)
	var zkpPublicKey string
	err = db.QueryRow(`SELECT COALESCE(zkp_public_key,'') FROM identities WHERE user_id = $1`, userID).Scan(&zkpPublicKey)
	if err != nil || zkpPublicKey == "" {
		RespondWithError(w, http.StatusConflict, "user ZKP key not found")
		return
	}

	message := zkpChallengeMessage(token.Challenge, token.UserID)
	userSigValid, err := fedcrypto.VerifySignature([]byte(message), token.UserSig, zkpPublicKey)
	if err != nil || !userSigValid {
		RespondWithError(w, http.StatusUnauthorized, "invalid user signature in token")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"valid":      true,
		"user_id":    token.UserID,
		"server_id":  token.ServerID,
		"issued_at":  token.IssuedAt,
		"expires_at": token.ExpiresAt,
	})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// getLocalServerPublicKey returns the server's own Ed25519 public key hex.
func getLocalServerPublicKey() (string, error) {
	var pubKey string
	err := db.QueryRow(`SELECT public_key FROM server_identity WHERE id = 1`).Scan(&pubKey)
	if err != nil {
		return "", fmt.Errorf("getLocalServerPublicKey: %w", err)
	}
	return pubKey, nil
}
