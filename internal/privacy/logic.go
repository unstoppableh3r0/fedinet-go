package privacy

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"

// EvaluateAccess evaluates if a viewer has permission to see content
// Covers Story 3.3 and 3.7
func EvaluateAccess(author, viewer string, visibility models.Visibility, isFollower bool) bool {
	if author == viewer {
		return true // Author always has access
	}

	switch visibility {
	case models.VisibilityPublic:
		return true
	case models.VisibilityFollowers:
		return isFollower
	case models.VisibilityPrivate:
		return false
	case models.VisibilityServer:
		// Logic to compare domains would be added here
		return false
	default:
		return false
	}
}
