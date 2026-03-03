package aimoderation

func GenerateRecommendation(res *ModerationResponse) string {

	if res.Toxicity > 0.80 || res.Hate > 0.75 {
		return "FLAG_FOR_REVIEW"
	}

	if res.Spam > 0.85 {
		return "MARK_AS_SPAM"
	}

	if res.Threat > 0.70 {
		return "ESCALATE_IMMEDIATELY"
	}

	return "SAFE"
}
