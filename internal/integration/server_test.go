package integration

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/unstoppableh3r0/fedinet-go/internal/server"
)

func TestRegisterEndpoint(t *testing.T) {
	handler := server.SetupRouter()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Case 1: Missing invite code → should return 403
	body := []byte(`{
		"username":"testuser",
		"password":"password123"
	}`)

	resp, err := http.Post(ts.URL+"/register", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when invite missing, got %d", resp.StatusCode)
	}
}
