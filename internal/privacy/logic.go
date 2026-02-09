package main

// Visibility type is defined in models.go in the same package

// EvaluateAccess evaluates if a viewer has permission to see content
// Covers Story 3.3 and 3.7
func EvaluateAccess(author, viewer string, visibility Visibility, isFollower bool) bool {
	if author == viewer {
		return true // Author always has access
	}

	switch visibility {
	case VisibilityPublic:
		return true
	case VisibilityFollowers:
		return isFollower
	case VisibilityPrivate:
		return false
	case VisibilityServer:
		// Logic to compare domains would be added here
		return false
	default:
		return false
	}
}
