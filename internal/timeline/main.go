package main

import (
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	
	if err := godotenv.Load("../../.env"); err != nil {
		log.Println("Warning: .env file not found, using default configuration")
	}

	
	InitDB()
	ApplyMigrations()

	
	go cleanupTask()
	go metricsCleanupTask()

	
	http.HandleFunc("/timeline/ranking/preference", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetRankingPreferenceHandler(w, r)
		} else if r.Method == http.MethodPost {
			SetRankingPreferenceHandler(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/timeline", GetTimelineHandler)

	
	http.HandleFunc("/timeline/post/edit", EditPostHandler)
	http.HandleFunc("/timeline/post/versions", GetPostVersionHistoryHandler)
	http.HandleFunc("/timeline/post/version", GetSpecificVersionHandler)

	
	http.HandleFunc("/timeline/cache", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			CacheTimelineHandler(w, r)
		} else if r.Method == http.MethodGet {
			GetCachedTimelineHandler(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/timeline/cache/refresh", RefreshCacheHandler)

	
	http.HandleFunc("/timeline/refresh/interval", GetRefreshIntervalHandler)
	http.HandleFunc("/timeline/activity/update", UpdateActivityHandler)
	http.HandleFunc("/timeline/server/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			RecordServerLoadHandler(w, r)
		} else if r.Method == http.MethodGet {
			GetServerLoadHandler(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	log.Println("Timeline service running on :8081")
	log.Println("Endpoints:")
	log.Println("  GET/POST /timeline/ranking/preference - Get/set ranking preferences")
	log.Println("  GET /timeline - Get ranked timeline")
	log.Println("  POST /timeline/post/edit - Edit post with versioning")
	log.Println("  GET /timeline/post/versions - Get version history")
	log.Println("  GET /timeline/post/version - Get specific version")
	log.Println("  GET/POST /timeline/cache - Get/create offline cache")
	log.Println("  POST /timeline/cache/refresh - Refresh cache")
	log.Println("  GET /timeline/refresh/interval - Get adaptive refresh interval")
	log.Println("  POST /timeline/activity/update - Update user activity")
	log.Println("  GET/POST /timeline/server/load - Get/record server load")

	log.Fatal(http.ListenAndServe(":8081", enableCORS(http.DefaultServeMux)))
}


func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}


func cleanupTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Running cache cleanup task...")
		if err := CleanExpiredCaches(); err != nil {
			log.Printf("Cache cleanup error: %v", err)
		}
	}
}


func metricsCleanupTask() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Running metrics cleanup task...")
		if err := CleanOldMetrics(); err != nil {
			log.Printf("Metrics cleanup error: %v", err)
		}
	}
}
