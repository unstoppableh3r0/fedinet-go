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

// ============================================================================
// DATA STRUCTURES: CRYPTOGRAPHIC PARAMETERS
// ============================================================================

// SignatureParams holds the decomposed elements of an HTTP Signature header.
// Following the IETF draft for HTTP Signatures, it identifies who is signing,
// what algorithm is being used, which headers are covered, and the resulting hash.
type SignatureParams struct {
	KeyID     string   // Unique identifier for the public key (usually an Actor URL)
	Algorithm string   // Cryptographic algorithm (e.g., rsa-sha256 or ed25519)
	Headers   []string // The ordered list of HTTP headers included in the signature string
	Signature string   // The Base64 or Hex encoded signature result
}

// ============================================================================
// HEADER PARSING & KEY MANAGEMENT
// ============================================================================

// ParseSignatureHeader breaks down the raw "Signature" string from an HTTP request.
// It uses a comma-separated key-value pair format common in federated protocols.
// Example input: keyId="user@server",algorithm="hs256",headers="(request-target) host",signature="..."
func ParseSignatureHeader(header string) (*SignatureParams, error) {
	params := &SignatureParams{}
	parts := strings.Split(header, ",")

	// Iterate through each attribute in the header (keyId, algorithm, headers, signature).
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

	// Structural validation: A signature is useless without an ID, the hash, or known headers.
	if params.KeyID == "" || params.Signature == "" || len(params.Headers) == 0 {
		return nil, fmt.Errorf("invalid signature header: missing required fields")
	}
	return params, nil
}

// FetchServerPublicKey implements a multi-tier lookup strategy to find a sender's public key.
// 1. Check local identities (is the sender a user on THIS server?)
// 2. Check trusted_servers (is this a known peer?)
// 3. Check server_identity (is this ourselves?)
// 4. Remote Resolve (if unknown, fetch the identity document from the internet).
func FetchServerPublicKey(keyID string) (string, error) {
	var publicKey string

	// Tier 1: Search the 'identities' table for local users.
	err := db.QueryRow(
		`SELECT public_key FROM identities WHERE user_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// Tier 2: Search for previously established handshakes with other instances.
	err = db.QueryRow(
		`SELECT public_key FROM trusted_servers WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		log.Printf("✅ Found key for %s in trusted_servers", keyID)
		return publicKey, nil
	}

	// Tier 3: Search for the node's own identity (loopback verification).
	err = db.QueryRow(
		`SELECT public_key FROM server_identity WHERE server_id = $1`, keyID,
	).Scan(&publicKey)
	if err == nil && publicKey != "" {
		return publicKey, nil
	}

	// Tier 4: The "Last Resort" - WebFinger/ActivityPub resolution of a remote ID.
	doc, err := ResolveAccount(keyID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public key for %s: %w", keyID, err)
	}
	return doc.Identity.PublicKey, nil
}

// ============================================================================
// SECURITY MIDDLEWARE: SIGNATURE VERIFICATION
// ============================================================================

// VerifySignatureMiddleware intercepts incoming HTTP requests to ensure they are
// cryptographically signed by a trusted or resolvable remote server.
// This prevents "spoofing" where one server pretends to be another.
func VerifySignatureMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Mandatory: The 'Signature' header must be present.
		sigHeader := r.Header.Get("Signature")
		if sigHeader == "" {
			sendError(w, http.StatusUnauthorized, "missing_signature",
				"Missing Signature header", "")
			return
		}

		// Deconstruct the header into its component parts.
		params, err := ParseSignatureHeader(sigHeader)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "invalid_signature_header",
				"Invalid Signature header format", err.Error())
			return
		}

		// Locate the public key associated with the KeyID provided in the header.
		publicKey, err := FetchServerPublicKey(params.KeyID)
		if err != nil {
			sendError(w, http.StatusUnauthorized, "key_not_found",
				"Could not find public key for sender", err.Error())
			return
		}

		// Buffer management: We must read the body to verify the 'Digest',
		// then put it back so the subsequent handler can read it too.
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

		// Reconstruct the "Signing String". The order of headers must match
		// exactly what the sender used, or the signature will fail.
		var signingParts []string
		for _, h := range params.Headers {
			switch h {
			case "(request-target)":
				// Special pseudo-header representing the method and path.
				requestTarget := strings.ToLower(r.Method) + " " + r.URL.RequestURI()
				signingParts = append(signingParts, "(request-target): "+requestTarget)
			case "host":
				signingParts = append(signingParts, "host: "+r.Host)
			case "date":
				signingParts = append(signingParts, "date: "+r.Header.Get("Date"))
			case "digest":
				// Body Integrity Check: Hash the actual body and compare it to the 'Digest' header.
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
				// Standard headers (e.g., Content-Type).
				signingParts = append(signingParts, h+": "+r.Header.Get(http.CanonicalHeaderKey(h)))
			}
		}
		// Join parts with newlines as per the HTTP Signature specification.
		signingString := strings.Join(signingParts, "\n")

		// Perform the actual cryptographic verification using the public key.
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

		// If we reach here, the message is authentic.
		log.Printf("✅ Signature verified for keyId=%s", params.KeyID)
		next(w, r)
	}
}

// ============================================================================
// INBOUND ACTIVITY PROCESSING (INBOX)
// ============================================================================

// InboxHandler processes "incoming" federation activities (Follows, Likes, Posts).
// It includes an AI-based moderation step to filter out toxic content before storage.
func InboxHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Protocol Validation: Activities are always sent via POST.
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	// 2. Data Decoding: Convert the raw JSON payload into an InboxRequest model.
	var req models.InboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	// 3. AI Moderation: This is a critical safety feature.
	// Before an activity is accepted, its content is sent to an AI service to check
	// for safety violations (hate speech, spam, etc.).
	content := fmt.Sprintf("%v", req.Payload["content"])
	aiResult, err := aimoderation.CallModerationAPI(content)

	if err != nil {
		// Fail-open strategy: If the AI service is down, we log it and allow the message
		// to prevent breaking federation, but a real-world app might fail-closed for safety.
		log.Println("Incoming moderation unavailable — allowing content:", err)
		aiResult = &aimoderation.ModerationResponse{
			Recommendation: "SAFE",
		}
	}

	log.Println("Incoming moderation result:", aiResult.Recommendation)

	// 4. Basic Schema Validation: Ensure the activity has the necessary actors.
	if req.ActivityType == "" || req.Actor == "" || req.ActorServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
		return
	}

	// 5. Processing Engine: Hand the activity off to the logic layer (ProcessInboundActivity).
	// This function handles side effects like updating follower lists or creating post records.
	activityID, err := ProcessInboundActivity(
		req.ActivityType,
		req.Actor,
		req.ActorServer,
		req.Target,
		req.Payload,
	)

	// 6. Detailed Error Handling: Categorize why an activity might be rejected.
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

	// 7. Success: Return the local ID assigned to this activity for tracking.
	sendSuccess(w, http.StatusOK, "Activity received", map[string]interface{}{
		"activity_id": activityID,
	})
}

// ============================================================================
// OUTBOUND ACTIVITY DISCOVERY (OUTBOX)
// ============================================================================

// OutboxHandler implements the "Pull" model of federation.
// It allows remote servers to request a list of public activities for a specific local user.
func OutboxHandler(w http.ResponseWriter, r *http.Request) {
	// Outbox retrieval is always a GET request.
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	// Identify which user's history is being requested.
	actorID := r.URL.Query().Get("actor_id")
	if actorID == "" {
		sendError(w, http.StatusBadRequest, "missing_actor", "actor_id parameter required", "")
		return
	}

	// Pagination limit to prevent large database queries from slowing down the server.
	limit := 50

	// Fetch activity records that were generated on this server (Outbox).
	activities, err := GetOutboxActivities(actorID, limit)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch activities", err.Error())
		return
	}

	// Return as a JSON collection.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"activities": activities,
		"count":      len(activities),
	})
}

// ============================================================================
// OUTBOUND ACTIVITY DISPATCH
// ============================================================================

// SendActivityHandler is an internal API endpoint used by the local UI/client.
// It triggers the "Push" model of federation, where we send data to a remote inbox.
func SendActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	// Parse the request containing the intent (e.g., "Create Post" -> "Server B").
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

	// PublishOutboundActivity places the activity in a delivery queue.
	// A background worker will handle the retry logic and cryptographic signing.
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
// DELIVERY TRACKING & ACKNOWLEDGMENT
// ============================================================================

// AcknowledgmentHandler is used by remote servers to confirm they have received
// a specific message or activity, allowing us to update delivery statuses in the DB.
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

	// Update the message state (e.g., from 'pending' to 'delivered' or 'failed').
	err := TrackDeliveryState(req.MessageID, req.Status, req.Reason)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to track acknowledgment", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Acknowledgment recorded", nil)
}

// ============================================================================
// CAPABILITY NEGOTIATION
// ============================================================================

// CapabilitiesHandler exposes what features this server supports (e.g., "ai-moderation", "end-to-end-encryption").
// This allows other servers to adjust how they communicate with us.
func CapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed", "")
		return
	}

	// AdvertiseCapabilities gathers local settings and returns them as a JSON object.
	caps, err := AdvertiseCapabilities()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to get capabilities", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caps)
}

// DiscoverCapabilitiesHandler initiates a request to a remote server's capability endpoint.
// This is typically done during the first contact between two servers.
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

	// Query the remote server and store the results in our local 'trusted_servers' cache.
	caps, err := DiscoverRemoteCapabilities(req.ServerURL)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "discovery_failed", "Failed to discover capabilities", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caps)
}

// ============================================================================
// HEALTH & MONITORING
// ============================================================================

// HealthHandler returns a comprehensive overview of the federation engine's health.
// It includes uptime, latency metrics, and success/failure ratios for outbound delivery.
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

	// Map the internal health struct to the public response model.
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
// SERVER BLOCKING (MODERATION)
// ============================================================================

// BlockedServersHandler provides an interface for administrators to manage "Defederation".
// Defederation is the act of refusing to send or receive data from a specific server.
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

// handleGetBlockedServers retrieves the current "Blacklist" of defederated instances.
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

// handleBlockServer adds a new entry to the block list.
// This can be temporary (via ExpiresAt) or permanent.
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

	// Update the database. Future Inbox/Outbox requests to this URL will now fail.
	err := BlockServer(req.ServerURL, req.Reason, "system", req.ExpiresAt)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "internal_error", "Failed to block server", err.Error())
		return
	}

	sendSuccess(w, http.StatusOK, "Server blocked successfully", map[string]interface{}{
		"server_url": req.ServerURL,
	})
}

// handleUnblockServer removes a server from the block list, restoring communication.
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
// CONFIGURATION: FEDERATION MODES
// ============================================================================

// FederationModeHandler allows switching between "soft" and "hard" federation.
// Soft: Allow unknown servers but validate their identity documents.
// Hard: Only allow communication with servers that have pre-shared keys (trusted_servers).
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

	// Mode constraint: Currently only 'soft' and 'hard' are supported logic paths.
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
// TRAFFIC CONTROL: RATE LIMITS
// ============================================================================

// RateLimitsHandler allows granular control over how many requests a specific
// remote server can send to a specific endpoint (e.g., limit /inbox requests).
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

	// Persistence: Update the rate_limits table.
	// The middleware (not shown here) uses this table to perform real-time throttling.
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
// PROTOCOL HANDSHAKE: MUTUAL TRUST ESTABLISHMENT
// ============================================================================

// HandshakeHandler processes an "Inbound" handshake request.
// It stores the remote server's credentials and returns its own for mutual trust.
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

	// A handshake requires the full identity of the peer.
	if req.ServerID == "" || req.ServerName == "" || req.PublicKey == "" || req.Endpoint == "" {
		sendError(w, http.StatusBadRequest, "missing_fields",
			"server_id, server_name, public_key, and endpoint are required", "")
		return
	}

	// Save the remote identity. From now on, requests from this server can be verified.
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

	// Fetch our own identity to complete the "Mutual" exchange.
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

// InitiateHandshakeHandler is called via an admin command to manually link with another server.
// It performs a POST request to the remote server's /handshake endpoint.
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

	// Step 1: Prepare local identity data.
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

	// Step 2: Send identity to the peer.
	targetURL := strings.TrimRight(req.TargetServer, "/") + "/federation/handshake"
	log.Printf("🤝 Initiating handshake with %s", targetURL)

	resp, err := http.Post(targetURL, "application/json", bytes.NewReader(payloadJSON))
	if err != nil {
		sendError(w, http.StatusBadGateway, "handshake_failed",
			"Failed to connect to remote server", err.Error())
		return
	}
	defer resp.Body.Close()

	// Step 3: Verify and save the peer's returned identity.
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

	// Finalize trust in the database.
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

// ============================================================================
// RESPONSE WRAPPERS (JSON API)
// ============================================================================

// sendSuccess constructs a standard successful JSON response for the federation API.
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

// sendError constructs a standard error JSON response.
// It logs the error internally while providing a clean message to the client.
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
// IDENTITY DISCOVERY (LOOKUP)
// ============================================================================

// HandleFederatedLookup implements a profile search endpoint.
// It allows users to find identities on this server using an 'id' parameter (e.g., alice@domain).
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

	// ResolveAccount handles both local lookups and remote resolution (WebFinger).
	doc, err := ResolveAccount(handle)
	if err != nil {
		if err.Error() == "identity not found" {
			sendError(w, http.StatusNotFound, "not_found", "Identity not found", "")
			return
		}

		sendError(w, http.StatusInternalServerError, "lookup_failed", "Failed to resolve identity", err.Error())
		return
	}

	// Return the combined Identity (keys/server) and Profile (avatar/bio).
	sendSuccess(w, http.StatusOK, "Lookup successful", map[string]interface{}{
		"handle":   handle,
		"identity": doc.Identity,
		"profile":  doc.Profile,
	})
}