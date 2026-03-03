package aimoderation

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func CallModerationAPI(text string) (*ModerationResponse, error) {

	payload := map[string]string{
		"input": text,
	}

	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "https://api.your-ai-provider.com/moderate", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer YOUR_API_KEY")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ModerationResponse
	json.NewDecoder(resp.Body).Decode(&result)

	result.Recommendation = GenerateRecommendation(&result)

	return &result, nil
}
