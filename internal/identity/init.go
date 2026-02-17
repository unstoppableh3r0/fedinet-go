package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)





type ServerInitRequest struct {
	ServerName    string `json:"server_name"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
}

type ServerInitResponse struct {
	Message    string `json:"message"`
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	PublicKey  string `json:"public_key"`
}

type ServerStatusResponse struct {
	Initialized bool   `json:"initialized"`
	ServerName  string `json:"server_name,omitempty"`
	ServerID    string `json:"server_id,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
}





func CheckInitializationStatus() (bool, error) {
	var initialized bool
	err := db.QueryRow("SELECT initialized FROM server_identity WHERE id = 1").Scan(&initialized)

	if err == sql.ErrNoRows {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return initialized, nil
}





func InitializeServer(req ServerInitRequest) (*ServerInitResponse, error) {
	
	if req.ServerName == "" {
		return nil, errors.New("server_name is required")
	}
	if req.AdminUsername == "" {
		return nil, errors.New("admin_username is required")
	}
	if req.AdminPassword == "" {
		return nil, errors.New("admin_password is required")
	}
	if len(req.AdminPassword) < 8 {
		return nil, errors.New("admin_password must be at least 8 characters")
	}

	
	initialized, err := CheckInitializationStatus()
	if err != nil {
		return nil, err
	}
	if initialized {
		return nil, errors.New("server already initialized")
	}

	
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	
	serverID := uuid.New()

	
	publicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)
	privateKeyB64 := base64.StdEncoding.EncodeToString(privateKey)

	
	
	encryptedPrivateKey := privateKeyB64

	
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	
	_, err = tx.Exec(`
		INSERT INTO server_identity (id, server_id, server_name, public_key, private_key_encrypted)
		VALUES (1, $1, $2, $3, $4)
	`, serverID, req.ServerName, publicKeyB64, encryptedPrivateKey)

	if err != nil {
		return nil, err
	}

	
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	
	_, err = tx.Exec(`
		INSERT INTO admins (username, password_hash, is_super_admin, created_by)
		VALUES ($1, $2, true, NULL)
	`, req.AdminUsername, string(passwordHash))

	if err != nil {
		return nil, err
	}

	
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	log.Printf("✅ Server initialized: %s (ID: %s)", req.ServerName, serverID)
	log.Printf("✅ Super admin created: %s", req.AdminUsername)

	return &ServerInitResponse{
		Message:    "Server initialized successfully",
		ServerID:   serverID.String(),
		ServerName: req.ServerName,
		PublicKey:  publicKeyB64,
	}, nil
}





func StatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	initialized, err := CheckInitializationStatus()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to check status")
		return
	}

	response := ServerStatusResponse{
		Initialized: initialized,
	}

	
	if initialized {
		var serverID, serverName, publicKey string
		err := db.QueryRow(`
			SELECT server_id, server_name, public_key
			FROM server_identity
			WHERE id = 1
		`).Scan(&serverID, &serverName, &publicKey)

		if err == nil {
			response.ServerID = serverID
			response.ServerName = serverName
			response.PublicKey = publicKey
		}
	}

	RespondWithJSON(w, http.StatusOK, response)
}

func InitializeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ServerInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	response, err := InitializeServer(req)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, response)
}





func GetServerInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var serverID, serverName, publicKey string
	err := db.QueryRow(`
		SELECT server_id, server_name, public_key
		FROM server_identity
		WHERE id = 1
	`).Scan(&serverID, &serverName, &publicKey)

	if err == sql.ErrNoRows {
		RespondWithError(w, http.StatusServiceUnavailable, "server not initialized")
		return
	}

	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch server info")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{
		"server_id":   serverID,
		"server_name": serverName,
		"public_key":  publicKey,
		"version":     "1.0.0",
	})
}
