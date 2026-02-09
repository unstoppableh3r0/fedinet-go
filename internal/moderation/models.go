package main

import (
	"time"
)

// =================================================================
// EPIC 5 — GOVERNANCE & MODERATION
// =================================================================

type ReportStatus string

const (
	ReportPending  ReportStatus = "pending"
	ReportResolved ReportStatus = "resolved"
)

type Report struct {
	ID           int64        `json:"id"`
	ReporterID   string       `json:"reporter_id"`
	TargetRef    string       `json:"target_ref"`
	TargetServer string       `json:"target_server"`
	Reason       string       `json:"reason"`
	Status       ReportStatus `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	ResolvedAt   *time.Time   `json:"resolved_at,omitempty"`
	ResolvedBy   *string      `json:"resolved_by,omitempty"`
}

type BlockedServer struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Reason    string    `json:"reason"`
	BlockedAt time.Time `json:"blocked_at"`
	BlockedBy string    `json:"blocked_by"`
}

type FederationEventType string

const (
	EventAbuseReportForward FederationEventType = "abuse_report_forward"
	EventServerBlockNotice  FederationEventType = "server_block_notice"
)

type FederationEvent struct {
	ID           int64               `json:"id"`
	EventType    FederationEventType `json:"event_type"`
	TargetServer string              `json:"target_server"`
	Payload      []byte              `json:"payload"`
	RetryCount   int                 `json:"retry_count"`
	CreatedAt    time.Time           `json:"created_at"`
	LastTriedAt  *time.Time          `json:"last_tried_at,omitempty"`
}

type BackupMetadata struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Location  string    `json:"location"`
	CreatedBy string    `json:"created_by"`
}
