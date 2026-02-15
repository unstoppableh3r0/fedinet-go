package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ============================================================================
// Epic 3: HTTP Signature Verification Middleware
// ============================================================================

// SignatureParams holds the parsed components of an HTTP Signature header.
type SignatureParams struct {
	KeyID     string
	Algorithm string
	Headers   []string
	Signature string
}

// ParseSignatureHeader parses a Signature header value into its components.
// Expected format: keyId="...",algorithm="...",headers="...",signature="..."
func ParseSignatureHeader(header string) (*SignatureParams, error) {
	params := &SignatureParams{}
	parts := strings.Split(header, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "keyId=") {
			params.KeyID = strings.Trim(strings.TrimPrefix(part, "keyId="), `"`)
		} else if strings.HasPrefix(part, "algorithm=") {
			params.Algorithm = strings.Trim(strings.TrimPrefix(part, "algorithm="), `"`)
		} else if strings.HasPrefix(part, "headers=") {
			headerList := strings.Trim(strings.TrimPrefix(part, "headers="), `"`)
			params.Headers = strings.Split(headerList, " ")
		} else if strings.HasPrefix(part, "signature=") {
			params.Signature = strings.Trim(strings.TrimPrefix(part, "signature="), `"`)
		}
	}

	if params.KeyID == "" || params.Signature == "" || len(params.Headers) == 0 {
		return nil, fmt.Errorf("invalid signature header: missing required fields")
	}
	return params, nil
}

// FetchServerPublicKey retrieves the public key for a given keyId.
// It first checks the local identities table, then falls back to federated lookup.
func FetchServerPublicKey(keyID string) (string, error) {
	// 1. Try local DB lookup (server_identity or identities table)
	var publicKey string
	err := db.QueryRow(
		`SELECT public_key FROM identities WHERE user_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// 2. Try server_identity table (in case keyId is a server ID)
	err = db.QueryRow(
		`SELECT public_key FROM server_identity WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// 3. Fallback: Resolve via federated lookup
	doc, err := ResolveAccount(keyID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public key for %s: %w", keyID, err)
	}
	return doc.Identity.PublicKey, nil
}

// VerifySignatureMiddleware wraps an http.HandlerFunc to verify HTTP Signatures.
// If verification fails, it returns 401 Unauthorized.
func VerifySignatureMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sigHeader := r.Header.Get("Signature")
		if sigHeader == "" {
			sendError(w, http.StatusUnauthorized, "missing_signature",
				"Missing Signature header", "")
			return
		}

		// 1. Parse the Signature header
		params, err := ParseSignatureHeader(sigHeader)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "invalid_signature_header",
				"Invalid Signature header format", err.Error())
			return
		}

		// 2. Fetch the public key for the keyId
		publicKey, err := FetchServerPublicKey(params.KeyID)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "key_not_found",
				"Could not find public key for sender", err.Error())
			return
		}

		// 3. Read and restore the request body for digest computation
		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				sendError(w, http.StatusBadRequest, "body_read_error",
					"Failed to read request body", err.Error())
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 4. Reconstruct the signing string from the declared headers
		var signingParts []string
		for _, h := range params.Headers {
			switch h {
			case "(request-target)":
				requestTarget := strings.ToLower(r.Method) + " " + r.URL.RequestURI()
				signingParts = append(signingParts, "(request-target): "+requestTarget)
			case "host":
				signingParts = append(signingParts, "host: "+r.Host)
			case "date":
				signingParts = append(signingParts, "date: "+r.Header.Get("Date"))
			case "digest":
				// Recompute digest from the body and compare
				digest := sha256.Sum256(bodyBytes)
				computedDigest := "SHA-256=" + hex.EncodeToString(digest[:])
				receivedDigest := r.Header.Get("Digest")
				if receivedDigest != computedDigest {
					sendError(w, http.StatusUnauthorized, "digest_mismatch",
						"Body digest does not match Digest header", "")
					return
				}
				signingParts = append(signingParts, "digest: "+computedDigest)
			default:
				// Support any other standard header
				signingParts = append(signingParts, h+": "+r.Header.Get(http.CanonicalHeaderKey(h)))
			}
		}
		signingString := strings.Join(signingParts, "\n")

		// 5. Verify the signature
		valid, err := crypto.VerifySignature([]byte(signingString), params.Signature, publicKey)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "verification_error",
				"Signature verification failed", err.Error())
			return
		}
		if !valid {
			sendError(w, http.StatusUnauthorized, "invalid_signature",
				"Signature is invalid", "")
			return
		}

		log.Printf("✅ Signature verified for keyId=%s", params.KeyID)
		next(w, r)
	}
}

// ============================================================================
// User Story 2.4: Inbox / Outbox Architecture
// ============================================================================

// InboxHandler receives incoming federated activities
func InboxHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req models.InboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	// Validate required fields
	if req.ActivityType == "" || req.Actor == "" || req.ActorServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
		return
	}

	// Verify Signature
	if err := VerifyRequestSignature(req); err != nil {
		sendError(w, http.StatusUnauthorized, "invalid_signature", "Signature verification failed", err.Error())
		return
	}

	// Process the inbound activity
	activityID, err := ProcessInboundActivity(
		req.ActivityType,
		req.Actor,
		req.ActorServer,
		req.Target,
		req.Payload,
	)

	if err != nil {
		if err.Error() == "sender server is blocked" {
			sendError(w, http.StatusForbidden, "server_blocked", "Sender server is blocked", "")
		} else if err.Error() == "rate limit exceeded" {
			sendError(w, http.StatusTooManyRequests, "rate_limit", "Rate limit exceeded", "")
		} else {
			sendError(w, http.StatusInternalServerError, "internal_error", "Failed to process activity", err.Error())
		}
		return
	}

	sendSuccess(w, http.StatusOK, "Activity received", map[string]interface{}{
		"activity_id": activityID,
	})
}

// OutboxHandler serves outgoing activities
func OutboxHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	actorID := r.URL.Query().Get("actor_id")
	if actorID == "" {
		sendError(w, http.StatusBadRequest, "missing_actor", "actor_id parameter required", "")
		return
	}

	// Default limit
	limit := 50

	activities, err := GetOutboxActivities(actorID, limit)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch activities", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"activities": activities,
		"count":      len(activities),
	})
}

// SendActivityHandler initiates outbound federation
func SendActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req struct {
		ActivityType string                 `json:"activity_type"`
		ActorID      string                 `json:"actor_id"`
		TargetServer string                 `json:"target_server"`
		TargetID     *string                `json:"target_id,omitempty"`
		Payload      map[string]interface{} `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.ActivityType == "" || req.ActorID == "" || req.TargetServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
		return
	}

	activityID, err := PublishOutboundActivity(
		req.ActivityType,
		req.ActorID,
		req.TargetServer,
		req.TargetID,
		req.Payload,
	)

	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to send activity", err.Error())
		return
	}

	sendSuccess(w, http.StatusCreated, "Activity queued for delivery", map[string]interface{}{
		"activity_id": activityID,
	})
}

// ============================================================================
// User Story 2.7: Delivery Acknowledgment
// ============================================================================

// AcknowledgmentHandler receives delivery confirmations
func AcknowledgmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req models.AcknowledgmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.Status == "" {
		sendError(w, http.StatusBadRequest, "missing_status", "Status field required", "")
		return
	}

	err := TrackDeliveryState(req.MessageID, req.Status, req.Reason)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to track acknowledgment", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Acknowledgment recorded", nil)
}

// ============================================================================
// User Story 2.11: Capability Negotiation
// ============================================================================

// CapabilitiesHandler returns server capabilities
func CapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	caps, err := AdvertiseCapabilities()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to get capabilities", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caps)
}

// DiscoverCapabilitiesHandler discovers capabilities of a remote server
func DiscoverCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req models.CapabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.ServerURL == "" {
		sendError(w, http.StatusBadRequest, "missing_server", "server_url field required", "")
		return
	}

	caps, err := DiscoverRemoteCapabilities(req.ServerURL)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "discovery_failed", "Failed to discover capabilities", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caps)
}

// ============================================================================
// User Story 2.14: Instance Health API
// ============================================================================

// HealthHandler returns instance health status
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	health, err := GetHealthStatus()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to get health status", err.Error())
		return
	}

	response := models.HealthResponse{
		Status:               health.Status,
		Timestamp:            health.LastHealthCheckAt,
		Uptime:               health.UptimeSeconds,
		TotalMessages:        health.TotalMessages,
		SuccessfulDeliveries: health.SuccessfulDeliveries,
		FailedDeliveries:     health.FailedDeliveries,
		PendingRetries:       health.PendingRetries,
		AverageLatencyMs:     health.AverageLatencyMs,
		ActiveConnections:    health.ActiveConnections,
		BlockedServers:       health.BlockedServersCount,
		RateLimitViolations:  health.RateLimitViolations,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ============================================================================
// User Story 2.12: Blocked Server Lists (Admin Endpoints)
// ============================================================================

// BlockedServersHandler manages blocked servers
func BlockedServersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetBlockedServers(w, r)
	case http.MethodPost:
		handleBlockServer(w, r)
	case http.MethodDelete:
		handleUnblockServer(w, r)
	default:
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
	}
}

func handleGetBlockedServers(w http.ResponseWriter, r *http.Request) {
	servers, err := GetBlockedServers()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch blocked servers", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"blocked_servers": servers,
		"count":           len(servers),
	})
}

func handleBlockServer(w http.ResponseWriter, r *http.Request) {
	var req models.BlockServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.ServerURL == "" || req.Reason == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "server_url and reason required", "")
		return
	}

	// For now, assume admin is "system" - in production, get from auth
	err := BlockServer(req.ServerURL, req.Reason, "system", req.ExpiresAt)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to block server", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Server blocked successfully", map[string]interface{}{
		"server_url": req.ServerURL,
	})
}

func handleUnblockServer(w http.ResponseWriter, r *http.Request) {
	serverURL := r.URL.Query().Get("server_url")
	if serverURL == "" {
		sendError(w, http.StatusBadRequest, "missing_server", "server_url parameter required", "")
		return
	}

	err := UnblockServer(serverURL)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to unblock server", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Server unblocked successfully", map[string]interface{}{
		"server_url": serverURL,
	})
}

// ============================================================================
// User Story 2.13: Soft / Hard Federation Modes (Admin Endpoint)
// ============================================================================

// FederationModeHandler configures federation mode
func FederationModeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetFederationMode(w, r)
	case http.MethodPut:
		handleSetFederationMode(w, r)
	default:
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and PUT allowed", "")
	}
}

func handleGetFederationMode(w http.ResponseWriter, r *http.Request) {
	config, err := GetFederationConfig()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to get config", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func handleSetFederationMode(w http.ResponseWriter, r *http.Request) {
	var req models.FederationModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.Mode != "soft" && req.Mode != "hard" {
		sendError(w, http.StatusBadRequest, "invalid_mode", "Mode must be 'soft' or 'hard'", "")
		return
	}

	err := SetFederationMode(req.Mode, req.AllowUnknownServers, req.RequireCapabilityNeg, req.StrictValidation)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to set mode", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Federation mode updated", map[string]interface{}{
		"mode": req.Mode,
	})
}

// ============================================================================
// User Story 2.8: Rate Limiting (Admin Endpoint)
// ============================================================================

// RateLimitsHandler manages rate limits
func RateLimitsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req models.RateLimitConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.ServerURL == "" || req.Endpoint == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "server_url and endpoint required", "")
		return
	}

	_, err := db.Exec(`
		INSERT INTO rate_limits (server_url, endpoint, requests_per_min, burst_allowance)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_url, endpoint) DO UPDATE SET
			requests_per_min = $3,
			burst_allowance = $4,
			updated_at = NOW()
	`, req.ServerURL, req.Endpoint, req.RequestsPerMin, req.BurstAllowance)

	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to set rate limit", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Rate limit configured", nil)
}

// ============================================================================
// Helper Functions
// ============================================================================

func sendSuccess(w http.ResponseWriter, statusCode int, message string, data map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := models.FederationResponse{
		Success: true,
		Message: message,
		Data:    data,
	}

	json.NewEncoder(w).Encode(response)
}

func sendError(w http.ResponseWriter, statusCode int, errorType, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := models.FederationResponse{
		Success: false,
		Error: &models.ErrorResponse{
			Code:    statusCode,
			Type:    errorType,
			Message: message,
			Details: details,
		},
	}

	json.NewEncoder(w).Encode(response)

	log.Printf("Error [%s]: %s - %s", errorType, message, details)
}

// ============================================================================
// User Story 1.2: Cross-Instance Account Discovery
// ============================================================================

// HandleFederatedLookup resolves a user handle to an identity
func HandleFederatedLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	handle := r.URL.Query().Get("id")
	if handle == "" {
		sendError(w, http.StatusBadRequest, "missing_param", "id parameter required", "")
		return
	}

	// Logic to resolve identity
	doc, err := ResolveAccount(handle)
	if err != nil {
		if err.Error() == "identity not found" {
			sendError(w, http.StatusNotFound, "not_found", "Identity not found", "")
			return
		}
		// Check for other errors or return internal error
		sendError(w, http.StatusInternalServerError, "lookup_failed", "Failed to resolve identity", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Lookup successful", map[string]interface{}{
		"handle":   handle,
		"identity": doc.Identity,
		"profile":  doc.Profile,
	})
}
