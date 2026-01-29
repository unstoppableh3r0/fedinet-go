package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func FollowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Follower string `json:"follower"`
		Followee string `json:"followee"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Follower == "" || req.Followee == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	if err := FollowUser(req.Follower, req.Followee); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("followed"))
}

func MessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.To == "" || req.Content == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	if err := SendMessage(req.From, req.To, req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("message sent"))
}

func UserSearchHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Println("PANIC recovered:", rec)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()

	log.Println("---- /user/search HIT ----")

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "missing user_id", http.StatusBadRequest)
		return
	}

	identity, err := GetIdentityByUserID(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if identity == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !identity.AllowDiscovery {
		http.Error(w, "profile unavailable", http.StatusForbidden)
		return
	}

	// ✅ REAL profile from PostgreSQL
	profile, err := GetProfileByUserID(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}

	doc := UserDocument{
		Identity: *identity, // kept for later crypto use
		Profile:  *profile,  // UI-safe
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func strPtr(s string) *string {
	return &s
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"` // Ignored for simulation
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	// For simulation, we assume local home server
	homeServer := "http://localhost:8080"

	if err := CreateAccount(req.Username, homeServer); err != nil {
		log.Println("Registration failed:", err)
		if err.Error() == "user already exists" {
			http.Error(w, "username taken", http.StatusConflict)
			return
		}
		http.Error(w, "internal registration error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"user_id":     req.Username,
		"home_server": homeServer,
	})
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Get UserID from query param as a "dummy auth token"
	// In a real app, this would come from a JWT or Session Cookie
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "unauthorized: missing user_id param", http.StatusUnauthorized)
		return
	}

	// 2. Fetch Identity
	identity, err := GetIdentityByUserID(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if identity == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// 3. Fetch Profile
	profile, err := GetProfileByUserID(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Profile should exist if Identity exists (Atomic guarantee), but handle just in case
	if profile == nil {
		http.Error(w, "profile missing (integrity error)", http.StatusInternalServerError)
		return
	}

	// 4. Return combined document
	doc := UserDocument{
		Identity: *identity,
		Profile:  *profile,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}
