package timeline

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

)




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

	
	rankingMode := RankingMode(r.URL.Query().Get("ranking_mode"))
	if rankingMode == "" {
		
		var err error
		rankingMode, err = GetUserRankingPreference(userID)
		if err != nil {
			rankingMode = RankingModeChronological 
		}
	}

	
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

	
	_ = RecordUserActivity(userID)

	
	
	posts := fetchTimelinePosts(userID, limit+offset)

	
	rankedPosts := RankPosts(posts, rankingMode, userID)

	
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

	
	currentVersion, err := GetLatestVersionNumber(req.PostID)
	if err != nil {
		http.Error(w, "Failed to get version", http.StatusInternalServerError)
		log.Printf("Error getting version: %v", err)
		return
	}

	newVersion := currentVersion + 1

	
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

	
	config, err := GetUserOfflineConfig(req.UserID)
	if err != nil {
		config = DefaultOfflineConfig()
	}

	
	posts := fetchTimelinePosts(req.UserID, config.MaxPostsPerUser)

	
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





func fetchTimelinePosts(userID string, limit int) []Post {
	
	
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
