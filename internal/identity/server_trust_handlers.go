package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TrustedServer represents a server in the federation
type TrustedServer struct {
	ID         string    `json:"id"`
	ServerID   string    `json:"server_id"`
	ServerName string    `json:"server_name"`
	PublicKey  string    `json:"public_key"`
	Endpoint   string    `json:"endpoint"`
	TrustedAt  time.Time `json:"trusted_at"`
}

// AddTrustedServerRequest represents the request to add a trusted server
type AddTrustedServerRequest struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	PublicKey  string `json:"public_key"`
	Endpoint   string `json:"endpoint"`
}

// GetTrustedServersHandler returns all trusted servers
func GetTrustedServersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT id, server_id, server_name, public_key, endpoint, trusted_at
		FROM trusted_servers
		ORDER BY trusted_at DESC
	`)
	if err != nil {
		log.Printf("Failed to fetch trusted servers: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch trusted servers")
		return
	}
	defer rows.Close()

	var servers []TrustedServer
	for rows.Next() {
		var s TrustedServer
		err := rows.Scan(&s.ID, &s.ServerID, &s.ServerName, &s.PublicKey, &s.Endpoint, &s.TrustedAt)
		if err != nil {
			log.Printf("Failed to scan trusted server:  %v", err)
			continue
		}
		servers = append(servers, s)
	}

	if servers == nil {
		servers = []TrustedServer{}
	}

	RespondWithJSON(w, http.StatusOK, servers)
}

// AddTrustedServerHandler adds a new trusted server via automatic handshake
func AddTrustedServerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req AddTrustedServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validation - public key is no longer required!
	if req.ServerID == "" || req.ServerName == "" || req.Endpoint == "" {
		RespondWithError(w, http.StatusBadRequest, "server_id, server_name, and endpoint are required")
		return
	}

	// Check if server already exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM trusted_servers WHERE server_id = $1)", req.ServerID).Scan(&exists)
	if err != nil {
		log.Printf("Failed to check server existence: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to check server existence")
		return
	}

	if exists {
		RespondWithError(w, http.StatusConflict, "server already exists in trusted list")
		return
	}

	// Initiate handshake with the remote server
	log.Printf("Initiating handshake with %s (%s)", req.ServerName, req.Endpoint)
	handshakeResp, err := InitiateHandshake(req.Endpoint, req.ServerName)
	if err != nil {
		log.Printf("Handshake failed with %s: %v", req.ServerName, err)
		RespondWithError(w, http.StatusBadGateway, fmt.Sprintf("handshake failed: %v", err))
		return
	}

	// Store the server with the public key received from handshake
	id := uuid.New()
	_, err = db.Exec(`
		INSERT INTO trusted_servers (id, server_id, server_name, public_key, endpoint, trusted_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, id, req.ServerID, req.ServerName, handshakeResp.PublicKey, req.Endpoint)

	if err != nil {
		log.Printf("Failed to add trusted server: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to add trusted server")
		return
	}

	log.Printf("✅ Handshake complete! Added trusted server:  %s (%s)", req.ServerName, req.ServerID)

	RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"success":    true,
		"server_id":  req.ServerID,
		"public_key": handshakeResp.PublicKey,
		"message":    "Handshake completed and server added successfully",
	})
}

// UpdateTrustedServerHandler updates an existing trusted server
func UpdateTrustedServerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ServerID   string `json:"server_id"`
		ServerName string `json:"server_name"`
		PublicKey  string `json:"public_key"`
		Endpoint   string `json:"endpoint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "server_id is required")
		return
	}

	result, err := db.Exec(`
		UPDATE trusted_servers
		SET server_name = $1, public_key = $2, endpoint = $3
		WHERE server_id = $4
	`, req.ServerName, req.PublicKey, req.Endpoint, req.ServerID)

	if err != nil {
		log.Printf("Failed to update trusted server: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to update trusted server")
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		RespondWithError(w, http.StatusNotFound, "server not found")
		return
	}

	log.Printf("✅ Updated trusted server: %s", req.ServerID)

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Trusted server updated successfully",
	})
}

// RemoveTrustedServerHandler removes a trusted server
func RemoveTrustedServerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverID := r.URL.Query().Get("server_id")
	if serverID == "" {
		RespondWithError(w, http.StatusBadRequest, "server_id query parameter is required")
		return
	}

	result, err := db.Exec("DELETE FROM trusted_servers WHERE server_id = $1", serverID)
	if err != nil {
		log.Printf("Failed to remove trusted server: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to remove trusted server")
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		RespondWithError(w, http.StatusNotFound, "server not found")
		return
	}

	log.Printf("⚠️  Removed trusted server: %s", serverID)

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Trusted server removed successfully",
	})
}

// GetServerPublicKeyHandler returns the public key of a specific trusted server
func GetServerPublicKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverID := r.URL.Query().Get("server_id")
	if serverID == "" {
		RespondWithError(w, http.StatusBadRequest, "server_id query parameter is required")
		return
	}

	var publicKey string
	var serverName string
	err := db.QueryRow(`
		SELECT public_key, server_name 
		FROM trusted_servers 
		WHERE server_id = $1
	`, serverID).Scan(&publicKey, &serverName)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusNotFound, "server not found in trusted list")
		return
	}

	if err != nil {
		log.Printf("Failed to fetch server public key: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch server public key")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"server_id":   serverID,
		"server_name": serverName,
		"public_key":  publicKey,
	})
}

// TestTrustedServerConnectionHandler verifies connectivity to a trusted server from the backend
func TestTrustedServerConnectionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Endpoint string `json:"endpoint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Endpoint == "" {
		RespondWithError(w, http.StatusBadRequest, "endpoint is required")
		return
	}

	// Create a client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Perform GET request to the health endpoint
	resp, err := client.Get(req.Endpoint + "/health")
	if err != nil {
		log.Printf("Failed to connect to %s: %v", req.Endpoint, err)
		RespondWithError(w, http.StatusBadGateway, fmt.Sprintf("failed to connect: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		RespondWithError(w, http.StatusBadGateway, fmt.Sprintf("server responded with status: %d", resp.StatusCode))
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Successfully connected to server",
	})
}
