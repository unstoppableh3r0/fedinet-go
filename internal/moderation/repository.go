package moderation

import (
	"database/sql"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

type Repository interface {
	CreateReport(*models.Report) error
	ListPendingReports() ([]models.Report, error)
	GetReportByID(int64) (*models.Report, error)
	ResolveReport(int64, string) error

	BlockServer(*models.BlockedServer) error
	IsServerBlocked(string) (bool, error)
	ListBlockedServers() ([]models.BlockedServer, error)

	// User-level blocking
	BlockUser(*models.UserBlock) error
	UnblockUser(blockerUserID, blockedUserID string) error
	IsUserBlocked(blockerUserID, blockedUserID string) (bool, error)
	ListBlockedUsers(blockerUserID string) ([]models.UserBlock, error)

	EnqueueFederationEvent(*models.FederationEvent) error
	ListPendingFederationEvents() ([]models.FederationEvent, error)
	IncrementFederationRetry(int64) error
	DeleteFederationEvent(int64) error

	SaveBackupMetadata(*models.BackupMetadata) error
	ListBackups() ([]models.BackupMetadata, error)
	GetModerationQueue() ([]map[string]interface{}, error)
	UpdateReviewStatus(contentID string, status string) error
	UpdatePostVisibility(postID string, visibility string) error
}

type repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *repository {
	return &repository{db: db}
}

func (r *repository) CreateReport(report *models.Report) error {
	query := `
		INSERT INTO reports (
			reporter_id,
			target_ref,
			target_server,
			reason,
			status,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id
	`

	return r.db.QueryRow(
		query,
		report.ReporterID,
		report.TargetRef,
		report.TargetServer,
		report.Reason,
		models.ReportPending,
	).Scan(&report.ID)
}

func (r *repository) ListPendingReports() ([]models.Report, error) {
	query := `
		SELECT 
			id,
			reporter_id,
			target_ref,
			target_server,
			reason,
			status,
			created_at,
			resolved_at,
			resolved_by
		FROM reports
		WHERE status = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(query, models.ReportPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []models.Report

	for rows.Next() {
		var report models.Report
		err := rows.Scan(
			&report.ID,
			&report.ReporterID,
			&report.TargetRef,
			&report.TargetServer,
			&report.Reason,
			&report.Status,
			&report.CreatedAt,
			&report.ResolvedAt,
			&report.ResolvedBy,
		)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}

	return reports, rows.Err()
}

func (r *repository) GetReportByID(reportID int64) (*models.Report, error) {
	query := `
		SELECT 
			id,
			reporter_id,
			target_ref,
			target_server,
			reason,
			status,
			created_at,
			resolved_at,
			resolved_by
		FROM reports
		WHERE id = $1
	`

	var report models.Report

	err := r.db.QueryRow(query, reportID).Scan(
		&report.ID,
		&report.ReporterID,
		&report.TargetRef,
		&report.TargetServer,
		&report.Reason,
		&report.Status,
		&report.CreatedAt,
		&report.ResolvedAt,
		&report.ResolvedBy,
	)

	if err != nil {
		return nil, err
	}

	return &report, nil
}

func (r *repository) ResolveReport(reportID int64, resolvedBy string) error {
	query := `
		UPDATE reports
		SET 
			status = $1,
			resolved_at = NOW(),
			resolved_by = $2
		WHERE id = $3
		AND status = $4
	`

	result, err := r.db.Exec(
		query,
		models.ReportResolved,
		resolvedBy,
		reportID,
		models.ReportPending,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *repository) BlockServer(server *models.BlockedServer) error {
	query := `
		INSERT INTO blocked_servers (
			server_url,
			reason,
			blocked_by,
			blocked_at,
			is_active,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, NOW(), true, NOW(), NOW())
		ON CONFLICT (server_url)
		DO UPDATE SET
			reason = EXCLUDED.reason,
			blocked_by = EXCLUDED.blocked_by,
			blocked_at = NOW(),
			is_active = true,
			updated_at = NOW()
	`

	_, err := r.db.Exec(
		query,
		server.Domain,
		server.Reason,
		server.BlockedBy,
	)

	return err
}

func (r *repository) IsServerBlocked(domain string) (bool, error) {
	query := `
		SELECT 1
		FROM blocked_servers
		WHERE server_url = $1
		AND is_active = true
		LIMIT 1
	`

	var exists int
	err := r.db.QueryRow(query, domain).Scan(&exists)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *repository) ListBlockedServers() ([]models.BlockedServer, error) {
	query := `
		SELECT 
			id,
			server_url,
			reason,
			blocked_at,
			blocked_by
		FROM blocked_servers
		WHERE is_active = true
		ORDER BY blocked_at DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []models.BlockedServer

	for rows.Next() {
		var s models.BlockedServer
		err := rows.Scan(
			&s.ID,
			&s.Domain,
			&s.Reason,
			&s.BlockedAt,
			&s.BlockedBy,
		)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}

	return servers, rows.Err()
}

// ─── USER BLOCKING ───────────────────────────────────────────────────────────

func (r *repository) BlockUser(block *models.UserBlock) error {
	query := `
		INSERT INTO user_blocks (
			blocker_user_id,
			blocked_user_id,
			reason,
			expires_at,
			is_active,
			created_at
		)
		VALUES ($1, $2, $3, $4, true, NOW())
		ON CONFLICT (blocker_user_id, blocked_user_id)
		DO UPDATE SET
			reason     = EXCLUDED.reason,
			expires_at = EXCLUDED.expires_at,
			is_active  = true
		RETURNING id, created_at
	`
	return r.db.QueryRow(
		query,
		block.BlockerUserID,
		block.BlockedUserID,
		block.Reason,
		block.ExpiresAt,
	).Scan(&block.ID, &block.CreatedAt)
}

func (r *repository) UnblockUser(blockerUserID, blockedUserID string) error {
	query := `
		UPDATE user_blocks
		SET is_active = false
		WHERE blocker_user_id = $1
		  AND blocked_user_id = $2
		  AND is_active = true
	`
	_, err := r.db.Exec(query, blockerUserID, blockedUserID)
	return err
}

func (r *repository) IsUserBlocked(blockerUserID, blockedUserID string) (bool, error) {
	query := `
		SELECT 1
		FROM user_blocks
		WHERE blocker_user_id = $1
		  AND blocked_user_id = $2
		  AND is_active = true
		  AND (expires_at IS NULL OR expires_at > NOW())
		LIMIT 1
	`
	var exists int
	err := r.db.QueryRow(query, blockerUserID, blockedUserID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (r *repository) ListBlockedUsers(blockerUserID string) ([]models.UserBlock, error) {
	query := `
		SELECT id, blocker_user_id, blocked_user_id, reason, created_at, expires_at, is_active
		FROM user_blocks
		WHERE blocker_user_id = $1
		  AND is_active = true
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(query, blockerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []models.UserBlock
	for rows.Next() {
		var b models.UserBlock
		if err := rows.Scan(
			&b.ID,
			&b.BlockerUserID,
			&b.BlockedUserID,
			&b.Reason,
			&b.CreatedAt,
			&b.ExpiresAt,
			&b.IsActive,
		); err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (r *repository) EnqueueFederationEvent(event *models.FederationEvent) error {
	query := `
		INSERT INTO federation_events (
			event_type,
			target_server,
			payload,
			retry_count,
			created_at
		)
		VALUES ($1, $2, $3, 0, NOW())
		RETURNING id
	`

	return r.db.QueryRow(
		query,
		event.EventType,
		event.TargetServer,
		event.Payload,
	).Scan(&event.ID)
}

func (r *repository) ListPendingFederationEvents() ([]models.FederationEvent, error) {
	query := `
		SELECT 
			id,
			event_type,
			target_server,
			payload,
			retry_count,
			created_at,
			last_tried_at
		FROM federation_events
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.FederationEvent

	for rows.Next() {
		var e models.FederationEvent
		err := rows.Scan(
			&e.ID,
			&e.EventType,
			&e.TargetServer,
			&e.Payload,
			&e.RetryCount,
			&e.CreatedAt,
			&e.LastTriedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func (r *repository) IncrementFederationRetry(eventID int64) error {
	query := `
		UPDATE federation_events
		SET 
			retry_count = retry_count + 1,
			last_tried_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.Exec(query, eventID)
	return err
}

func (r *repository) DeleteFederationEvent(eventID int64) error {
	query := `DELETE FROM federation_events WHERE id = $1`
	_, err := r.db.Exec(query, eventID)
	return err
}

func (r *repository) SaveBackupMetadata(backup *models.BackupMetadata) error {
	query := `
		INSERT INTO backup_metadata (
			location,
			created_by,
			created_at
		)
		VALUES ($1, $2, NOW())
		RETURNING id, created_at
	`

	return r.db.QueryRow(
		query,
		backup.Location,
		backup.CreatedBy,
	).Scan(&backup.ID, &backup.CreatedAt)
}

func (r *repository) ListBackups() ([]models.BackupMetadata, error) {
	query := `
		SELECT id, location, created_by, created_at
		FROM backup_metadata
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []models.BackupMetadata

	for rows.Next() {
		var b models.BackupMetadata
		err := rows.Scan(
			&b.ID,
			&b.Location,
			&b.CreatedBy,
			&b.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		backups = append(backups, b)
	}

	return backups, rows.Err()
}

func (r *repository) GetModerationQueue() ([]map[string]interface{}, error) {
	query := `
		SELECT content_id, content_type,
		       toxicity_score, recommendation, review_status
		FROM moderation_results
		WHERE review_status = 'PENDING'
		ORDER BY toxicity_score DESC
	`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, ctype, rec, status string
		var toxicity float64

		if err := rows.Scan(&id, &ctype, &toxicity, &rec, &status); err != nil {
			return nil, err
		}

		results = append(results, map[string]interface{}{
			"content_id":     id,
			"type":           ctype,
			"toxicity":       toxicity,
			"recommendation": rec,
			"status":         status,
		})
	}

	return results, rows.Err()
}

func (r *repository) UpdateReviewStatus(contentID string, status string) error {
	_, err := r.db.Exec(`
		UPDATE moderation_results
		SET review_status = $1
		WHERE content_id = $2
	`, status, contentID)
	return err
}

func (r *repository) UpdatePostVisibility(postID string, visibility string) error {
	_, err := r.db.Exec(`
		UPDATE posts 
		SET visibility = $1 
		WHERE id = $2
	`, visibility, postID)
	return err
}
