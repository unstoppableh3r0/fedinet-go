package moderation_test

import (
	"testing"

	"github.com/unstoppableh3r0/fedinet-go/internal/moderation"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"

)

// --------------------
// Mock Repository
// --------------------

type MockRepository struct {
	CreateReportFn           func(*models.Report) error
	ListPendingReportsFn     func() ([]models.Report, error)
	GetReportByIDFn          func(int64) (*models.Report, error)
	ResolveReportFn          func(int64, string) error
	BlockServerFn            func(*models.BlockedServer) error
	IsServerBlockedFn        func(string) (bool, error)
	EnqueueFederationEventFn func(*models.FederationEvent) error
}

func (m *MockRepository) CreateReport(r *models.Report) error {
	return m.CreateReportFn(r)
}
func (m *MockRepository) ListPendingReports() ([]models.Report, error) {
	return m.ListPendingReportsFn()
}
func (m *MockRepository) GetReportByID(id int64) (*models.Report, error) {
	return m.GetReportByIDFn(id)
}
func (m *MockRepository) ResolveReport(id int64, by string) error {
	return m.ResolveReportFn(id, by)
}
func (m *MockRepository) BlockServer(s *models.BlockedServer) error {
	return m.BlockServerFn(s)
}
func (m *MockRepository) IsServerBlocked(domain string) (bool, error) {
	return m.IsServerBlockedFn(domain)
}
func (m *MockRepository) EnqueueFederationEvent(e *models.FederationEvent) error {
	return m.EnqueueFederationEventFn(e)
}

// --------------------
// Tests
// --------------------

func TestSubmitReport_Local(t *testing.T) {
	mockRepo := &MockRepository{
		CreateReportFn: func(r *models.Report) error { return nil },
		IsServerBlockedFn: func(string) (bool, error) {
			return false, nil
		},
	}

	service := moderation.NewService(mockRepo)

	err := service.SubmitReport(
		"user1",
		"post:1@local",
		"",
		"spam",
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSubmitReport_Remote_ServerBlocked(t *testing.T) {
	mockRepo := &MockRepository{
		CreateReportFn: func(r *models.Report) error { return nil },
		IsServerBlockedFn: func(string) (bool, error) {
			return true, nil
		},
	}

	service := moderation.NewService(mockRepo)

	err := service.SubmitReport(
		"user1",
		"post:9@bad.server",
		"bad.server",
		"abuse",
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestResolveReport(t *testing.T) {
	mockRepo := &MockRepository{
		GetReportByIDFn: func(id int64) (*models.Report, error) {
			return &models.Report{ID: id}, nil
		},
		ResolveReportFn: func(int64, string) error {
			return nil
		},
	}

	service := moderation.NewService(mockRepo)

	if err := service.ResolveReport(1, "mod1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestBlockServer(t *testing.T) {
	mockRepo := &MockRepository{
		BlockServerFn: func(*models.BlockedServer) error { return nil },
		EnqueueFederationEventFn: func(*models.FederationEvent) error {
			return nil
		},
	}

	service := moderation.NewService(mockRepo)

	if err := service.BlockServer("evil.server", "spam", "admin1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
