package federation

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)


func HandleProfileUpdate(activity *models.InboxActivity) error {
	
	
	

	
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(activity.Payload), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	
	obj, ok := payload["object"].(map[string]interface{})
	if !ok {
		
		
		
		
		
		
		

		
		
		return fmt.Errorf("invalid payload: missing object")
	}

	
	
	

	

	
	
	

	
	
	

	newVersionFloat, ok := obj["version"].(float64) 
	if !ok {
		
		
		
		log.Println("Warning: No version in Profile Update, proceeding with caution")
		newVersionFloat = 0
	}
	newVersion := int(newVersionFloat)

	
	
	
	

	
	

	

	err := updateRemoteProfile(activity.ActorID, obj, newVersion)
	return err
}

func updateRemoteProfile(userID string, data map[string]interface{}, newVersion int) error {
	
	

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentVersion int
	var exists bool
	err = tx.QueryRow("SELECT version FROM profiles WHERE user_id=$1", userID).Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		exists = true
	}

	if exists && newVersion <= currentVersion {
		return fmt.Errorf("update ignored: stale version (current: %d, new: %d)", currentVersion, newVersion)
	}

	
	displayName, _ := data["display_name"].(string)
	bio, _ := data["bio"].(string)
	

	if exists {
		_, err = tx.Exec(`
            UPDATE profiles 
            SET display_name=COALESCE(NULLIF($1, ''), display_name), 
                bio=COALESCE(NULLIF($2, ''), bio),
                version=$3, 
                updated_at=NOW()
            WHERE user_id=$4
        `, displayName, bio, newVersion, userID)
	} else {
		
		
		
		
		

		_, err = tx.Exec(`
            INSERT INTO profiles (user_id, display_name, bio, version, created_at, updated_at, followers_visibility, following_visibility)
            VALUES ($1, $2, $3, $4, NOW(), NOW(), 'public', 'public')
        `, userID, displayName, bio, newVersion)
	}

	if err != nil {
		return err
	}

	return tx.Commit()
}
