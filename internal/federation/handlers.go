package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// ============================================================================
// User Story 2.4: Inbox / Outbox Architecture
// ============================================================================

// InboxHandler receives incoming federated activities
func InboxHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed", "")
		return
	}

	var req InboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON payload", err.Error())
		return
	}

	// Validate required fields
	if req.ActivityType == "" || req.Actor == "" || req.ActorServer == "" {
		sendError(w, http.StatusBadRequest, "missing_fields", "Missing required fields", "")
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

	var req AcknowledgmentRequest
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

	var req CapabilityRequest
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

	response := HealthResponse{
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
	var req BlockServerRequest
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
	var req FederationModeRequest
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

	var req RateLimitConfig
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

	response := FederationResponse{
		Success: true,
		Message: message,
		Data:    data,
	}

	json.NewEncoder(w).Encode(response)
}

func sendError(w http.ResponseWriter, statusCode int, errorType, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := FederationResponse{
		Success: false,
		Error: &ErrorResponse{
			Code:    statusCode,
			Type:    errorType,
			Message: message,
			Details: details,
		},
	}

	json.NewEncoder(w).Encode(response)

	log.Printf("Error [%s]: %s - %s", errorType, message, details)
}
