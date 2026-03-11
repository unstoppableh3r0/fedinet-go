package aimoderation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// CallModerationAPI sends text to the Python ML microservice and returns scores.
// URL and API key are read from environment variables:
//
//	AI_MODERATION_API_URL  (default: http://ml-service:8090/moderate)
//	AI_MODERATION_API_KEY  (default: changeme)
func CallModerationAPI(text string) (*ModerationResponse, error) {
	apiURL := os.Getenv("AI_MODERATION_API_URL")
	if apiURL == "" {
		apiURL = "http://ml-service:8090/moderate"
	}

	apiKey := os.Getenv("AI_MODERATION_API_KEY")
	if apiKey == "" {
		apiKey = "changeme"
	}

	payload := map[string]string{"input": text}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build moderation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("moderation service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("moderation service returned status %d", resp.StatusCode)
	}

	var result ModerationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode moderation response: %w", err)
	}

	// Recompute recommendation locally as a safety check
	result.Recommendation = GenerateRecommendation(&result)

	return &result, nil
}
