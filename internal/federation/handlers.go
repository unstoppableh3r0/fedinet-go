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

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

type SignatureParams struct {
	KeyID     string
	Algorithm string
	Headers   []string
	Signature string
}

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
func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func GetReportsHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	rows, err := db.Query("SELECT id, activity_id, toxicity_score, status FROM moderation_logs WHERE status = 'processed'")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var reports []map[string]interface{}
	for rows.Next() {
		var id, activityID, status string
		var score float64
		// Scanning directly from database columns
		if err := rows.Scan(&id, &activityID, &score, &status); err != nil {
			continue
		}
		reports = append(reports, map[string]interface{}{
			"id":          id,
			"activity_id": activityID,
			"score":       score,
			"status":      status,
		})
	}
	json.NewEncoder(w).Encode(reports)
}

func ResolveReportHandler(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow POST for resolution
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID (e.g., /reports/{id}/resolve)
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	reportID := parts[len(parts)-2]

	// Update status in DB
	_, err := db.Exec("UPDATE moderation_logs SET status = 'resolved' WHERE id = $1", reportID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Resolved"})
}
