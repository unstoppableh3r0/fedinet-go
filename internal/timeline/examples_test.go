package main

import (
	"testing"
	"time"
)

// TestRankingModes demonstrates different ranking algorithms
func TestRankingModes(t *testing.T) {
	now := time.Now()

	posts := []Post{
		{
			ID:          "1",
			Author:      "user1",
			Content:     "Old but popular post",
			CreatedAt:   now.Add(-48 * time.Hour),
			UpdatedAt:   now.Add(-48 * time.Hour),
			LikeCount:   200,
			ReplyCount:  50,
			RepostCount: 30,
		},
		{
			ID:          "2",
			Author:      "user2",
			Content:     "Recent trending post",
			CreatedAt:   now.Add(-2 * time.Hour),
			UpdatedAt:   now.Add(-2 * time.Hour),
			LikeCount:   100,
			ReplyCount:  25,
			RepostCount: 15,
		},
		{
			ID:          "3",
			Author:      "user3",
			Content:     "Very recent post",
			CreatedAt:   now.Add(-10 * time.Minute),
			UpdatedAt:   now.Add(-10 * time.Minute),
			LikeCount:   5,
			ReplyCount:  2,
			RepostCount: 1,
		},
	}

	t.Run("Chronological Ranking", func(t *testing.T) {
		ranked := RankPosts(posts, RankingModeChronological, "test-user")

		// Most recent should be first
		if ranked[0].Post.ID != "3" {
			t.Errorf("Expected post 3 first in chronological, got %s", ranked[0].Post.ID)
		}

		t.Logf("Chronological order: %s, %s, %s", ranked[0].Post.ID, ranked[1].Post.ID, ranked[2].Post.ID)
	})

	t.Run("Popular Ranking", func(t *testing.T) {
		ranked := RankPosts(posts, RankingModePopular, "test-user")

		// Highest engagement should be first
		if ranked[0].Post.ID != "1" {
			t.Errorf("Expected post 1 first in popular, got %s", ranked[0].Post.ID)
		}

		t.Logf("Popular order: %s (score: %.2f), %s (score: %.2f), %s (score: %.2f)",
			ranked[0].Post.ID, ranked[0].RankScore,
			ranked[1].Post.ID, ranked[1].RankScore,
			ranked[2].Post.ID, ranked[2].RankScore)
	})

	t.Run("Trending Ranking", func(t *testing.T) {
		ranked := RankPosts(posts, RankingModeTrending, "test-user")

		// Post 2 should rank high due to recent high velocity
		t.Logf("Trending order: %s (score: %.2f), %s (score: %.2f), %s (score: %.2f)",
			ranked[0].Post.ID, ranked[0].RankScore,
			ranked[1].Post.ID, ranked[1].RankScore,
			ranked[2].Post.ID, ranked[2].RankScore)
	})

	t.Run("Relevance Ranking", func(t *testing.T) {
		ranked := RankPosts(posts, RankingModeRelevance, "test-user")

		t.Logf("Relevance order: %s (score: %.2f), %s (score: %.2f), %s (score: %.2f)",
			ranked[0].Post.ID, ranked[0].RankScore,
			ranked[1].Post.ID, ranked[1].RankScore,
			ranked[2].Post.ID, ranked[2].RankScore)
	})
}

// TestActivityLevel demonstrates activity level calculation
func TestActivityLevel(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		lastActivity  time.Time
		expectedLevel ActivityLevel
	}{
		{
			name:          "High activity - 2 minutes ago",
			lastActivity:  now.Add(-2 * time.Minute),
			expectedLevel: ActivityHigh,
		},
		{
			name:          "Medium activity - 10 minutes ago",
			lastActivity:  now.Add(-10 * time.Minute),
			expectedLevel: ActivityMedium,
		},
		{
			name:          "Low activity - 30 minutes ago",
			lastActivity:  now.Add(-30 * time.Minute),
			expectedLevel: ActivityLow,
		},
		{
			name:          "Idle - 2 hours ago",
			lastActivity:  now.Add(-2 * time.Hour),
			expectedLevel: ActivityIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := GetActivityLevel(tt.lastActivity)
			if level != tt.expectedLevel {
				t.Errorf("Expected %s, got %s", tt.expectedLevel, level)
			}
			t.Logf("Activity level: %s", level)
		})
	}
}

// TestDefaultConfigs demonstrates default configurations
func TestDefaultConfigs(t *testing.T) {
	t.Run("Default Offline Config", func(t *testing.T) {
		config := DefaultOfflineConfig()

		t.Logf("Max Cache Size: %d bytes (%.2f MB)",
			config.MaxCacheSizeBytes,
			float64(config.MaxCacheSizeBytes)/(1024*1024))
		t.Logf("Max Posts Per User: %d", config.MaxPostsPerUser)
		t.Logf("Cache Duration: %s", config.CacheDuration)
		t.Logf("Auto Refresh: %v", config.AutoRefresh)

		if config.MaxCacheSizeBytes != 50*1024*1024 {
			t.Errorf("Expected 50MB cache size, got %d", config.MaxCacheSizeBytes)
		}
	})

	t.Run("Default Refresh Config", func(t *testing.T) {
		config := DefaultRefreshConfig("test-user")

		t.Logf("Base Interval: %s", config.BaseInterval)
		t.Logf("Min Interval: %s", config.MinInterval)
		t.Logf("Max Interval: %s", config.MaxInterval)
		t.Logf("Adaptive Enabled: %v", config.AdaptiveEnabled)
		t.Logf("Throttle Enabled: %v", config.ThrottleEnabled)

		if config.BaseInterval != 30*time.Second {
			t.Errorf("Expected 30s base interval, got %s", config.BaseInterval)
		}
	})
}

// TestUtilityFunctions demonstrates utility functions
func TestUtilityFunctions(t *testing.T) {
	t.Run("Validate Ranking Mode", func(t *testing.T) {
		validModes := []RankingMode{
			RankingModeChronological,
			RankingModePopular,
			RankingModeRelevance,
			RankingModeTrending,
		}

		for _, mode := range validModes {
			if err := ValidateRankingMode(mode); err != nil {
				t.Errorf("Valid mode %s failed validation: %v", mode, err)
			}
		}

		if err := ValidateRankingMode("invalid"); err == nil {
			t.Error("Expected error for invalid mode")
		}
	})

	t.Run("Time Ago", func(t *testing.T) {
		now := time.Now()

		tests := []struct {
			time     time.Time
			expected string
		}{
			{now.Add(-30 * time.Second), "just now"},
			{now.Add(-5 * time.Minute), "5 minutes ago"},
			{now.Add(-2 * time.Hour), "2 hours ago"},
			{now.Add(-3 * 24 * time.Hour), "3 days ago"},
		}

		for _, tt := range tests {
			result := TimeAgo(tt.time)
			t.Logf("Time ago for %v: %s", time.Since(tt.time), result)
			if result != tt.expected {
				t.Logf("Expected '%s', got '%s'", tt.expected, result)
			}
		}
	})

	t.Run("Engagement Score", func(t *testing.T) {
		score := CalculateEngagementScore(100, 50, 25)
		expected := 100 + (50 * 2) + (25 * 3) // 100 + 100 + 75 = 275

		if score != expected {
			t.Errorf("Expected score %d, got %d", expected, score)
		}

		t.Logf("Engagement score for 100 likes, 50 replies, 25 reposts: %d", score)
	})

	t.Run("Page Size Enforcement", func(t *testing.T) {
		tests := []struct {
			input    int
			expected int
		}{
			{0, 50},    // Default
			{25, 25},   // Valid
			{500, 200}, // Too large
			{-10, 50},  // Invalid
		}

		for _, tt := range tests {
			result := EnforcePageSize(tt.input)
			if result != tt.expected {
				t.Errorf("EnforcePageSize(%d): expected %d, got %d", tt.input, tt.expected, result)
			}
		}
	})
}

// Example demonstrates how to use the timeline features
func ExampleRankPosts() {
	posts := []Post{
		{
			ID:          "1",
			Content:     "Test post",
			CreatedAt:   time.Now(),
			LikeCount:   10,
			ReplyCount:  5,
			RepostCount: 2,
		},
	}

	ranked := RankPosts(posts, RankingModePopular, "user123")

	for _, post := range ranked {
		println("Post:", post.Post.ID, "Score:", post.RankScore)
	}
}

// BenchmarkRankPosts benchmarks the ranking algorithm
func BenchmarkRankPosts(b *testing.B) {
	// Create sample posts
	posts := make([]Post, 100)
	now := time.Now()

	for i := 0; i < 100; i++ {
		posts[i] = Post{
			ID:          string(rune(i)),
			Content:     "Test post",
			CreatedAt:   now.Add(-time.Duration(i) * time.Hour),
			LikeCount:   i * 10,
			ReplyCount:  i * 2,
			RepostCount: i,
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = RankPosts(posts, RankingModeRelevance, "test-user")
	}
}
