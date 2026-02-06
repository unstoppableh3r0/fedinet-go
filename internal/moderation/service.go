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

	if err := s.repo.CreateReport(report); err != nil {
		return err
	}

	if targetServer != "" {
		blocked, err := s.repo.IsServerBlocked(targetServer)
		if err != nil {
			return err
		}
		if blocked {
			return nil
		}

		if err := s.forwardReport(report); err != nil {
			return s.queueReportForward(report)
		}
	}

	return nil
}

func (s *Service) ListPendingReports() ([]models.Report, error) {
	return s.repo.ListPendingReports()
}

func (s *Service) ResolveReport(
	reportID int64,
	resolvedBy string,
) error {

	_, err := s.repo.GetReportByID(reportID)
	if err != nil {
		return err
	}

	return s.repo.ResolveReport(reportID, resolvedBy)
}

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

func (s *Service) forwardReport(report *models.Report) error {
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
