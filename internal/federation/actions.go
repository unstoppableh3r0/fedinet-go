package main

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
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
)






func SendFederatedActivity(activityID uuid.UUID, targetServer string, payload map[string]interface{}) error {
	
	blocked, err := IsServerBlocked(targetServer)
	if err != nil {
		return fmt.Errorf("failed to check block status: %w", err)
	}
	if blocked {
		return fmt.Errorf("server is blocked: %s", targetServer)
	}

	
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

	
	err = DeliverWithRetry(activityID, targetServer, payload, 1)
	if err != nil {
		
		return QueueForRetry(activityID, err.Error())
	}

	return nil
}


func DeliverWithRetry(messageID uuid.UUID, targetServer string, payload map[string]interface{}, attemptNumber int) error {
	
	message := models.FederationRequest{
		Version: "1.0.0",
		Type:    "activity",
		Sender:  "http://localhost:8081", 
		Payload: payload,
	}

	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	
	resp, err := http.Post(
		targetServer+"/federation/inbox",
		"application/json",
		bytes.NewBuffer(jsonData),
	)

	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delivery failed with status %d: %s", resp.StatusCode, string(body))
	}

	
	_, err = db.Exec(`
		UPDATE outbox_activities
		SET delivery_status = 'delivered', delivered_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, messageID)

	return err
}


func QueueForRetry(messageID uuid.UUID, errorMsg string) error {
	
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
	maxRetries := 6

	
	if nextAttempt > maxRetries {
		
		_, err = db.Exec(`
			UPDATE outbox_activities
			SET delivery_status = 'expired', error_message = $2, updated_at = NOW()
			WHERE id = $1
		`, messageID, fmt.Sprintf("Max retries exceeded: %s", errorMsg))
		return err
	}

	
	backoffSeconds := calculateBackoff(nextAttempt)
	nextRetryAt := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	
	_, err = db.Exec(`
		INSERT INTO delivery_attempts 
		(message_id, attempt_number, status, error_message, next_retry_at, backoff_seconds)
		VALUES ($1, $2, 'pending', $3, $4, $5)
	`, messageID, nextAttempt, errorMsg, nextRetryAt, backoffSeconds)

	return err
}


func calculateBackoff(attempt int) int {
	
	backoffs := []int{30, 60, 300, 900, 3600, 21600}
	if attempt <= len(backoffs) {
		return backoffs[attempt-1]
	}
	return 21600 
}


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


func ProcessRetryQueue() error {
	rows, err := db.Query(`
		SELECT da.message_id, oa.target_server, oa.payload, da.attempt_number
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

	for rows.Next() {
		var messageID uuid.UUID
		var targetServer, payloadStr string
		var attemptNumber int

		err := rows.Scan(&messageID, &targetServer, &payloadStr, &attemptNumber)
		if err != nil {
			log.Printf("Error scanning retry: %v", err)
			continue
		}

		
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			log.Printf("Error parsing payload: %v", err)
			continue
		}

		
		err = DeliverWithRetry(messageID, targetServer, payload, attemptNumber)

		if err != nil {
			
			QueueForRetry(messageID, err.Error())

			
			db.Exec(`
				UPDATE delivery_attempts
				SET status = 'failed', updated_at = NOW()
				WHERE message_id = $1 AND attempt_number = $2
			`, messageID, attemptNumber)
		} else {
			
			db.Exec(`
				UPDATE delivery_attempts
				SET status = 'success', updated_at = NOW()
				WHERE message_id = $1 AND attempt_number = $2
			`, messageID, attemptNumber)
		}
	}

	return rows.Err()
}






func ProcessInboundActivity(activityType, actorID, actorServer string, targetID *string, payload map[string]interface{}) (uuid.UUID, error) {
	
	blocked, err := IsServerBlocked(actorServer)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to check block status: %w", err)
	}
	if blocked {
		return uuid.Nil, fmt.Errorf("sender server is blocked")
	}

	
	allowed, err := CheckRateLimit(actorServer, "/federation/inbox")
	if err != nil {
		return uuid.Nil, fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return uuid.Nil, fmt.Errorf("rate limit exceeded")
	}

	
	if targetID != nil {
		blocked, err := IsUserBlocked(*targetID, actorID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to check user block status: %w", err)
		}
		if blocked {
			return uuid.Nil, fmt.Errorf("actor is blocked by target")
		}
	}

	
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

	
	go SendAcknowledgment(activityID, actorServer, "received", nil)

	
	
	
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

func DispatchActivity(activity *models.InboxActivity) {
	var err error
	switch activity.ActivityType {
	case "Update":
		err = HandleProfileUpdate(activity)
		
	}

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

	
	go func() {
		err := SendFederatedActivity(activityID, targetServer, payload)
		if err != nil {
			log.Printf("Failed to deliver activity %s: %v", activityID, err)
		}
	}()

	return activityID, nil
}


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






func CheckRateLimit(serverURL, endpoint string) (bool, error) {
	now := time.Now()

	
	var currentCount, requestsPerMin, burstAllowance int
	var windowStartedAt time.Time

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
		
		return true, nil
	}
	if err != nil {
		return false, err
	}

	
	if now.Sub(windowStartedAt) > time.Minute {
		
		_, err = db.Exec(`
			UPDATE rate_limits
			SET current_count = 1, window_started_at = NOW(), last_request_at = NOW(), updated_at = NOW()
			WHERE server_url = $1 AND endpoint = $2
		`, serverURL, endpoint)
		return true, err
	}

	
	if currentCount >= requestsPerMin+burstAllowance {
		return false, nil
	}

	
	return IncrementRateLimiter(serverURL, endpoint)
}


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






func AdvertiseCapabilities() (*models.ServerCapabilities, error) {
	protocolVersions, _ := json.Marshal([]string{"1.0.0"})
	supportedTypes, _ := json.Marshal([]string{"Follow", "Like", "Post", "Message"})
	rateLimitInfo, _ := json.Marshal(map[string]int{"requests_per_min": 100, "burst": 20})

	caps := &models.ServerCapabilities{
		ID:               uuid.New(),
		ServerURL:        "http://localhost:8081",
		ProtocolVersions: string(protocolVersions),
		SupportedTypes:   string(supportedTypes),
		MaxMessageSize:   1048576, 
		SupportsRetries:  true,
		SupportsAcks:     true,
		RateLimitInfo:    stringPtr(string(rateLimitInfo)),
		LastDiscoveredAt: time.Now(),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	return caps, nil
}


func DiscoverRemoteCapabilities(serverURL string) (*models.ServerCapabilities, error) {
	
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


func IsKnownServer(serverURL string) (bool, error) {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM server_capabilities WHERE server_url = $1)
	`, serverURL).Scan(&exists)

	return exists, err
}






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


func UnblockServer(serverURL string) error {
	_, err := db.Exec(`
		UPDATE blocked_servers
		SET is_active = false, updated_at = NOW()
		WHERE server_url = $1
	`, serverURL)

	return err
}


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






func GetFederationMode() (string, error) {
	var mode string
	err := db.QueryRow(`SELECT mode FROM federation_config ORDER BY created_at DESC LIMIT 1`).Scan(&mode)
	if err != nil {
		return "soft", err 
	}
	return mode, nil
}


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






func UpdateHealthMetrics() error {
	var totalMessages, successful, failed, pending int64
	var blockedCount int

	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities`).Scan(&totalMessages)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'delivered'`).Scan(&successful)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'failed' OR delivery_status = 'expired'`).Scan(&failed)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_activities WHERE delivery_status = 'pending'`).Scan(&pending)
	db.QueryRow(`SELECT COUNT(*) FROM blocked_servers WHERE is_active = true`).Scan(&blockedCount)

	
	var avgLatency float64
	db.QueryRow(`
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (delivered_at - created_at)) * 1000), 0)
		FROM outbox_activities
		WHERE delivered_at IS NOT NULL
		AND delivered_at > NOW() - INTERVAL '1 hour'
	`).Scan(&avgLatency)

	
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





func stringPtr(s string) *string {
	return &s
}


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
