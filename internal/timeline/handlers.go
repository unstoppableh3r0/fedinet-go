package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// ============ Ranking Preference Handlers ============

// GetRankingPreferenceHandler retrieves user's ranking preference
func GetRankingPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}

	preference, err := GetUserRankingPreference(userID)
	if err != nil {
		http.Error(w, "Failed to get preference", http.StatusInternalServerError)
		log.Printf("Error getting ranking preference: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":    userID,
		"preference": preference,
	})
}

// SetRankingPreferenceHandler sets user's ranking preference
func SetRankingPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID     string      `json:"user_id"`
		Preference RankingMode `json:"preference"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate ranking mode
	validModes := map[RankingMode]bool{
		RankingModeChronological: true,
		RankingModePopular:       true,
		RankingModeRelevance:     true,
		RankingModeTrending:      true,
	}

	if !validModes[req.Preference] {
		http.Error(w, "Invalid ranking mode", http.StatusBadRequest)
		return
	}

	if err := SetUserRankingPreference(req.UserID, req.Preference); err != nil {
		http.Error(w, "Failed to set preference", http.StatusInternalServerError)
		log.Printf("Error setting ranking preference: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Ranking preference updated",
	})
}

// GetTimelineHandler retrieves timeline with applied ranking
func GetTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}

	// Get ranking mode (from query or user preference)
	rankingMode := RankingMode(r.URL.Query().Get("ranking_mode"))
	if rankingMode == "" {
		// Use user's saved preference
		var err error
		rankingMode, err = GetUserRankingPreference(userID)
		if err != nil {
			rankingMode = RankingModeChronological // default
		}
	}

	// Parse pagination
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil {
			offset = parsed
		}
	}

	// Record user activity
	_ = RecordUserActivity(userID)

	// Fetch posts (this would normally query from posts table)
	// For now, using mock data
	posts := fetchTimelinePosts(userID, limit+offset)

	// Apply ranking
	rankedPosts := RankPosts(posts, rankingMode, userID)

	// Apply pagination
	total := len(rankedPosts)
	if offset >= total {
		rankedPosts = []RankedPost{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		rankedPosts = rankedPosts[offset:end]
	}

	response := TimelineResponse{
		Posts:       rankedPosts,
		RankingMode: rankingMode,
		Total:       total,
		HasMore:     offset+limit < total,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ============ Post Versioning Handlers ============

// EditPostHandler handles post editing with versioning
func EditPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PostID     string  `json:"post_id"`
		EditorID   string  `json:"editor_id"`
		NewContent string  `json:"new_content"`
		ChangeNote *string `json:"change_note,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get current version number
	currentVersion, err := GetLatestVersionNumber(req.PostID)
	if err != nil {
		http.Error(w, "Failed to get version", http.StatusInternalServerError)
		log.Printf("Error getting version: %v", err)
		return
	}

	newVersion := currentVersion + 1

	// Create version record
	if err := CreatePostVersion(req.PostID, req.EditorID, req.NewContent, newVersion, req.ChangeNote); err != nil {
		http.Error(w, "Failed to create version", http.StatusInternalServerError)
		log.Printf("Error creating version: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"version": newVersion,
		"message": "Post edited successfully",
	})
}

// GetPostVersionHistoryHandler retrieves version history for a post
func GetPostVersionHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	postID := r.URL.Query().Get("post_id")
	if postID == "" {
		http.Error(w, "post_id required", http.StatusBadRequest)
		return
	}

	versions, err := GetPostVersionHistory(postID)
	if err != nil {
		http.Error(w, "Failed to get version history", http.StatusInternalServerError)
		log.Printf("Error getting version history: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"post_id":  postID,
		"versions": versions,
		"count":    len(versions),
	})
}

// GetSpecificVersionHandler retrieves a specific version of a post
func GetSpecificVersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	postID := r.URL.Query().Get("post_id")
	versionStr := r.URL.Query().Get("version")

	if postID == "" || versionStr == "" {
		http.Error(w, "post_id and version required", http.StatusBadRequest)
		return
	}

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		http.Error(w, "Invalid version number", http.StatusBadRequest)
		return
	}

	postVersion, err := GetPostVersion(postID, version)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(postVersion)
}

// ============ Offline Mode Handlers ============

// CacheTimelineHandler caches timeline for offline access
func CacheTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user's offline config
	config, err := GetUserOfflineConfig(req.UserID)
	if err != nil {
		config = DefaultOfflineConfig()
	}

	// Fetch timeline posts
	posts := fetchTimelinePosts(req.UserID, config.MaxPostsPerUser)

	// Cache the posts
	if err := CacheTimelineForUser(req.UserID, posts, config); err != nil {
		http.Error(w, fmt.Sprintf("Failed to cache timeline: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"posts_count": len(posts),
		"expires_at":  time.Now().Add(config.CacheDuration),
	})
}

// GetCachedTimelineHandler retrieves cached timeline
func GetCachedTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}

	posts, err := GetCachedTimeline(userID)
	if err != nil {
		http.Error(w, "No cached data available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"posts": posts,
		"count": len(posts),
	})
}

// RefreshCacheHandler refreshes cached content
func RefreshCacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Fetch fresh timeline
	config, _ := GetUserOfflineConfig(req.UserID)
	posts := fetchTimelinePosts(req.UserID, config.MaxPostsPerUser)

	if err := RefreshCachedContent(req.UserID, posts); err != nil {
		http.Error(w, "Failed to refresh cache", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Cache refreshed",
	})
}

// ============ Adaptive Refresh Handlers ============

// GetRefreshIntervalHandler returns the adaptive refresh interval
func GetRefreshIntervalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}

	interval, err := CalculateAdaptiveInterval(userID)
	if err != nil {
		http.Error(w, "Failed to calculate interval", http.StatusInternalServerError)
		log.Printf("Error calculating interval: %v", err)
		return
	}

	config, _ := GetRefreshConfig(userID)
	activityLevel := GetActivityLevel(config.LastActivity)
	loadLevel, _ := GetCurrentLoadLevel()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":          userID,
		"interval_ms":      interval.Milliseconds(),
		"activity_level":   activityLevel,
		"load_level":       loadLevel,
		"last_activity":    config.LastActivity,
		"adaptive_enabled": config.AdaptiveEnabled,
	})
}

// UpdateActivityHandler updates user activity timestamp
func UpdateActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := RecordUserActivity(req.UserID); err != nil {
		http.Error(w, "Failed to update activity", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// RecordServerLoadHandler records server load metrics
func RecordServerLoadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var load ServerLoad
	if err := json.NewDecoder(r.Body).Decode(&load); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	load.Timestamp = time.Now()

	if err := RecordServerLoad(load); err != nil {
		http.Error(w, "Failed to record load", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// GetServerLoadHandler retrieves current server load level
func GetServerLoadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	loadLevel, err := GetCurrentLoadLevel()
	if err != nil {
		http.Error(w, "Failed to get load level", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"load_level": loadLevel,
		"timestamp":  time.Now(),
	})
}

// ============ Helper Functions ============

// fetchTimelinePosts is a placeholder for fetching posts from the database
// In production, this would query the posts table with joins to user data
func fetchTimelinePosts(userID string, limit int) []Post {
	// Mock data for demonstration
	// In production, this would fetch from the database
	posts := []Post{
		{
			ID:          "1",
			Author:      "user1",
			Content:     "First post!",
			CreatedAt:   time.Now().Add(-2 * time.Hour),
			UpdatedAt:   time.Now().Add(-2 * time.Hour),
			LikeCount:   42,
			ReplyCount:  5,
			RepostCount: 3,
			HasLiked:    false,
			HasReposted: false,
			IsEdited:    false,
			VersionNum:  1,
		},
		{
			ID:          "2",
			Author:      "user2",
			Content:     "Trending post with lots of engagement!",
			CreatedAt:   time.Now().Add(-30 * time.Minute),
			UpdatedAt:   time.Now().Add(-30 * time.Minute),
			LikeCount:   150,
			ReplyCount:  45,
			RepostCount: 30,
			HasLiked:    true,
			HasReposted: false,
			IsEdited:    false,
			VersionNum:  1,
		},
		{
			ID:          "3",
			Author:      "user3",
			Content:     "Recent post",
			CreatedAt:   time.Now().Add(-5 * time.Minute),
			UpdatedAt:   time.Now().Add(-5 * time.Minute),
			LikeCount:   8,
			ReplyCount:  2,
			RepostCount: 1,
			HasLiked:    false,
			HasReposted: false,
			IsEdited:    false,
			VersionNum:  1,
		},
	}

	if limit < len(posts) {
		return posts[:limit]
	}
	return posts
}
