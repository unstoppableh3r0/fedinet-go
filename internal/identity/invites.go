package identity

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/skip2/go-qrcode"
)





type Invite struct {
	ID          string     `json:"id"`
	InviteCode  string     `json:"invite_code"`
	InviteType  string     `json:"invite_type"`
	CreatedBy   string     `json:"created_by"`
	MaxUses     int        `json:"max_uses"`
	CurrentUses int        `json:"current_uses"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Revoked     bool       `json:"revoked"`
	CreatedAt   time.Time  `json:"created_at"`
	Metadata    string     `json:"metadata"` 
}

type GenerateInviteRequest struct {
	InviteType string `json:"invite_type"` 
	MaxUses    int    `json:"max_uses"`    
	ExpiresIn  int    `json:"expires_in"`  
}





func generateInviteCode() (string, error) {
	b := make([]byte, 6) 
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func GenerateInvite(req GenerateInviteRequest, createdBy string) (*Invite, error) {
	if req.InviteType != "user" && req.InviteType != "admin" {
		return nil, errors.New("invalid invite_type")
	}

	code, err := generateInviteCode()
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * time.Hour)
		expiresAt = &t
	}

	var inviteID string
	var createdAt time.Time

	err = db.QueryRow(`
		INSERT INTO invites (invite_code, invite_type, created_by, max_uses, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, code, req.InviteType, createdBy, req.MaxUses, expiresAt).Scan(&inviteID, &createdAt)

	if err != nil {
		return nil, err
	}

	return &Invite{
		ID:          inviteID,
		InviteCode:  code,
		InviteType:  req.InviteType,
		CreatedBy:   createdBy,
		MaxUses:     req.MaxUses,
		CurrentUses: 0,
		ExpiresAt:   expiresAt,
		Revoked:     false,
		CreatedAt:   createdAt,
	}, nil
}

func ValidateInvite(code string) (*Invite, error) {
	var i Invite
	var expiresAt sql.NullTime

	err := db.QueryRow(`
		SELECT id, invite_code, invite_type, created_by, max_uses, current_uses, expires_at, revoked, created_at
		FROM invites
		WHERE invite_code = $1
	`, code).Scan(
		&i.ID, &i.InviteCode, &i.InviteType, &i.CreatedBy,
		&i.MaxUses, &i.CurrentUses, &expiresAt, &i.Revoked, &i.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, errors.New("invalid invite code")
	}
	if err != nil {
		return nil, err
	}

	if expiresAt.Valid {
		i.ExpiresAt = &expiresAt.Time
	}

	if i.Revoked {
		return nil, errors.New("invite revoked")
	}

	if i.ExpiresAt != nil && time.Now().After(*i.ExpiresAt) {
		return nil, errors.New("invite expired")
	}

	if i.MaxUses != -1 && i.CurrentUses >= i.MaxUses {
		return nil, errors.New("invite usage limit reached")
	}

	return &i, nil
}

func UseInvite(code string, userID string, ip string, userAgent string) error {
	
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	
	var inviteID string
	err = tx.QueryRow(`
		SELECT id FROM invites WHERE invite_code = $1 FOR UPDATE
	`, code).Scan(&inviteID)
	if err != nil {
		return err
	}

	
	_, err = tx.Exec(`
		UPDATE invites SET current_uses = current_uses + 1 WHERE id = $1
	`, inviteID)
	if err != nil {
		return err
	}

	
	_, err = tx.Exec(`
		INSERT INTO invite_usage (invite_id, user_id, ip_address, user_agent)
		VALUES ($1, $2, $3, $4)
	`, inviteID, userID, ip, userAgent)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func RevokeInvite(code string) error {
	result, err := db.Exec("UPDATE invites SET revoked = true WHERE invite_code = $1", code)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("invite not found")
	}
	return nil
}

func ListInvites() ([]Invite, error) {
	rows, err := db.Query(`
		SELECT id, invite_code, invite_type, created_by, max_uses, current_uses, expires_at, revoked, created_at
		FROM invites
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invites := make([]Invite, 0)
	for rows.Next() {
		var i Invite
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&i.ID, &i.InviteCode, &i.InviteType, &i.CreatedBy,
			&i.MaxUses, &i.CurrentUses, &expiresAt, &i.Revoked, &i.CreatedAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			i.ExpiresAt = &expiresAt.Time
		}
		invites = append(invites, i)
	}
	return invites, nil
}





func GenerateInviteQR(code string) ([]byte, error) {
	
	invite, err := ValidateInvite(code)
	if err != nil {
		return nil, err
	}

	
	var serverID, serverName, publicKey string
	err = db.QueryRow("SELECT server_id, server_name, public_key FROM server_identity WHERE id = 1").
		Scan(&serverID, &serverName, &publicKey)
	if err != nil {
		return nil, err
	}

	
	payload := map[string]string{
		"server_url":  "http://localhost:8082", 
		"server_id":   serverID,
		"server_name": serverName,
		"public_key":  publicKey,
		"invite_code": code,
		"invite_type": invite.InviteType,
		"action":      "FederatedInvite",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	
	return qrcode.Encode(string(jsonData), qrcode.Medium, 256)
}
