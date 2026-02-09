package main

import (
	"testing"
)

// --------------------
// Mock Repository
// --------------------

type MockRepository struct {
	CreateReportFn           func(*Report) error
	ListPendingReportsFn     func() ([]Report, error)
	GetReportByIDFn          func(int64) (*Report, error)
	ResolveReportFn          func(int64, string) error
	BlockServerFn            func(*BlockedServer) error
	IsServerBlockedFn        func(string) (bool, error)
	EnqueueFederationEventFn func(*FederationEvent) error
}

func (m *MockRepository) CreateReport(r *Report) error {
	return m.CreateReportFn(r)
}
func (m *MockRepository) ListPendingReports() ([]Report, error) {
	return m.ListPendingReportsFn()
}
func (m *MockRepository) GetReportByID(id int64) (*Report, error) {
	return m.GetReportByIDFn(id)
}
func (m *MockRepository) ResolveReport(id int64, by string) error {
	return m.ResolveReportFn(id, by)
}
func (m *MockRepository) BlockServer(s *BlockedServer) error {
	return m.BlockServerFn(s)
}
func (m *MockRepository) IsServerBlocked(domain string) (bool, error) {
	return m.IsServerBlockedFn(domain)
}
func (m *MockRepository) EnqueueFederationEvent(e *FederationEvent) error {
	return m.EnqueueFederationEventFn(e)
}

// --------------------
// Tests
// --------------------

func TestSubmitReport_Local(t *testing.T) {
	mockRepo := &MockRepository{
		CreateReportFn: func(r *Report) error { return nil },
		IsServerBlockedFn: func(string) (bool, error) {
			return false, nil
		},
	}

	service := NewService(mockRepo)

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
		CreateReportFn: func(r *Report) error { return nil },
		IsServerBlockedFn: func(string) (bool, error) {
			return true, nil
		},
	}

	service := NewService(mockRepo)

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
		GetReportByIDFn: func(id int64) (*Report, error) {
			return &Report{ID: id}, nil
		},
		ResolveReportFn: func(int64, string) error {
			return nil
		},
	}

	service := NewService(mockRepo)

	if err := service.ResolveReport(1, "mod1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestBlockServer(t *testing.T) {
	mockRepo := &MockRepository{
		BlockServerFn: func(*BlockedServer) error { return nil },
		EnqueueFederationEventFn: func(*FederationEvent) error {
			return nil
		},
	}

	service := NewService(mockRepo)

	if err := service.BlockServer("evil.server", "spam", "admin1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
