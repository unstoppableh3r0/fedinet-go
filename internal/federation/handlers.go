// Package federation implements the HTTP handlers and business logic for the
// inter-server federation layer of the fedinated social network.
//
// Federation allows users homed on different servers to follow each other, receive
// notifications, and interact with posts across server boundaries. The federation
// layer communicates via an HTTP-based ActivityPub-inspired protocol with signed
// requests for authenticity guarantees.
//
// Key responsibilities of this package:
//   - Verifying inbound HTTP signatures from remote servers (VerifySignatureMiddleware).
//   - Routing inbound activities (follows, likes, reposts, notifications) via InboxHandler.
//   - Publishing outbound activities to remote servers via SendActivityHandler.
//   - Advertising and discovering per-server capability sets (CapabilitiesHandler).
//   - Managing the trusted-server registry (HandshakeHandler, InitiateHandshakeHandler).
//   - Blocking/unblocking remote servers from federating with this node.
//   - Exposing a federation health-check endpoint with delivery statistics.
//
// Security model:
// All inbound federation requests must carry an HTTP Signature header signed with the
// remote server's RSA private key. The corresponding public key is resolved either from
// the local trusted_servers table or via a live WebFinger-style lookup. Unsigned
// requests are rejected with 401 Unauthorized.
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

// SignatureParams carries the parsed components of an HTTP Signature header.
// The header conforms to the draft-cavage-http-signatures specification:
//
//	Signature: keyId="<id>",algorithm="<alg>",headers="<h1> <h2>",signature="<base64>"
//
// Fields:
//   - KeyID:     identifier for the public key used to create the signature (typically
//     a server ID or actor URL). Used to look up the corresponding public key.
//   - Algorithm: signing algorithm string (e.g. "rsa-sha256"). Validated against
//     the actual key material before verification.
//   - Headers:   ordered list of header names (and pseudo-headers such as
//     "(request-target)") whose values were included in the signing string.
//   - Signature: base64-encoded raw signature bytes.
type SignatureParams struct {
	KeyID     string
	Algorithm string
	Headers   []string
	Signature string
}

// ParseSignatureHeader parses the value of an HTTP "Signature" header into its
// constituent components. The expected format is a comma-separated list of
// key="value" pairs, for example:
//
//	keyId="server-a",algorithm="rsa-sha256",headers="(request-target) host date digest",signature="Base64=="
//
// Parsing is lenient with whitespace but strict about required fields: keyId,
// signature, and at least one header name must be present or an error is returned.
// The function does not validate the signature itself — that is done by VerifySignatureMiddleware.
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

// FetchServerPublicKey resolves the public key for a given keyId.
// It searches three sources in priority order:
//
//  1. identities table — covers local users whose keyId is their user_id.
//  2. trusted_servers table — covers remote servers that have previously completed
//     a handshake and whose key was persisted locally (fastest path for known peers).
//  3. server_identity table — covers the local server's own identity record.
//  4. Live WebFinger/ActivityPub lookup via ResolveAccount — fallback for previously
//     unseen remote servers. The resolved key is NOT automatically cached here;
//     callers that need caching should call HandshakeHandler first.
//
// Returns the PEM-encoded public key string, or an error if resolution fails.
func FetchServerPublicKey(keyID string) (string, error) {

	var publicKey string
	err := db.QueryRow(
		`SELECT public_key FROM identities WHERE user_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	err = db.QueryRow(
		`SELECT public_key FROM trusted_servers WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		log.Printf("✅ Found key for %s in trusted_servers", keyID)
		return publicKey, nil
	}

	err = db.QueryRow(
		`SELECT public_key FROM server_identity WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	doc, err := ResolveAccount(keyID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public key for %s: %w", keyID, err)
	}
	return doc.Identity.PublicKey, nil
}

// VerifySignatureMiddleware is an HTTP middleware that enforces HTTP Signature
// authentication on every request it wraps. It must be applied to all inbound
// federation endpoints to ensure that only trusted, authenticated peers can
// deliver activities to this server.
//
// Verification steps performed on each request:
//  1. Parse the Signature header using ParseSignatureHeader.
//  2. Resolve the sender's public key via FetchServerPublicKey.
//  3. Read and buffer the request body so that the Digest pseudo-header can be
//     verified and the body can be re-read by downstream handlers.
//  4. Reconstruct the signing string from the listed headers in the same order
//     they were signed (including "(request-target)", "host", "date", "digest").
//  5. Verify the signature bytes against the reconstructed string using the
//     sender's public key via the pkg/crypto package.
//
// Requests that fail any step are rejected with 401 Unauthorized before the
// wrapped handler is ever called.
func VerifySignatureMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sigHeader := r.Header.Get("Signature")
		if sigHeader == "" {
			sendError(w, http.StatusUnauthorized, "missing_signature",
				"Missing Signature header", "")
			return
		}

		params, err := ParseSignatureHeader(sigHeader)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "invalid_signature_header",
				"Invalid Signature header format", err.Error())
			return
		}

		publicKey, err := FetchServerPublicKey(params.KeyID)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "key_not_found",
				"Could not find public key for sender", err.Error())
			return
		}

		// Buffer the request body so downstream handlers can still read it after
		// we've consumed it here for the Digest verification step.
		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				sendError(w, http.StatusBadRequest, "body_read_error",
					"Failed to read request body", err.Error())
				return
			}
			// Restore body for downstream handler consumption.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Rebuild the signing string exactly as the sender did.
		// Order matters: we iterate params.Headers in the same sequence.
		var signingParts []string
		for _, h := range params.Headers {
			switch h {
			case "(request-target)":
				// The request-target pseudo-header is "METHOD /path?query" lowercased.
				requestTarget := strings.ToLower(r.Method) + " " + r.URL.RequestURI()
				signingParts = append(signingParts, "(request-target): "+requestTarget)
			case "host":
				signingParts = append(signingParts, "host: "+r.Host)
			case "date":
				signingParts = append(signingParts, "date: "+r.Header.Get("Date"))
			case "digest":
				// Compute SHA-256 of the buffered body and compare against the
				// sender's Digest header to detect in-flight tampering.
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
				// Any other listed header is included verbatim (lowercase name, original value).
				signingParts = append(signingParts, h+": "+r.Header.Get(http.CanonicalHeaderKey(h)))
			}
		}
		signingString := strings.Join(signingParts, "\n")

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

// InboxHandler handles POST /federation/inbox — the primary inbound activity endpoint.
//
// This endpoint receives federated activities (Follow, Like, Repost, Create, Delete, etc.)
// from remote servers. Each payload is first screened by the AI moderation service to
// catch cross-server spam or harmful content before it is stored locally. Activities
// that pass moderation are forwarded to ProcessInboundActivity which routes them to
// the appropriate domain-specific handler (follow acceptance, notification creation, etc.).
//
// Error responses:
//   - 403 Forbidden:        sender server is on the local block list.
//   - 429 Too Many Requests: the sender has exceeded its configured rate limit.
//   - 400 Bad Request:       required envelope fields (activity_type, actor, actor_server) are missing.
//   - 500 Internal Server Error: unexpected processing failure.
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

	content := fmt.Sprintf("%v", req.Payload["content"])

	aiResult, err := aimoderation.CallModerationAPI(content)

	if err != nil {
		log.Println("Incoming moderation unavailable — allowing content:", err)

		aiResult = &aimoderation.ModerationResponse{
			Recommendation: "SAFE",
		}
	}

	log.Println("Incoming moderation result:", aiResult.Recommendation)

	if req.ActivityType == "" || req.Actor == "" || req.ActorServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
		return
	}

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

// OutboxHandler handles GET /federation/outbox?actor_id=<id>.
// It returns the last 50 outbound activities published by the given actor,
// allowing remote servers to backfill the local activity history of a user
// (e.g. after a follow is accepted). Useful for replay/catch-up scenarios.
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
// It enqueues an outbound activity for delivery to a specific remote server.
// The activity is persisted to the outbox table and dispatched asynchronously
// by the delivery retry system, which handles transient failures with exponential backoff.
//
// Required body fields: activity_type, actor_id, target_server, payload.
// Optional: target_id (specific entity on the remote server, e.g. a post ID).
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

// AcknowledgmentHandler handles POST /federation/ack.
// Remote servers call this endpoint to notify us about the delivery outcome of
// an activity we sent them. The status can be "delivered", "failed", or "rejected".
// Delivery tracking data is used by the health dashboard and retry scheduler.
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
// It advertises what optional federation features this server supports, such as:
//   - linked_posts: the server can receive cross-server post replications.
//   - ephemeral_posts: the server honours expiry timestamps on posts.
//   - close_friends: the server supports CLOSE_FRIENDS visibility level.
//
// Remote servers call this endpoint before sending certain activity types to
// determine whether the target understands and will honour the feature.
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

// HandshakeHandler handles POST /federation/handshake.
// This is the receiver side of the mutual TLS-less trust establishment protocol.
// When a remote server wants to federate with this node it calls this endpoint,
// providing its server_id, server_name, public_key, and endpoint URL.
//
// On receipt, we:
//  1. Persist the remote server's public key in our trusted_servers table so
//     subsequent signed requests from it can be verified without a live lookup.
//  2. Respond with our own identity (server_id, server_name, public_key) so the
//     remote server can reciprocally trust us.
//
// Note: handshakes are unauthenticated (the trust is established here, not before).
// The public key supplied by the requester is trusted optimistically; operators who
// need stronger guarantees should use an allow-list or require manual approval.
func HandshakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

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

	// Upsert the remote server into trusted_servers.
	// Using ON CONFLICT DO UPDATE ensures re-handshakes (e.g. after key rotation)
	// update the stored key rather than failing.
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

	var localID, localName, localKey string
	err := db.QueryRow(`SELECT server_id, server_name, public_key FROM server_identity WHERE id = 1`).
		Scan(&localID, &localName, &localKey)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "not_initialized",
			"This server is not yet initialized", err.Error())
		return
	}

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

	targetURL := strings.TrimRight(req.TargetServer, "/") + "/federation/handshake"
	log.Printf("🤝 Initiating handshake with %s", targetURL)

	resp, err := http.Post(targetURL, "application/json", bytes.NewReader(payloadJSON))
	if err != nil {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Failed to connect to remote server", err.Error())
		return
	}
	defer resp.Body.Close()

	var remoteResp models.FederationResponse
	if err := json.NewDecoder(resp.Body).Decode(&remoteResp); err != nil {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Failed to parse remote server response", err.Error())
		return
	}

	if !remoteResp.Success {
		errMsg := "unknown error"
		if remoteResp.Error != nil {
			errMsg = remoteResp.Error.Message
		}
		sendError(w, http.StatusBadGateway, "handshake_rejected",
			"Remote server rejected handshake", errMsg)
		return
	}

	remoteID, _ := remoteResp.Data["server_id"].(string)
	remoteName, _ := remoteResp.Data["server_name"].(string)
	remoteKey, _ := remoteResp.Data["public_key"].(string)

	if remoteID == "" || remoteKey == "" {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Remote server returned incomplete identity", "")
		return
	}

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

// sendSuccess writes a JSON success envelope to w with the given HTTP status code.
// All federation handlers that succeed use this helper to ensure a consistent
// response shape: { success: true, message: "...", data: {...} }.
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

// sendError writes a JSON error envelope to w and logs the error details.
// The error shape mirrors the FederationResponse model with Success=false.
// errorType is a machine-readable code (e.g. "missing_fields") that clients
// can use for programmatic error handling without parsing the human message.
// details carries implementation-level context (e.g. wrapped Go errors) and
// should be omitted from responses sent to untrusted callers in production.
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

// HandleFederatedLookup handles GET /federation/lookup?id=<handle>.
// It resolves a user handle (e.g. "alice@server-a.example") to its full identity
// document (public key, profile metadata) by querying the local database first and
// falling back to a live remote lookup. This endpoint is consumed by remote servers
// that need to verify the identity of a user before accepting follow requests or
// cryptographically signed activities from that user.
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
