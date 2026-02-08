package main

import (
	"testing"
)

// Mock DB interactions for testing (stubbed)
// In a real integration test we'd use the test DB.
// For this unit test, we just want to verify the logic of parseHandle and dispatch.

func TestParseHandle(t *testing.T) {
	tests := []struct {
		input    string
		username string
		domain   string
		valid    bool
	}{
		{"alice", "alice", "", true},
		{"alice@localhost", "alice", "localhost", true},
		{"alice@remote.com", "alice", "remote.com", true},
		{"@alice@remote.com", "alice", "remote.com", true},
		{"invalid@@format", "", "", false},
	}

	for _, tt := range tests {
		u, d, err := parseHandle(tt.input)
		if tt.valid && err != nil {
			t.Errorf("parseHandle(%q) unexpected error: %v", tt.input, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("parseHandle(%q) expected error, got nil", tt.input)
		}
		if u != tt.username || d != tt.domain {
			t.Errorf("parseHandle(%q) = %q, %q; want %q, %q", tt.input, u, d, tt.username, tt.domain)
		}
	}
}

func TestIsLocalDomain(t *testing.T) {
	if !isLocalDomain("localhost") {
		t.Error("localhost should be local")
	}
	if !isLocalDomain("") {
		t.Error("empty domain should be local")
	}
	if isLocalDomain("google.com") {
		t.Error("google.com should not be local")
	}
}
