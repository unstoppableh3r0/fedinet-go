package aimoderation

type ModerationRequest struct {
	ContentID string `json:"content_id"`
	Text      string `json:"text"`
}

type ModerationResponse struct {
	Toxicity       float64 `json:"toxicity"`
	Hate           float64 `json:"hate"`
	Spam           float64 `json:"spam"`
	Threat         float64 `json:"threat"`
	Confidence     float64 `json:"confidence"`
	Recommendation string  `json:"recommendation"`
}
