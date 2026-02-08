package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// HandleProfileUpdate processes incoming Update activities for profiles
func HandleProfileUpdate(activity *InboxActivity) error {
	// 1. Parse Payload
	// The payload should be the Object being updated (the Profile)
	// ActivityPub: { type: "Update", object: { type: "Person", id: "...", ... } }

	// For our specific protocol, let's assume payload IS the Profile fields or a partial
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(activity.Payload), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Check if object is provided and is a Person/Profile
	obj, ok := payload["object"].(map[string]interface{})
	if !ok {
		// Sometimes payload itself is the object in simplified implementations,
		// but ActivityPub specifies 'object' property for Update.
		// Let's support both for robustness or sticky to spec.
		// If "type" exists in top level and is "Person", maybe it's a direct representation?
		// But the InboxActivity wrapper separated them.
		// Let's assume standard { type: Update, object: { ... } } structure was passed to InboxActivity.Payload
		// Wait, InboxActivity.Payload IS the JSONB of the Activity.

		// Let's check "type" of the activity, which we know is "Update".
		// So we look for "object".
		return fmt.Errorf("invalid payload: missing object")
	}

	// 2. Extract Actor/ID
	// actorID := activity.ActorID // Who Sent it
	// objectID := obj["id"].(string) // What is being updated

	// In ActivityPub, Update.actor should match Update.object.id (for profiles)

	// 3. Resolve internal identity to update
	// We expect the actor to already exist if we are following them,
	// or we might strictly only process updates for known actors.

	// 4. Version Check
	// We look for a "version" or "updated" field to prevent replay attacks or out-of-order updates
	// Our Profile model has `Version`.

	newVersionFloat, ok := obj["version"].(float64) // JSON numbers are floats
	if !ok {
		// If no version, fallback to updated_at?
		// For MVP, require version to align with our schema
		// Or default to 0 and always update if we trust the source (strict mode might reject)
		log.Println("Warning: No version in Profile Update, proceeding with caution")
		newVersionFloat = 0
	}
	newVersion := int(newVersionFloat)

	// 5. Update Local Cache
	// We need to map the JSON fields back to our DB columns.
	// This looks VERY similar to UpdateProfile in identity/actions.go but for *remote* users
	// which effectively are stored in the same `profiles` table.

	// We need to find the user_id corresponding to the actor
	// The actorID (URL or Handle) needs to be mapped to our internal stored user_id (which might be the same string)

	// Let's assume actorID is the user_id in our DB for now.

	err := updateRemoteProfile(activity.ActorID, obj, newVersion)
	return err
}

func updateRemoteProfile(userID string, data map[string]interface{}, newVersion int) error {
	// Optimistic Concurrency Check: Only update if newVersion > currentVersion
	// Or if we don't have it (INSERT)

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

	// Map fields
	displayName, _ := data["display_name"].(string)
	bio, _ := data["bio"].(string)
	// ... extract other fields ...

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
		// Insert new cache entry
		// We might need to ensure Identity exists first?
		// usage of 'COALESCE' with empty strings handles partial updates if we send ""
		// But usually json nil vs "" is valid distinction.
		// For MVP simplified.

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
