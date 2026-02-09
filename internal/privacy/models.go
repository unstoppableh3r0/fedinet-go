package main

import (
	"time"
)

// Visibility represents content visibility settings (from identity module)
type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityFollowers Visibility = "followers"
	VisibilityPrivate   Visibility = "private"
	VisibilityServer    Visibility = "server"
)

// =================================================================
// EPIC 3 — PRIVACY, ENCRYPTION & USER SAFETY
// =================================================================

type PrivacyAuditLog struct {
	ID            string    `json:"id" db:"id"`
	ActorID       string    `json:"actor_id" db:"actor_id"`
	TargetID      string    `json:"target_id" db:"target_id"`
	Action        string    `json:"action" db:"action"`
	AccessGranted bool      `json:"access_granted" db:"access_granted"`
	Reason        string    `json:"reason" db:"reason"`
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
}

type ProxyRequest struct {
	RequestID   string    `json:"request_id"`
	OriginalURL string    `json:"original_url"`
	Method      string    `json:"method"`
	UserAgent   string    `json:"user_agent"`
	CreatedAt   time.Time `json:"created_at"`
}

type EncryptionEnvelope struct {
	KeyID      string `json:"kid"`
	Algorithm  string `json:"alg"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type VisibilityLevel string

const (
	VisibilityCircle  VisibilityLevel = "circle"
	VisibilityMutuals VisibilityLevel = "mutuals"
)
