package moderation

import (
	"errors"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// REPORT SUBMISSION

func (s *Service) SubmitReport(
	reporterID string,
	targetRef string,
	targetServer string,
	reason string,
) error {

	report := &models.Report{
		ReporterID:   reporterID,
		TargetRef:    targetRef,
		TargetServer: targetServer,
		Reason:       reason,
		Status:       models.ReportPending,
		CreatedAt:    time.Now(),
	}

	// Save locally
	if err := s.repo.CreateReport(report); err != nil {
		return err
	}

	// If report targets remote server → attempt federation
	if targetServer != "" {

		blocked, err := s.repo.IsServerBlocked(targetServer)
		if err != nil {
			return err
		}

		// Do not forward to blocked servers
		if blocked {
			return nil
		}

		// Try direct federation call
		if err := s.forwardReport(report); err != nil {
			// If federation fails → queue event for retry
			return s.queueReportForward(report)
		}
	}

	return nil
}

// REPORT LISTING

func (s *Service) ListPendingReports() ([]models.Report, error) {
	return s.repo.ListPendingReports()
}

func (s *Service) GetModerationQueue() ([]map[string]interface{}, error) {
	return s.repo.GetModerationQueue()
}

func (s *Service) UpdateReviewStatus(contentID string, status string) error {
	err := s.repo.UpdateReviewStatus(contentID, status)
	if err != nil {
		return err
	}

	if status == "APPROVED" {
		return s.repo.UpdatePostVisibility(contentID, "PUBLIC")
	}
	return nil
}

// REPORT RESOLUTION

func (s *Service) ResolveReport(
	reportID int64,
	resolvedBy string,
) error {

	report, err := s.repo.GetReportByID(reportID)
	if err != nil {
		return err
	}

	if err := s.repo.ResolveReport(reportID, resolvedBy); err != nil {
		return err
	}

	// If report involved remote server → notify via federation
	if report.TargetServer != "" {

		event := &models.FederationEvent{
			EventType:    models.EventAbuseReportResolved,
			TargetServer: report.TargetServer,
			Payload:      []byte{},
			RetryCount:   0,
			CreatedAt:    time.Now(),
		}

		_ = s.repo.EnqueueFederationEvent(event)
	}

	return nil
}

// USER BLOCKING

func (s *Service) BlockUser(
	blockerUserID string,
	blockedUserID string,
	reason string,
) error {
	if blockerUserID == "" || blockedUserID == "" {
		return errors.New("blocker and blocked user IDs are required")
	}
	if blockerUserID == blockedUserID {
		return errors.New("a user cannot block themselves")
	}
	block := &models.UserBlock{
		BlockerUserID: blockerUserID,
		BlockedUserID: blockedUserID,
		Reason:        reason,
		IsActive:      true,
	}
	return s.repo.BlockUser(block)
}

func (s *Service) UnblockUser(
	blockerUserID string,
	blockedUserID string,
) error {
	if blockerUserID == "" || blockedUserID == "" {
		return errors.New("blocker and blocked user IDs are required")
	}
	return s.repo.UnblockUser(blockerUserID, blockedUserID)
}

func (s *Service) IsUserBlocked(blockerUserID, blockedUserID string) (bool, error) {
	return s.repo.IsUserBlocked(blockerUserID, blockedUserID)
}

func (s *Service) ListBlockedUsers(blockerUserID string) ([]models.UserBlock, error) {
	if blockerUserID == "" {
		return nil, errors.New("blocker user ID is required")
	}
	return s.repo.ListBlockedUsers(blockerUserID)
}

// SERVER BLOCKING

func (s *Service) BlockServer(
	domain string,
	reason string,
	adminID string,
) error {

	if domain == "" {
		return errors.New("server domain cannot be empty")
	}

	block := &models.BlockedServer{
		Domain:    domain,
		Reason:    reason,
		BlockedAt: time.Now(),
		BlockedBy: adminID,
	}

	if err := s.repo.BlockServer(block); err != nil {
		return err
	}

	return s.notifyServerBlock(domain)
}

// FEDERATION HELPERS

func (s *Service) forwardReport(report *models.Report) error {
	// Placeholder for real federation HTTP call
	// Currently simulated failure → forces queue fallback
	return errors.New("federation unavailable")
}

func (s *Service) queueReportForward(report *models.Report) error {
	event := &models.FederationEvent{
		EventType:    models.EventAbuseReportForward,
		TargetServer: report.TargetServer,
		Payload:      []byte{},
		RetryCount:   0,
		CreatedAt:    time.Now(),
	}

	return s.repo.EnqueueFederationEvent(event)
}

func (s *Service) notifyServerBlock(domain string) error {
	event := &models.FederationEvent{
		EventType:    models.EventServerBlockNotice,
		TargetServer: domain,
		Payload:      []byte{},
		RetryCount:   0,
		CreatedAt:    time.Now(),
	}

	return s.repo.EnqueueFederationEvent(event)
}
