package main

import (
	"database/sql"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// CreateReport inserts a new moderation report
func CreateReport(r models.Report) (*models.Report, error) {
	err := db.QueryRow(`
		INSERT INTO reports (reporter_id, target_ref, target_server, reason, status, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, created_at
	`, r.ReporterID, r.TargetRef, r.TargetServer, r.Reason, models.ReportPending).
		Scan(&r.ID, &r.CreatedAt)

	if err != nil {
		return nil, err
	}

	r.Status = models.ReportPending
	return &r, nil
}

// ListReports fetches reports optionally filtered by status
func ListReports(status string) ([]models.Report, error) {
	query := `
		SELECT id, reporter_id, target_ref, target_server,
		       reason, status, created_at, resolved_at, resolved_by
		FROM reports
	`

	var rows *sql.Rows
	var err error

	if status != "" {
		query += " WHERE status = $1"
		rows, err = db.Query(query, status)
	} else {
		rows, err = db.Query(query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reports := []models.Report{}

	for rows.Next() {
		var r models.Report
		err := rows.Scan(
			&r.ID,
			&r.ReporterID,
			&r.TargetRef,
			&r.TargetServer,
			&r.Reason,
			&r.Status,
			&r.CreatedAt,
			&r.ResolvedAt,
			&r.ResolvedBy,
		)
		if err != nil {
			continue
		}
		reports = append(reports, r)
	}

	return reports, nil
}

// ResolveReport updates a report to resolved
func ResolveReport(reportID int64, resolvedBy string) error {
	_, err := db.Exec(`
		UPDATE reports
		SET status = $1,
		    resolved_at = NOW(),
		    resolved_by = $2
		WHERE id = $3
	`, models.ReportResolved, resolvedBy, reportID)

	return err
}
