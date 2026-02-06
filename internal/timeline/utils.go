package main

import (
	"fmt"
	"time"
)

// ValidateRankingMode checks if a ranking mode is valid
func ValidateRankingMode(mode RankingMode) error {
	switch mode {
	case RankingModeChronological, RankingModePopular, RankingModeRelevance, RankingModeTrending:
		return nil
	default:
		return fmt.Errorf("invalid ranking mode: %s", mode)
	}
}

// FormatDuration formats duration in a human-readable way
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// CalculateEngagementScore computes a simple engagement score
func CalculateEngagementScore(likeCount, replyCount, repostCount int) int {
	return likeCount + (replyCount * 2) + (repostCount * 3)
}

// TimeAgo returns a human-readable time difference
func TimeAgo(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		return fmt.Sprintf("%d minute%s ago", minutes, pluralize(minutes))
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("%d hour%s ago", hours, pluralize(hours))
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%d day%s ago", days, pluralize(days))
	} else if duration < 30*24*time.Hour {
		weeks := int(duration.Hours() / (24 * 7))
		return fmt.Sprintf("%d week%s ago", weeks, pluralize(weeks))
	} else {
		months := int(duration.Hours() / (24 * 30))
		return fmt.Sprintf("%d month%s ago", months, pluralize(months))
	}
}

func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// IsWithinTimeWindow checks if a time is within a duration from now
func IsWithinTimeWindow(t time.Time, window time.Duration) bool {
	return time.Since(t) <= window
}

// TruncateContent truncates content to a maximum length
func TruncateContent(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}
	return content[:maxLength-3] + "..."
}

// GetDefaultPageSize returns the default page size for pagination
func GetDefaultPageSize() int {
	return 50
}

// GetMaxPageSize returns the maximum allowed page size
func GetMaxPageSize() int {
	return 200
}

// EnforcePageSize ensures page size is within valid bounds
func EnforcePageSize(size int) int {
	if size <= 0 {
		return GetDefaultPageSize()
	}
	if size > GetMaxPageSize() {
		return GetMaxPageSize()
	}
	return size
}
