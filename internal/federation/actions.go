package federation

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ============================================================================
// OUTBOUND MESSAGE ROUTING & POLICY ENFORCEMENT
// ============================================================================

// SendFederatedActivity is the primary entry point for sending data to a remote server.
// It performs a "Pre-Flight" check to ensure the target server is neither blocked
// nor restricted by the current federation mode (Soft vs Hard).
func SendFederatedActivity(activityID uuid.UUID, targetServer string, activityType, actorID string, targetID *string, payload map[string]interface{}) error {

	// 1. SAFETY CHECK: Defederation Check
	// Before any bytes leave the network, we verify the target isn't on our global blacklist.
	blocked, err := IsServerBlocked(targetServer)
	if err != nil {
		return fmt.Errorf("failed to check block status: %w", err)
	}
	if blocked {
		return fmt.Errorf("server is blocked: %s", targetServer)
	}

	// 2. POLICY CHECK: Federation Mode
	// 'Hard' mode requires that we have previously performed a handshake or
	// capability discovery with the server. 'Soft' mode allows opportunistic sending.
	mode, err := GetFederationMode()
	if err != nil {
		return fmt.Errorf("failed to get federation mode: %w", err)
	}

	if mode == "hard" {
		known, err := IsKnownServer(targetServer)
		if err != nil {
			return fmt.Errorf("failed to check server knowledge: %w", err)
		}
		if !known {
			return fmt.Errorf("unknown server in hard mode: %s", targetServer)
		}
	}

	// 3. ATTEMPT DELIVERY
	// Initial attempt is immediate. If it fails, the error is caught and
	// the message is moved to the retry queue for exponential backoff.
	err = DeliverWithRetry(activityID, targetServer, activityType, actorID, targetID, payload, 1)
	if err != nil {
		// Log the failure reason and schedule a future retry based on attempt number.
		return QueueForRetry(activityID, err.Error())
	}

	return nil
}

// ============================================================================
// NETWORK DELIVERY LAYER
// ============================================================================

// DeliverWithRetry performs the actual HTTP POST to the remote server's inbox.
// It transforms internal models into the format expected by the recipient
// and updates the local database state upon success.
func DeliverWithRetry(messageID uuid.UUID, targetServer, activityType, actorID string, targetID *string, payload map[string]interface{}, attemptNumber int) error {

	// 1. PAYLOAD DECORATION
	// We inject origin-server metadata to help the recipient trace the moderation history.
	if payload != nil {
		payload["moderation"] = map[string]interface{}{
			"origin_server": true,
			"moderated_at":  time.Now().UTC(),
		}
	}
	log.Println("Outgoing federation payload:", payload)

	// 2. PROTOCOL MAPPING
	// Mapping internal activity data to an 'InboxRequest' which is the standard
	// envelope for federated messaging in this architecture.
	message := models.InboxRequest{
		ActivityType: activityType,
		Actor:        actorID,
		ActorServer:  "http://localhost:8081", // Static placeholder for the local server's public URL
		Target:       targetID,
		Payload:      payload,
	}

	// 3. SERIALIZATION
	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// 4. REQUEST CONSTRUCTION
	// All federated activities are delivered to the /federation/inbox endpoint.
	req, err := http.NewRequest("POST", targetServer+"/federation/inbox", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 5. EXECUTION
	// Use a 10-second timeout. Federated servers may be slow, but we shouldn't
	// exhaust our own thread pool waiting indefinitely.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// 6. RESPONSE VERIFICATION
	// We accept 200 OK or 201 Created as valid delivery confirmations.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delivery failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 7. PERSISTENCE
	// Update the activity record to reflect a successful "Delivered" state.
	_, err = db.Exec(`
        UPDATE outbox_activities
        SET delivery_status = 'delivered', delivered_at = NOW(), updated_at = NOW()
        WHERE id = $1
    `, messageID)

	return err
}

// ============================================================================
// RELIABILITY & RETRY LOGIC
// ============================================================================

// QueueForRetry manages the lifecycle of a failing delivery attempt.
// It calculates the next retry window and marks a message as "Expired"
// if the maximum number of retries is exceeded.
func QueueForRetry(messageID uuid.UUID, errorMsg string) error {

	// 1. LOOKUP PREVIOUS ATTEMPTS
	var attempts int
	err := db.QueryRow(`
        SELECT COALESCE(MAX(attempt_number), 0)
        FROM delivery_attempts
        WHERE message_id = $1
    `, messageID).Scan(&attempts)

	if err != nil && err != sql.ErrNoRows {
		return err
	}

	nextAttempt := attempts + 1
	maxRetries := 6 // Approximately 24 hours of retry time total

	// 2. FAILURE TERMINATION
	// If we've exhausted all 6 retries, we stop trying to avoid wasting resources
	// on a dead server or invalid endpoint.
	if nextAttempt > maxRetries {
		_, err = db.Exec(`
            UPDATE outbox_activities
            SET delivery_status = 'expired', error_message = $2, updated_at = NOW()
            WHERE id = $1
        `, messageID, fmt.Sprintf("Max retries exceeded: %s", errorMsg))
		return err
	}

	// 3. SCHEDULE NEXT ATTEMPT
	// Calculate backoff (e.g., 30s, 60s, 5m, 15m, 1h, 6h).
	backoffSeconds := calculateBackoff(nextAttempt)
	nextRetryAt := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	// 4. LOG ATTEMPT
	// Record why the last attempt failed for administrative auditing.
	_, err = db.Exec(`
        INSERT INTO delivery_attempts
        (message_id, attempt_number, status, error_message, next_retry_at, backoff_seconds)
        VALUES ($1, $2, 'pending', $3, $4, $5)
    `, messageID, nextAttempt, errorMsg, nextRetryAt, backoffSeconds)

	return err
}

// calculateBackoff provides the delays in seconds for the retry state machine.
func calculateBackoff(attempt int) int {
	// A predefined sequence of increasing delays to provide the remote server
	// time to recover from downtime or high load.
	backoffs := []int{30, 60, 300, 900, 3600, 21600}
	if attempt <= len(backoffs) {
		return backoffs[attempt-1]
	}
	return 21600 // Cap the backoff at 6 hours
}

// ExpireOldMessages cleans up the outbox by moving stuck "pending" messages
// to an "expired" state if they are older than 24 hours.
func ExpireOldMessages() error {
	expirationTime := time.Now().Add(-24 * time.Hour)

	_, err := db.Exec(`
        UPDATE outbox_activities
        SET delivery_status = 'expired', updated_at = NOW()
        WHERE delivery_status = 'pending'
        AND created_at < $1
    `, expirationTime)

	return err
}

// ProcessRetryQueue scans the database for delivery attempts that are due.
// This should be run by a background cron job or a persistent worker loop.
func ProcessRetryQueue() error {
	// 1. FETCH DUE TASKS
	// Limit to 100 per batch to prevent memory spikes if the queue is large.
	rows, err := db.Query(`
        SELECT da.message_id, oa.target_server, oa.activity_type, oa.actor_id, oa.target_id, oa.payload, da.attempt_number
        FROM delivery_attempts da
        JOIN outbox_activities oa ON da.message_id = oa.id
        WHERE da.status = 'pending'
        AND da.next_retry_at <= NOW()
        ORDER BY da.next_retry_at
        LIMIT 100
	`)

	if err != nil {
		return err
	}
	defer rows.Close()

	// 2. ITERATE AND EXECUTE
	for rows.Next() {
		var messageID uuid.UUID
		var targetServer, activityType, actorID, payloadStr string
		var targetID *string
		var attemptNumber int

		err := rows.Scan(&messageID, &targetServer, &activityType, &actorID, &targetID, &payloadStr, &attemptNumber)
		if err != nil {
			log.Printf("Error scanning retry: %v", err)
			continue
		}

		// Re-marshal the payload back into a map for the Deliver function.
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			log.Printf("Error parsing payload: %v", err)
			continue
		}

		// Trigger the delivery logic.
		err = DeliverWithRetry(messageID, targetServer, activityType, actorID, targetID, payload, attemptNumber)

		// 3. RECORD RESULTS
		if err != nil {
			// If it still fails, increment attempt number and schedule next.
			QueueForRetry(messageID, err.Error())
			db.Exec(`
                UPDATE delivery_attempts
                SET status = 'failed', updated_at = NOW()
                WHERE message_id = $1 AND attempt_number = $2
            `, messageID, attemptNumber)
		} else {
			// Mark as successful.
			db.Exec(`
                UPDATE delivery_attempts
                SET status = 'success', updated_at = NOW()
                WHERE message_id = $1 AND attempt_number = $2
            `, messageID, attemptNumber)
		}
	}

	return rows.Err()
}

// ============================================================================
// INBOUND ACTIVITY PROCESSING
// ============================================================================

// ProcessInboundActivity handles activities arriving from foreign servers.
// It enforces rate limits, defederation blocks, and individual user block lists.
func ProcessInboundActivity(activityType, actorID, actorServer string, targetID *string, payload map[string]interface{}) (uuid.UUID, error) {

	// 1. SERVER-LEVEL SECURITY
	// Is the sending server banned globally?
	blocked, err := IsServerBlocked(actorServer)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to check block status: %w", err)
	}
	if blocked {
		return uuid.Nil, fmt.Errorf("sender server is blocked")
	}

	// 2. TRAFFIC CONTROL
	// Protect our API from spam or DDoS by checking server-specific rate limits.
	allowed, err := CheckRateLimit(actorServer, "/federation/inbox")
	if err != nil {
		return uuid.Nil, fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return uuid.Nil, fmt.Errorf("rate limit exceeded")
	}

	// 3. USER-LEVEL PRIVACY
	// If the activity targets a specific local user (like a DM or Follow),
	// check if that user has blocked the remote actor.
	if targetID != nil {
		blocked, err := IsUserBlocked(*targetID, actorID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to check user block status: %w", err)
		}
		if blocked {
			return uuid.Nil, fmt.Errorf("actor is blocked by target")
		}
	}

	// 4. STORAGE
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var activityID uuid.UUID
	err = db.QueryRow(`
        INSERT INTO inbox_activities
        (activity_type, actor_id, actor_server, target_id, payload, status)
        VALUES ($1, $2, $3, $4, $5, 'received')
        RETURNING id
    `, activityType, actorID, actorServer, targetID, string(payloadJSON)).Scan(&activityID)

	if err != nil {
		return uuid.Nil, err
	}

	// 5. ASYNCHRONOUS COMPLETION
	// Immediately acknowledge receipt to the sender so they can stop retrying.
	go SendAcknowledgment(activityID, actorServer, "received", nil)

	// Dispatch to internal handlers (Profile updates, Message delivery, etc.).
	go func() {
		activity := models.InboxActivity{
			ID:           activityID,
			ActivityType: activityType,
			ActorID:      actorID,
			ActorServer:  actorServer,
			TargetID:     targetID,
			Payload:      string(payloadJSON),
		}
		DispatchActivity(&activity)
	}()

	return activityID, nil
}

// DispatchActivity routes the received activity to the appropriate handler
// based on the ActivityType (Follow, Update, Message, etc.).
func DispatchActivity(activity *models.InboxActivity) {
	var err error
	switch activity.ActivityType {
	case "Update":
		err = HandleProfileUpdate(activity)
	case "Message":
		log.Printf("📩 Received Direct Message from %s: %s", activity.ActorID, activity.Payload)
	case "Follow":
		log.Printf("👤 Received Follow request from %s", activity.ActorID)
	}

	// Update the database with the processing outcome.
	status := "processed"
	var errMsg *string
	if err != nil {
		status = "failed"
		msg := err.Error()
		errMsg = &msg
		log.Printf("Failed to process activity %s: %v", activity.ID, err)
	}

	db.Exec(`UPDATE inbox_activities SET status=$1, error_message=$2, processed_at=NOW() WHERE id=$3`,
		status, errMsg, activity.ID)
}

// ============================================================================
// DATA PERSISTENCE & QUEUING
// ============================================================================

// PublishOutboundActivity stages an activity in the outbox table and
// triggers an immediate delivery attempt in a separate goroutine.
func PublishOutboundActivity(activityType, actorID, targetServer string, targetID *string, payload map[string]interface{}) (uuid.UUID, error) {

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var activityID uuid.UUID
	err = db.QueryRow(`
        INSERT INTO outbox_activities
        (activity_type, actor_id, target_server, target_id, payload, delivery_status)
        VALUES ($1, $2, $3, $4, $5, 'pending')
        RETURNING id
    `, activityType, actorID, targetServer, targetID, string(payloadJSON)).Scan(&activityID)

	if err != nil {
		return uuid.Nil, err
	}

	// Fire-and-forget delivery attempt.
	go func() {
		err := SendFederatedActivity(activityID, targetServer, activityType, actorID, targetID, payload)
		if err != nil {
			log.Printf("Failed to deliver activity %s: %v", activityID, err)
		}
	}()

	return activityID, nil
}

// GetInboxActivities retrieves received activities for display or audit purposes.
func GetInboxActivities(targetID string, limit int) ([]models.InboxActivity, error) {
	rows, err := db.Query(`
        SELECT id, activity_type, actor_id, actor_server, target_id, payload,
               received_at, processed_at, processed_by, status, error_message, created_at
        FROM inbox_activities
        WHERE target_id = $1 OR target_id IS NULL
        ORDER BY received_at DESC
        LIMIT $2
    `, targetID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []models.InboxActivity
	for rows.Next() {
		var a models.InboxActivity
		err := rows.Scan(
			&a.ID, &a.ActivityType, &a.ActorID, &a.ActorServer, &a.TargetID,
			&a.Payload, &a.ReceivedAt, &a.ProcessedAt, &a.ProcessedBy,
			&a.Status, &a.ErrorMessage, &a.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}

	return activities, rows.Err()
}

// GetOutboxActivities retrieves sent activities for history tracking.
func GetOutboxActivities(actorID string, limit int) ([]models.OutboxActivity, error) {
	rows, err := db.Query(`
        SELECT id, activity_type, actor_id, target_server, target_id, payload,
               delivery_status, delivered_at, acknowledged_at, error_message,
               created_at, updated_at
        FROM outbox_activities
        WHERE actor_id = $1
        ORDER BY created_at DESC
        LIMIT $2
    `, actorID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []models.OutboxActivity
	for rows.Next() {
		var a models.OutboxActivity
		err := rows.Scan(
			&a.ID, &a.ActivityType, &a.ActorID, &a.TargetServer, &a.TargetID,
			&a.Payload, &a.DeliveryStatus, &a.DeliveredAt, &a.AcknowledgedAt,
			&a.ErrorMessage, &a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}

	return activities, rows.Err()
}

// ============================================================================
// HANDSHAKE & ACKNOWLEDGMENT
// ============================================================================

// SendAcknowledgment notifies the remote server that their message was
// successfully accepted into our local pipeline.
func SendAcknowledgment(messageID uuid.UUID, receiverServer string, status string, reason *string) error {
	ack := models.AcknowledgmentRequest{
		MessageID: messageID,
		Status:    status,
		Reason:    reason,
	}

	jsonData, err := json.Marshal(ack)
	if err != nil {
		return err
	}

	resp, err := http.Post(
		receiverServer+"/federation/ack",
		"application/json",
		bytes.NewBuffer(jsonData),
	)

	if err != nil {
		log.Printf("Failed to send acknowledgment: %v", err)
		return err
	}
	defer resp.Body.Close()

	return nil
}

// TrackDeliveryState records an inbound acknowledgment from a remote server
// to confirm they have processed one of our outbound activities.
func TrackDeliveryState(messageID uuid.UUID, status string, reason *string) error {
	_, err := db.Exec(`
        INSERT INTO delivery_acknowledgments
        (message_id, sender_server, receiver_server, status, reason)
        VALUES ($1, 'http://localhost:8081', 'remote', $2, $3)
    `, messageID, status, reason)

	if status == "processed" {
		db.Exec(`
            UPDATE outbox_activities
            SET acknowledged_at = NOW(), updated_at = NOW()
            WHERE id = $1
        `, messageID)
	}

	return err
}

// ============================================================================
// TRAFFIC CONTROL (RATE LIMITING)
// ============================================================================

// CheckRateLimit implements a "Sliding Window" or "Fixed Window" rate limiter
// to protect the inbox from being overwhelmed by a single remote server.
func CheckRateLimit(serverURL, endpoint string) (bool, error) {
	now := time.Now()

	var currentCount, requestsPerMin, burstAllowance int
	var windowStartedAt time.Time

	// Find the most specific rate limit rule (Endpoint -> Server -> Global).
	err := db.QueryRow(`
        SELECT current_count, requests_per_min, burst_allowance, window_started_at
        FROM rate_limits
        WHERE (server_url = $1 AND endpoint = $2)
           OR (server_url = $1 AND endpoint = '*')
           OR (server_url = '*' AND endpoint = '*')
        ORDER BY
            CASE WHEN server_url = $1 AND endpoint = $2 THEN 1
                 WHEN server_url = $1 AND endpoint = '*' THEN 2
                 ELSE 3 END
        LIMIT 1
    `, serverURL, endpoint).Scan(&currentCount, &requestsPerMin, &burstAllowance, &windowStartedAt)

	if err == sql.ErrNoRows {
		// No limits defined means unrestricted access.
		return true, nil
	}
	if err != nil {
		return false, err
	}

	// Reset window if it's been more than a minute.
	if now.Sub(windowStartedAt) > time.Minute {
		_, err = db.Exec(`
            UPDATE rate_limits
            SET current_count = 1, window_started_at = NOW(), last_request_at = NOW(), updated_at = NOW()
            WHERE server_url = $1 AND endpoint = $2
        `, serverURL, endpoint)
		return true, err
	}

	// Reject if over quota + burst capacity.
	if currentCount >= requestsPerMin+burstAllowance {
		return false, nil
	}

	return IncrementRateLimiter(serverURL, endpoint)
}

// IncrementRateLimiter simple atomic increment for the traffic counter.
func IncrementRateLimiter(serverURL, endpoint string) (bool, error) {
	_, err := db.Exec(`
        UPDATE rate_limits
        SET current_count = current_count + 1, last_request_at = NOW(), updated_at = NOW()
        WHERE server_url = $1 AND endpoint = $2
    `, serverURL, endpoint)

	if err != nil {
		return false, err
	}

	return true, nil
}

// GetRateLimitForServer diagnostic function to view current server quotas.
func GetRateLimitForServer(serverURL, endpoint string) (*models.RateLimit, error) {
	var rl models.RateLimit

	err := db.QueryRow(`
        SELECT id, server_url, endpoint, requests_per_min, burst_allowance,
               current_count, window_started_at, last_request_at, created_at, updated_at
        FROM rate_limits
        WHERE server_url = $1 AND endpoint = $2
    `, serverURL, endpoint).Scan(
		&rl.ID, &rl.ServerURL, &rl.Endpoint, &rl.RequestsPerMin,
		&rl.BurstAllowance, &rl.CurrentCount, &rl.WindowStartedAt,
		&rl.LastRequestAt, &rl.CreatedAt, &rl.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &rl, nil
}

// ============================================================================
// CAPABILITY DISCOVERY & NEGOTIATION
// ============================================================================

// AdvertiseCapabilities returns what this node supports.
// Other servers use this to see if we support specific encryption or activity types.
func AdvertiseCapabilities() (*models.ServerCapabilities, error) {
	protocolVersions, _ := json.Marshal([]string{"1.0.0"})
	supportedTypes, _ := json.Marshal([]string{"Follow", "Like", "Post", "Message"})
	rateLimitInfo, _ := json.Marshal(map[string]int{"requests_per_min": 100, "burst": 20})

	caps := &models.ServerCapabilities{
		ID:               uuid.New(),
		ServerURL:        "http://localhost:8081",
		ProtocolVersions: string(protocolVersions),
		SupportedTypes:   string(supportedTypes),
		MaxMessageSize:   1048576, // 1MB
		SupportsRetries:  true,
		SupportsAcks:     true,
		RateLimitInfo:    stringPtr(string(rateLimitInfo)),
		LastDiscoveredAt: time.Now(),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	return caps, nil
}

// DiscoverRemoteCapabilities fetches the feature list from a remote server.
// It uses a local cache (1 hour) to avoid unnecessary network overhead.
func DiscoverRemoteCapabilities(serverURL string) (*models.ServerCapabilities, error) {

	// 1. CACHE LOOKUP
	var caps models.ServerCapabilities
	err := db.QueryRow(`
        SELECT id, server_url, protocol_versions, supported_types, max_message_size,
               supports_retries, supports_acks, rate_limit_info, custom_features,
               last_discovered_at, created_at, updated_at
        FROM server_capabilities
        WHERE server_url = $1
        AND last_discovered_at > NOW() - INTERVAL '1 hour'
    `, serverURL).Scan(
		&caps.ID, &caps.ServerURL, &caps.ProtocolVersions, &caps.SupportedTypes,
		&caps.MaxMessageSize, &caps.SupportsRetries, &caps.SupportsAcks,
		&caps.RateLimitInfo, &caps.CustomFeatures, &caps.LastDiscoveredAt,
		&caps.CreatedAt, &caps.UpdatedAt,
	)

	if err == nil {
		return &caps, nil
	}

	// 2. REMOTE FETCH
	resp, err := http.Get(serverURL + "/federation/capabilities")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch capabilities: status %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(&caps)
	if err != nil {
		return nil, err
	}

	// 3. CACHE UPDATE
	_, err = db.Exec(`
        INSERT INTO server_capabilities
        (server_url, protocol_versions, supported_types, max_message_size,
         supports_retries, supports_acks, rate_limit_info, custom_features, last_discovered_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
        ON CONFLICT (server_url) DO UPDATE SET
            protocol_versions = $2,
            supported_types = $3,
            max_message_size = $4,
            supports_retries = $5,
            supports_acks = $6,
            rate_limit_info = $7,
            custom_features = $8,
            last_discovered_at = NOW(),
            updated_at = NOW()
    `, caps.ServerURL, caps.ProtocolVersions, caps.SupportedTypes, caps.MaxMessageSize,
		caps.SupportsRetries, caps.SupportsAcks, caps.RateLimitInfo, caps.CustomFeatures)

	return &caps, err
}

// ============================================================================
// MODERATION & HEALTH TOOLS
// ============================================================================

// IsKnownServer checks if we've ever successfully communicated with this server.
func IsKnownServer(serverURL string) (bool, error) {
	var exists bool
	err := db.QueryRow(`
        SELECT EXISTS(SELECT 1 FROM server_capabilities WHERE server_url = $1)
    `, serverURL).Scan(&exists)

	return exists, err
}

// IsServerBlocked checks the active defederation list.
func IsServerBlocked(serverURL string) (bool, error) {
	var blocked bool
	err := db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM blocked_servers
            WHERE server_url = $1
            AND is_active = true
            AND (expires_at IS NULL OR expires_at > NOW())
        )
    `, serverURL).Scan(&blocked)

	return blocked, err
}

// BlockServer bans an instance from sending/receiving data.
func BlockServer(serverURL, reason, blockedBy string, expiresAt *time.Time) error {
	_, err := db.Exec(`
        INSERT INTO blocked_servers (server_url, reason, blocked_by, expires_at, is_active)
        VALUES ($1, $2, $3, $4, true)
        ON CONFLICT (server_url) DO UPDATE SET
            reason = $2,
            blocked_by = $3,
            expires_at = $4,
            is_active = true,
            blocked_at = NOW(),
            updated_at = NOW()
    `, serverURL, reason, blockedBy, expiresAt)

	return err
}

// UnblockServer restores trust with an instance.
func UnblockServer(serverURL string) error {
	_, err := db.Exec(`
        UPDATE blocked_servers
        SET is_active = false, updated_at = NOW()
        WHERE server_url = $1
    `, serverURL)

	return err
}

// GetBlockedServers returns all currently restricted servers.
func GetBlockedServers() ([]models.FederationBlockedServer, error) {
	rows, err := db.Query(`
        SELECT id, server_url, reason, blocked_by, blocked_at, expires_at, is_active, created_at, updated_at
        FROM blocked_servers
        WHERE is_active = true
        ORDER BY blocked_at DESC
    `)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []models.FederationBlockedServer
	for rows.Next() {
		var s models.FederationBlockedServer
		err := rows.Scan(&s.ID, &s.ServerURL, &s.Reason, &s.BlockedBy, &s.BlockedAt,
			&s.ExpiresAt, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}

	return servers, rows.Err()
}

// GetFederationMode retrieves the current network policy (Soft/Hard).
func GetFederationMode() (string, error) {
	var mode string
	err := db.QueryRow(`SELECT mode FROM federation_config ORDER BY created_at DESC LIMIT 1`).Scan(&mode)
	if err != nil {
		return "soft", err
	}
	return mode, nil
}

// SetFederationMode updates the global instance policy settings.
func SetFederationMode(mode string, allowUnknown, requireCapNeg, strictValid *bool) error {
	query := `UPDATE federation_config SET mode = $1, updated_at = NOW()`
	args := []interface{}{mode}
	argCount := 2

	if allowUnknown != nil {
		query += fmt.Sprintf(", allow_unknown_servers = $%d", argCount)
		args = append(args, *allowUnknown)
		argCount++
	}
	if requireCapNeg != nil {
		query += fmt.Sprintf(", require_capability_neg = $%d", argCount)
		args = append(args, *requireCapNeg)
		argCount++
	}
	if strictValid != nil {
		query += fmt.Sprintf(", strict_validation = $%d", argCount)
		args = append(args, *strictValid)
	}

	_, err := db.Exec(query, args...)
	return err
}

// GetFederationConfig retrieves the full configuration object for the admin dashboard.
func GetFederationConfig() (*models.FederationConfig, error) {
	var config models.FederationConfig
	err := db.QueryRow(`
        SELECT id, mode, allow_unknown_servers, require_capability_neg, strict_validation,
               log_unknown_servers, auto_block_malicious, created_at, updated_at
        FROM federation_config
        ORDER BY created_at DESC
        LIMIT 1
    `).Scan(
		&config.ID, &config.Mode, &config.AllowUnknownServers, &config.RequireCapabilityNeg,
		&config.StrictValidation, &config.LogUnknownServers, &config.AutoBlockMalicious,
		&config.CreatedAt, &config.UpdatedAt,
	)

	return &config, err
}

// UpdateHealthMetrics aggregates performance data and calculates instance status.
func UpdateHealthMetrics() error {
	var totalMessages, successful, failed, pending int64
	var blockedCount int

	// Tallying stats from the outbox for delivery success rates.
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities`).Scan(&totalMessages)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'delivered'`).Scan(&successful)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'failed' OR delivery_status = 'expired'`).Scan(&failed)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'pending'`).Scan(&pending)
	db.QueryRow(`SELECT COUNT(*) FROM blocked_servers WHERE is_active = true`).Scan(&blockedCount)

	// Calculate average network latency for delivered activities over the last hour.
	var avgLatency float64
	db.QueryRow(`
        SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (delivered_at - created_at)) * 1000), 0)
        FROM outbox_activities
        WHERE delivered_at IS NOT NULL
        AND delivered_at > NOW() - INTERVAL '1 hour'
    `).Scan(&avgLatency)

	// Determine status: If more than 50% of messages are failing, mark as 'unhealthy'.
	status := "healthy"
	failureRate := float64(0)
	if totalMessages > 0 {
		failureRate = float64(failed) / float64(totalMessages)
	}
	if failureRate > 0.5 {
		status = "unhealthy"
	} else if failureRate > 0.2 {
		status = "degraded"
	}

	_, err := db.Exec(`
        UPDATE instance_health SET
            status = $1,
            total_messages = $2,
            successful_deliveries = $3,
            failed_deliveries = $4,
            pending_retries = $5,
            average_latency_ms = $6,
            blocked_servers_count = $7,
            last_health_check_at = NOW(),
            updated_at = NOW()
        WHERE id = (SELECT id FROM instance_health ORDER BY created_at LIMIT 1)
    `, status, totalMessages, successful, failed, pending, int(math.Round(avgLatency)), blockedCount)

	return err
}

// GetHealthStatus returns the current instance health summary for monitoring.
func GetHealthStatus() (*models.InstanceHealth, error) {
	var health models.InstanceHealth

	err := db.QueryRow(`
        SELECT id, status, total_messages, successful_deliveries, failed_deliveries,
               pending_retries, average_latency_ms, active_connections, blocked_servers_count,
               rate_limit_violations, uptime_seconds, last_health_check_at, created_at, updated_at
        FROM instance_health
        ORDER BY created_at
        LIMIT 1
    `).Scan(
		&health.ID, &health.Status, &health.TotalMessages, &health.SuccessfulDeliveries,
		&health.FailedDeliveries, &health.PendingRetries, &health.AverageLatencyMs,
		&health.ActiveConnections, &health.BlockedServersCount, &health.RateLimitViolations,
		&health.UptimeSeconds, &health.LastHealthCheckAt, &health.CreatedAt, &health.UpdatedAt,
	)

	return &health, err
}

// stringPtr is a convenience helper for creating string pointers.
func stringPtr(s string) *string {
	return &s
}

// IsUserBlocked check if a local user has filtered out a specific remote actor.
func IsUserBlocked(blockerID, blockedID string) (bool, error) {
	var blocked bool
	err := db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM block_events
            WHERE blocker_id = $1 AND blocked_id = $2
        )
    `, blockerID, blockedID).Scan(&blocked)
	return blocked, err
}
