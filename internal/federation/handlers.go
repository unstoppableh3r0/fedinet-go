// Package federation provides HTTP handlers for all cross-server (federation)
// communication in FediNet. This includes:
//   - Incoming activity ingestion (Inbox)
//   - Outgoing activity queuing (Outbox, SendActivity)
//   - Mutual-trust handshakes between servers (Handshake, InitiateHandshake)
//   - Server blocking/unblocking management
//   - Federation mode configuration (soft vs. hard federation)
//   - Per-server rate limit configuration
//   - Server capability advertisement and discovery
//   - Health/metrics reporting
//   - HTTP Signature verification middleware
package federation

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

	aimoderation "github.com/unstoppableh3r0/fedinet-go/internal/ai-moderation-service"
	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// SignatureParams holds the parsed components of an HTTP Signature header.
// The HTTP Signatures spec (draft-cavage-http-signatures) requires each
// request to carry a "Signature" header whose value is a comma-separated
// list of key=value pairs.
type SignatureParams struct {
	// KeyID is the URI or identifier of the key used to produce the signature.
	// Typically this is an actor's public key URL, e.g.
	// "https://remote.example/users/alice#main-key".
	KeyID string

	// Algorithm names the signing algorithm, e.g. "rsa-sha256".
	Algorithm string

	// Headers is an ordered list of header names (and the special
	// "(request-target)" pseudo-header) that were included in the
	// signing string.
	Headers []string

	// Signature is the Base64-encoded signature bytes.
	Signature string
}

// ParseSignatureHeader parses a raw HTTP "Signature" header value into its
// component parts. The expected format is:
//
//	keyId="...",algorithm="...",headers="...",signature="..."
//
// Fields may appear in any order. Returns an error if keyId, signature, or
// headers are missing, since those are the minimum required for verification.
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
			// The headers value is a space-separated list of header names.
			headerList := strings.Trim(strings.TrimPrefix(part, "headers="), `"`)
			params.Headers = strings.Split(headerList, " ")
		} else if strings.HasPrefix(part, "signature=") {
			params.Signature = strings.Trim(strings.TrimPrefix(part, "signature="), `"`)
		}
	}

	// A signature is meaningless without a key reference, the actual
	// signature bytes, and the list of headers that were signed.
	if params.KeyID == "" || params.Signature == "" || len(params.Headers) == 0 {
		return nil, fmt.Errorf("invalid signature header: missing required fields")
	}
	return params, nil
}

// FetchServerPublicKey retrieves the RSA public key associated with keyID.
// It searches three sources in order and returns the first match:
//
//  1. identities table — covers locally registered users whose key is cached.
//  2. trusted_servers table — covers remote servers we have already performed
//     a handshake with.
//  3. server_identity table — covers this server's own identity record.
//
// If none of the database lookups succeed, it falls back to a live DNS/HTTP
// resolution via ResolveAccount, which fetches the actor document from the
// remote server and extracts the public key.
func FetchServerPublicKey(keyID string) (string, error) {

	var publicKey string

	// 1. Try local identity cache first (fastest path).
	err := db.QueryRow(
		`SELECT public_key FROM identities WHERE user_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// 2. Try the trusted_servers table (keys exchanged during handshake).
	err = db.QueryRow(
		`SELECT public_key FROM trusted_servers WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		log.Printf("✅ Found key for %s in trusted_servers", keyID)
		return publicKey, nil
	}

	// 3. Try the server_identity table (this server's own published key).
	err = db.QueryRow(
		`SELECT public_key FROM server_identity WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// 4. Fall back to live resolution: fetch the actor document over HTTP.
	doc, err := ResolveAccount(keyID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public key for %s: %w", keyID, err)
	}
	return doc.Identity.PublicKey, nil
}

// VerifySignatureMiddleware is an HTTP middleware that enforces HTTP Signature
// authentication on every request that passes through it. The middleware:
//
//  1. Reads and parses the "Signature" header.
//  2. Fetches the sender's public key (via FetchServerPublicKey).
//  3. Buffers the request body so the digest can be checked without
//     consuming the body for downstream handlers.
//  4. Reconstructs the signing string from the signed headers.
//  5. Validates the body digest (SHA-256) against the "Digest" header.
//  6. Verifies the signature using the sender's RSA public key.
//
// Requests that fail any of these checks are rejected with HTTP 401.
// On success, the original handler (next) is called with the request intact.
func VerifySignatureMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Reject requests with no Signature header immediately.
		sigHeader := r.Header.Get("Signature")
		if sigHeader == "" {
			sendError(w, http.StatusUnauthorized, "missing_signature",
				"Missing Signature header", "")
			return
		}

		// Parse the components of the Signature header.
		params, err := ParseSignatureHeader(sigHeader)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "invalid_signature_header",
				"Invalid Signature header format", err.Error())
			return
		}

		// Resolve the public key for the declared key ID.
		publicKey, err := FetchServerPublicKey(params.KeyID)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "key_not_found",
				"Could not find public key for sender", err.Error())
			return
		}

		// Buffer the entire request body. This is necessary because:
		//   a) We need the raw bytes to compute the SHA-256 digest.
		//   b) After reading, we restore the body so downstream handlers
		//      can decode it normally.
		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				sendError(w, http.StatusBadRequest, "body_read_error",
					"Failed to read request body", err.Error())
				return
			}
			// Replace the consumed body with a fresh reader backed by the buffer.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Build the canonical "signing string" by concatenating the
		// header name–value pairs that were included in the signature,
		// separated by newlines. The order must match params.Headers exactly.
		var signingParts []string
		for _, h := range params.Headers {
			switch h {
			case "(request-target)":
				// The pseudo-header "(request-target)" is defined as
				// "<lowercased-method> <request-uri>".
				requestTarget := strings.ToLower(r.Method) + " " + r.URL.RequestURI()
				signingParts = append(signingParts, "(request-target): "+requestTarget)
			case "host":
				signingParts = append(signingParts, "host: "+r.Host)
			case "date":
				signingParts = append(signingParts, "date: "+r.Header.Get("Date"))
			case "digest":
				// Compute the SHA-256 digest of the body and verify it matches
				// the "Digest" header sent by the remote server. This prevents
				// body tampering even if the signature itself is valid.
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
				// For any other header, include it verbatim using its canonical form.
				signingParts = append(signingParts, h+": "+r.Header.Get(http.CanonicalHeaderKey(h)))
			}
		}
		signingString := strings.Join(signingParts, "\n")

		// Verify the signature using the sender's public key.
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
		// Signature is valid — hand off to the next handler.
		next(w, r)
	}
}

// InboxHandler handles POST /federation/inbox.
//
// This is the primary entry point for all inbound federated activities from
// remote servers. The handler:
//
//  1. Decodes the InboxRequest JSON payload.
//  2. Runs the activity content through the AI moderation service. If the
//     AI service is unavailable, it defaults to SAFE to avoid dropping
//     valid federated content.
//  3. Validates that required fields (activity_type, actor, actor_server)
//     are present.
//  4. Delegates processing to ProcessInboundActivity, which persists the
//     activity and applies federation rules (server blocks, rate limits, etc.).
//
// Specific error codes returned:
//   - server_blocked (403) — the sender's server is on the block list.
//   - rate_limit (429)     — the sender has exceeded its request quota.
//   - internal_error (500) — any other processing failure.
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

	// Extract the text content from the payload for moderation.
	content := fmt.Sprintf("%v", req.Payload["content"])

	aiResult, err := aimoderation.CallModerationAPI(content)

	if err != nil {
		// The AI service being down is a non-fatal error for federation.
		// We log it and allow the content through rather than rejecting
		// legitimate activities from federated peers.
		log.Println("Incoming moderation unavailable — allowing content:", err)

		aiResult = &aimoderation.ModerationResponse{
			Recommendation: "SAFE",
		}
	}

	log.Println("Incoming moderation result:", aiResult.Recommendation)

	// Validate the minimum required fields before attempting to process.
	if req.ActivityType == "" || req.Actor == "" || req.ActorServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
		return
	}

	// Delegate to the service layer; this persists the activity and
	// enforces federation policy (blocks, rate limits, etc.).
	activityID, err := ProcessInboundActivity(
		req.ActivityType,
		req.Actor,
		req.ActorServer,
		req.Target,
		req.Payload,
	)

	if err != nil {
		// Map well-known domain errors to appropriate HTTP status codes.
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

// OutboxHandler handles GET /federation/outbox.
//
// Returns the most recent outbound activities for a given actor, up to a
// hard-coded limit of 50 items. The actor is identified by the required
// "actor_id" query parameter.
//
// This endpoint is used by remote servers that want to replay or audit the
// local server's published activity history.
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

	// Cap results to prevent excessively large responses.
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

// SendActivityHandler handles POST /federation/send.
//
// Queues a new outbound federation activity for delivery to a remote server.
// The caller supplies the activity type, the local actor initiating the action,
// the destination server, an optional target object ID, and an arbitrary
// payload map.
//
// The activity is not delivered synchronously — it is persisted and picked up
// by the background delivery worker (PublishOutboundActivity). A 201 Created
// response with the new activity ID is returned immediately.
func SendActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	// Inline struct for the request body — keeps the schema co-located
	// with the handler that owns it.
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

// AcknowledgmentHandler handles POST /federation/ack.
//
// Remote servers call this endpoint to report the delivery outcome of an
// activity that was previously sent to them. The status update is recorded
// by TrackDeliveryState, which the retry worker uses to decide whether to
// attempt re-delivery.
//
// The "status" field is required; "reason" is optional and provides detail
// on failures (e.g. a human-readable error message from the remote side).
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

// CapabilitiesHandler handles GET /federation/capabilities.
//
// Returns this server's own capability advertisement — a structured document
// describing which federation features and protocol extensions the server
// supports. Remote servers use this during capability negotiation to
// determine which activities and behaviours are safe to use.
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

// DiscoverCapabilitiesHandler handles POST /federation/capabilities/discover.
//
// Fetches and returns the capability document published by a remote server.
// The caller supplies the remote server's base URL in the "server_url" field
// of the JSON body. This is typically called before initiating federation
// with a previously unknown server to check protocol compatibility.
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

// HealthHandler handles GET /federation/health.
//
// Returns a snapshot of this server's federation health metrics, including
// uptime, message counters, delivery success/failure rates, pending retries,
// average latency, active connections, blocked server count, and rate-limit
// violation count. Useful for monitoring dashboards and inter-server health
// probes.
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

	// Map internal domain struct to the API response model.
	// Keeping these separate prevents exposing internal field names over the wire.
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

// BlockedServersHandler handles GET|POST|DELETE /federation/blocked-servers.
//
// A single endpoint that multiplexes three sub-operations based on the HTTP
// method:
//   - GET    → list all currently blocked servers
//   - POST   → add a server to the block list
//   - DELETE → remove a server from the block list
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

// handleGetBlockedServers fetches and returns the full list of blocked servers.
// Each entry in the response includes details such as the server URL, the
// block reason, when the block was created, and its optional expiry.
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

// handleBlockServer adds a remote server to the block list.
// Both "server_url" and "reason" are required. The optional "expires_at"
// field allows temporary blocks that automatically expire.
// The block is attributed to the "system" actor since this endpoint is
// typically called by an admin or automated moderation process.
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

	// "system" is used as the blocking actor for programmatically triggered blocks.
	err := BlockServer(req.ServerURL, req.Reason, "system", req.ExpiresAt)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to block server", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Server blocked successfully", map[string]interface{}{
		"server_url": req.ServerURL,
	})
}

// handleUnblockServer removes a server from the block list.
// The target server is identified by the "server_url" query parameter.
// Future inbound activities from this server will be allowed through
// once it has been unblocked.
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

// FederationModeHandler handles GET|PUT /federation/mode.
//
// Multiplexes between reading (GET) and updating (PUT) the federation mode:
//   - GET → returns the current federation configuration
//   - PUT → updates the mode and associated policy flags
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

// handleGetFederationMode fetches and returns the current federation
// configuration, including the mode ("soft" or "hard"), and the values
// of associated policy flags such as allow_unknown_servers and
// require_capability_negotiation.
func handleGetFederationMode(w http.ResponseWriter, r *http.Request) {
	config, err := GetFederationConfig()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to get config", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleSetFederationMode updates the federation mode and associated policy flags.
//
// Valid values for "mode":
//   - "soft" — more permissive; unknown servers may be allowed; validation
//     is lenient. Suitable for open, public federations.
//   - "hard" — strict; only servers that have completed a handshake are
//     accepted; capability negotiation may be required; validation is strict.
//     Suitable for closed or invite-only federations.
func handleSetFederationMode(w http.ResponseWriter, r *http.Request) {
	var req models.FederationModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	// Only "soft" and "hard" are valid federation modes.
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

// RateLimitsHandler handles POST /federation/rate-limits.
//
// Creates or updates a per-server, per-endpoint rate limit rule. The rule is
// stored in the rate_limits table and is read by the InboxHandler path when
// processing inbound activities from the specified server.
//
// Both "server_url" and "endpoint" are required to uniquely identify the rule.
// "requests_per_min" and "burst_allowance" control the throttle parameters.
// An upsert (INSERT … ON CONFLICT DO UPDATE) is used so that the caller does
// not need to know whether a rule already exists.
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

	// Upsert the rate limit rule. updated_at is refreshed so monitoring tools
	// can see when the configuration was last changed.
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

// HandshakeHandler handles POST /federation/handshake.
//
// This is the *receiver* side of the mutual-trust handshake protocol.
// A remote server POSTs its own identity (server_id, server_name, public_key,
// endpoint) to establish trust. This server:
//
//  1. Stores the remote server's public key in trusted_servers — subsequent
//     signature verifications for inbound activities from that server will use
//     this cached key.
//  2. Responds with its own identity (server_id, server_name, public_key)
//     so the caller can reciprocally trust this server.
//
// Together with InitiateHandshakeHandler, this enables a two-way trust
// establishment in a single round-trip.
func HandshakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	// Inline struct for the remote server's identity payload.
	var req struct {
		ServerID   string `json:"server_id"`
		ServerName string `json:"server_name"`
		PublicKey  string `json:"public_key"`
		Endpoint   string `json:"endpoint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.ServerID == "" || req.ServerName == "" || req.PublicKey == "" || req.Endpoint == "" {
		sendError(w, http.StatusBadRequest, "missing_fields",
			"server_id, server_name, public_key, and endpoint are required", "")
		return
	}

	// Store (or refresh) the remote server's trust record.
	// ON CONFLICT … DO UPDATE means re-handshaking is idempotent and will
	// update a stale public key if the remote server rotated its key pair.
	_, err := db.Exec(`
		INSERT INTO trusted_servers (server_id, server_name, public_key, endpoint)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id) DO UPDATE SET
			server_name = EXCLUDED.server_name,
			public_key = EXCLUDED.public_key,
			endpoint = EXCLUDED.endpoint,
			trusted_at = NOW()
	`, req.ServerID, req.ServerName, req.PublicKey, req.Endpoint)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error",
			"Failed to store remote server key", err.Error())
		return
	}

	log.Printf("🤝 Handshake received from %s (%s)", req.ServerName, req.ServerID)

	// Look up this server's own identity to include in the response,
	// allowing the caller to reciprocally trust us.
	var localID, localName, localKey string
	err = db.QueryRow(`SELECT server_id, server_name, public_key FROM server_identity WHERE id = 1`).
		Scan(&localID, &localName, &localKey)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "not_initialized",
			"This server is not yet initialized", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Handshake accepted", map[string]interface{}{
		"server_id":   localID,
		"server_name": localName,
		"public_key":  localKey,
	})
}

// InitiateHandshakeHandler handles POST /federation/handshake/initiate.
//
// This is the *initiator* side of the mutual-trust handshake protocol.
// The admin or system provides a "target_server" base URL, and this handler:
//
//  1. Reads this server's own identity from server_identity.
//  2. POSTs that identity to the remote server's /federation/handshake endpoint.
//  3. Parses the remote server's identity from the response.
//  4. Stores the remote server's public key in trusted_servers.
//
// After a successful call to this handler, both servers trust each other and
// can verify each other's HTTP Signatures without needing live key resolution.
func InitiateHandshakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req struct {
		TargetServer string `json:"target_server"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	if req.TargetServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "target_server is required", "")
		return
	}

	// Read this server's own credentials to send as the handshake payload.
	var localID, localName, localKey string
	err := db.QueryRow(`SELECT server_id, server_name, public_key FROM server_identity WHERE id = 1`).
		Scan(&localID, &localName, &localKey)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "not_initialized",
			"This server is not yet initialized", err.Error())
		return
	}

	// Build the identity payload to send to the remote server.
	// The endpoint is derived from the Host header of the incoming request
	// so we advertise the correct reachable URL.
	payload := map[string]string{
		"server_id":   localID,
		"server_name": localName,
		"public_key":  localKey,
		"endpoint":    "http://" + r.Host,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error",
			"Failed to serialize handshake payload", err.Error())
		return
	}

	// Construct the target handshake URL and initiate the HTTP request.
	targetURL := strings.TrimRight(req.TargetServer, "/") + "/federation/handshake"
	log.Printf("🤝 Initiating handshake with %s", targetURL)

	resp, err := http.Post(targetURL, "application/json", bytes.NewReader(payloadJSON))
	if err != nil {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Failed to connect to remote server", err.Error())
		return
	}
	defer resp.Body.Close()

	// Decode the remote server's response envelope.
	var remoteResp models.FederationResponse
	if err := json.NewDecoder(resp.Body).Decode(&remoteResp); err != nil {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Failed to parse remote server response", err.Error())
		return
	}

	// Check whether the remote server accepted our handshake.
	if !remoteResp.Success {
		errMsg := "unknown error"
		if remoteResp.Error != nil {
			errMsg = remoteResp.Error.Message
		}
		sendError(w, http.StatusBadGateway, "handshake_rejected",
			"Remote server rejected handshake", errMsg)
		return
	}

	// Extract the remote server's identity fields from the response data map.
	remoteID, _ := remoteResp.Data["server_id"].(string)
	remoteName, _ := remoteResp.Data["server_name"].(string)
	remoteKey, _ := remoteResp.Data["public_key"].(string)

	// Both fields are needed to establish a useful trust record.
	if remoteID == "" || remoteKey == "" {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Remote server returned incomplete identity", "")
		return
	}

	// Persist the remote server's trust record locally. Subsequent inbound
	// requests signed with its key will be verifiable without a live fetch.
	_, err = db.Exec(`
		INSERT INTO trusted_servers (server_id, server_name, public_key, endpoint)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id) DO UPDATE SET
			server_name = EXCLUDED.server_name,
			public_key = EXCLUDED.public_key,
			endpoint = EXCLUDED.endpoint,
			trusted_at = NOW()
	`, remoteID, remoteName, remoteKey, req.TargetServer)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error",
			"Failed to store remote server key", err.Error())
		return
	}

	log.Printf("✅ Handshake complete with %s (%s)", remoteName, remoteID)

	// Return a summary of the newly established mutual trust relationship.
	sendSuccess(w, http.StatusOK, "Handshake complete — mutual trust established", map[string]interface{}{
		"local_server": map[string]string{
			"server_id":   localID,
			"server_name": localName,
		},
		"remote_server": map[string]string{
			"server_id":   remoteID,
			"server_name": remoteName,
		},
		"status": "trusted",
	})
}

// sendSuccess writes a successful JSON response with the given HTTP status code.
// All success responses share a common envelope shape (models.FederationResponse)
// so clients can handle them uniformly regardless of the specific endpoint.
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

// sendError writes an error JSON response with the given HTTP status code.
// The errorType string is a machine-readable error code (e.g. "missing_fields")
// that clients can use for programmatic error handling. The message is a
// human-readable summary, and details (if non-empty) provides extra context
// such as the underlying Go error string.
//
// All errors are also logged for server-side diagnostics.
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

// HandleFederatedLookup handles GET /federation/lookup.
//
// Resolves a federated identity handle (passed as the "id" query parameter)
// to an identity and profile document. The lookup is performed by
// ResolveAccount, which checks the local database first and falls back to a
// live HTTP fetch from the handle's home server if needed.
//
// This endpoint is used by remote servers and clients that want to discover
// information about an actor (e.g. their public key, display name, avatar)
// without authenticating.
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

	doc, err := ResolveAccount(handle)
	if err != nil {
		// Distinguish "not found" from other resolution errors so the caller
		// gets an appropriate 404 rather than a generic 500.
		if err.Error() == "identity not found" {
			sendError(w, http.StatusNotFound, "not_found", "Identity not found", "")
			return
		}

		sendError(w, http.StatusInternalServerError, "lookup_failed", "Failed to resolve identity", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Lookup successful", map[string]interface{}{
		"handle":   handle,
		"identity": doc.Identity,
		"profile":  doc.Profile,
	})
}
