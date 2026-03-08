package aimoderation

func GenerateRecommendation(res *ModerationResponse) string {

	// Threat is checked first — highest priority (mirrors Python ml-service)
	if res.Threat > 0.70 {
		return "ESCALATE_IMMEDIATELY"
	}

	if res.Toxicity > 0.80 || res.Hate > 0.75 {
		return "FLAG_FOR_REVIEW"
	}

	if res.Spam > 0.85 {
		return "MARK_AS_SPAM"
	}

	return "SAFE"
}
