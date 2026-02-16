package main

import (
	"fmt"
	"time"
)


func ValidateRankingMode(mode RankingMode) error {
	switch mode {
	case RankingModeChronological, RankingModePopular, RankingModeRelevance, RankingModeTrending:
		return nil
	default:
		return fmt.Errorf("invalid ranking mode: %s", mode)
	}
}


func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}


func CalculateEngagementScore(likeCount, replyCount, repostCount int) int {
	return likeCount + (replyCount * 2) + (repostCount * 3)
}


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


func IsWithinTimeWindow(t time.Time, window time.Duration) bool {
	return time.Since(t) <= window
}


func TruncateContent(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}
	return content[:maxLength-3] + "..."
}


func GetDefaultPageSize() int {
	return 50
}


func GetMaxPageSize() int {
	return 200
}


func EnforcePageSize(size int) int {
	if size <= 0 {
		return GetDefaultPageSize()
	}
	if size > GetMaxPageSize() {
		return GetMaxPageSize()
	}
	return size
}
