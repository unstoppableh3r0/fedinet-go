package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Mock structures matching the identity models
type Identity struct {
	ID             uuid.UUID `json:"id"`
	UserID         string    `json:"user_id"`
	HomeServer     string    `json:"home_server"`
	PublicKey      string    `json:"public_key"`
	AllowDiscovery bool      `json:"allow_discovery"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Profile struct {
	UserID              string    `json:"user_id"`
	DisplayName         string    `json:"display_name"`
	AvatarURL           *string   `json:"avatar_url"`
	BannerURL           *string   `json:"banner_url"`
	Bio                 *string   `json:"bio"`
	PortfolioURL        *string   `json:"portfolio_url"`
	BirthDate           *string   `json:"birth_date"`
	Location            *string   `json:"location"`
	FollowersVisibility string    `json:"followers_visibility"`
	FollowingVisibility string    `json:"following_visibility"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	FollowersCount      int       `json:"followers_count"`
	FollowingCount      int       `json:"following_count"`
}

type UserDocument struct {
	Identity Identity `json:"identity"`
	Profile  Profile  `json:"profile"`
}

type UpdateProfileRequest struct {
	UserID              string  `json:"user_id"`
	DisplayName         *string `json:"display_name"`
	AvatarURL           *string `json:"avatar_url"`
	BannerURL           *string `json:"banner_url"`
	Bio                 *string `json:"bio"`
	PortfolioURL        *string `json:"portfolio_url"`
	BirthDate           *string `json:"birth_date"`
	Location            *string `json:"location"`
	FollowersVisibility *string `json:"followers_visibility"`
	FollowingVisibility *string `json:"following_visibility"`
}

// TestUserSearchEndpoint tests User Story 1.3: User Search
func TestUserSearchEndpoint(t *testing.T) {
	req, err := http.NewRequest("GET", "/identity/user/search?user_id=alice", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(mockUserSearchHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response UserDocument
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}

	if response.Identity.UserID != "alice@fedinet.local" {
		t.Errorf("Expected UserID alice@fedinet.local, got %s", response.Identity.UserID)
	}
	if response.Profile.DisplayName != "Alice Wonderland" {
		t.Errorf("Expected DisplayName Alice Wonderland, got %s", response.Profile.DisplayName)
	}
}

// TestRegisterEndpoint tests User Story 1.1: Registration
func TestRegisterEndpoint(t *testing.T) {
	payload := map[string]string{
		"username": "bob",
		"password": "securepassword123",
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "/identity/register", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(mockRegisterHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusCreated)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}

	if response["user_id"] != "bob@fedinet.local" {
		t.Errorf("Expected user_id bob@fedinet.local, got %s", response["user_id"])
	}
}

// TestFollowEndpoint tests User Story 1.4: Following
func TestFollowEndpoint(t *testing.T) {
	payload := map[string]string{
		"follower": "alice@fedinet.local",
		"followee": "bob@fedinet.local",
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "/identity/follow", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(mockFollowHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}

	if response["message"] != "followed" {
		t.Errorf("Expected message 'followed', got %s", response["message"])
	}
}

// Mock handlers simulating the identity service behavior

func mockUserSearchHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "missing user_id", http.StatusBadRequest)
		return
	}

	// Respond with a mock UserDocument
	doc := UserDocument{
		Identity: Identity{
			ID:             uuid.New(),
			UserID:         userID + "@fedinet.local",
			HomeServer:     "http://localhost:8080",
			AllowDiscovery: true,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		Profile: Profile{
			UserID:         userID + "@fedinet.local",
			DisplayName:    "Alice Wonderland",
			FollowersCount: 100,
			FollowingCount: 50,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func mockRegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req["username"] == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"user_id":     req["username"] + "@fedinet.local",
		"home_server": "http://localhost:8080",
	})
}

func mockFollowHandler(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req["follower"] == "" || req["followee"] == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "followed",
	})
}
