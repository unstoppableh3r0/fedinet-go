package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/unstoppableh3r0/fedinet-go/internal/privacy"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// TestEvaluateAccess tests User Story 3.3 and 3.7: Access control logic
func TestEvaluateAccess(t *testing.T) {
	tests := []struct {
		name       string
		author     string
		viewer     string
		visibility models.Visibility
		isFollower bool
		want       bool
	}{
		{
			name:       "Author viewing own content",
			author:     "alice",
			viewer:     "alice",
			visibility: models.VisibilityPrivate,
			isFollower: false,
			want:       true,
		},
		{
			name:       "Public content",
			author:     "alice",
			viewer:     "bob",
			visibility: models.VisibilityPublic,
			isFollower: false,
			want:       true,
		},
		{
			name:       "Followers only - is follower",
			author:     "alice",
			viewer:     "bob",
			visibility: models.VisibilityFollowers,
			isFollower: true,
			want:       true,
		},
		{
			name:       "Followers only - not follower",
			author:     "alice",
			viewer:     "charlie",
			visibility: models.VisibilityFollowers,
			isFollower: false,
			want:       false,
		},
		{
			name:       "Private content - regular user",
			author:     "alice",
			viewer:     "bob",
			visibility: models.VisibilityPrivate,
			isFollower: true,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := privacy.EvaluateAccess(tt.author, tt.viewer, tt.visibility, tt.isFollower)
			if got != tt.want {
				t.Errorf("EvaluateAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFetchMediaProxied tests User Story 3.14: Privacy Proxy
func TestFetchMediaProxied(t *testing.T) {
	// Start a local server to mock the remote media server
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers are scrubbed
		if r.Header.Get("Referer") != "" {
			t.Errorf("Expected Referer header to be scrubbed, got %s", r.Header.Get("Referer"))
		}
		if r.Header.Get("X-Forwarded-For") != "" {
			t.Errorf("Expected X-Forwarded-For header to be scrubbed, got %s", r.Header.Get("X-Forwarded-For"))
		}
		if r.Header.Get("User-Agent") != "Fedinet-Privacy-Proxy/1.0" {
			t.Errorf("Expected User-Agent to be 'Fedinet-Privacy-Proxy/1.0', got %s", r.Header.Get("User-Agent"))
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake-image-data"))
	}))
	defer remoteServer.Close()

	// Call the proxy function
	body, contentType, err := privacy.FetchMediaProxied(remoteServer.URL)
	if err != nil {
		t.Fatalf("FetchMediaProxied failed: %v", err)
	}
	defer body.Close()

	// Verify content type
	if contentType != "image/jpeg" {
		t.Errorf("Expected content type image/jpeg, got %s", contentType)
	}

	// Verify body content
	content, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	if string(content) != "fake-image-data" {
		t.Errorf("Expected body 'fake-image-data', got %s", string(content))
	}
}

// TestFetchMediaProxiedError tests error handling
func TestFetchMediaProxiedError(t *testing.T) {
	_, _, err := privacy.FetchMediaProxied("http://invalid-url-that-does-not-exist.local")
	if err == nil {
		t.Error("Expected error for invalid URL, got nil")
	}
}
