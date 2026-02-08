package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ExportProfileHandler generates a JSON bundle of the user's data
func ExportProfileHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		RespondWithError(w, http.StatusBadRequest, "user_id required")
		return
	}

	internalID := ToInternalID(userID)

	// 1. Fetch Identity (with PrivateKey)
	var identity Identity
	var encryptedPrivKey string // Store encrypted first
	err := db.QueryRow(`
        SELECT id, user_id, home_server, public_key, private_key, allow_discovery, created_at, updated_at, key_version, recovery_key_hash
        FROM identities WHERE user_id=$1
    `, internalID).Scan(
		&identity.ID, &identity.UserID, &identity.HomeServer, &identity.PublicKey, &encryptedPrivKey,
		&identity.AllowDiscovery, &identity.CreatedAt, &identity.UpdatedAt,
		&identity.KeyVersion, &identity.RecoveryKeyHash,
	)
	if err != nil {
		RespondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	// Decrypt Private Key
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	privKey, err := crypto.Decrypt(encryptedPrivKey, masterKey)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to decrypt private key")
		return
	}

	identity.UserID = ToExternalID(identity.UserID) // Export external format

	// 2. Fetch Profile
	profile, err := GetProfileByUserID(internalID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch profile")
		return
	}
	profile.UserID = ToExternalID(profile.UserID)

	// 3. Fetch Posts
	posts, err := GetUserPosts(internalID, "", 1000, 0) // Limit 1000 for MVP
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch posts")
		return
	}

	// 4. Fetch Graph (Followers/Following)
	// Simplified: Just returning lists of IDs
	followers := []string{}
	rows, err := db.Query("SELECT follower_user_id FROM follows WHERE followee_user_id=$1", internalID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var f string
			if rows.Scan(&f) == nil {
				followers = append(followers, ToExternalID(f))
			}
		}
	}

	following := []string{}
	rows2, err := db.Query("SELECT followee_user_id FROM follows WHERE follower_user_id=$1", internalID)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var f string
			if rows2.Scan(&f) == nil {
				following = append(following, ToExternalID(f))
			}
		}
	}

	// 5. Construct Bundle
	export := PortableProfile{
		User: UserDocument{
			Identity: identity,
			Profile:  *profile,
		},
		PrivateKey: privKey, // EXTREME CAUTION: In real app, MUST be encrypted
		Posts:      posts,
		Followers:  followers,
		Following:  following,
		ExportedAt: time.Now(),
	}

	// 6. Sign Bundle Integrity
	// Sign the export timestamp + userID with private key to prove origin
	sigPayload := identity.UserID + export.ExportedAt.String()
	signature, _ := crypto.SignData([]byte(sigPayload), privKey)
	export.IdentitySig = signature

	w.Header().Set("Content-Disposition", "attachment; filename=profile_export.json")
	RespondWithJSON(w, http.StatusOK, export)
}

// ImportProfileHandler ingests a JSON bundle
func ImportProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var importData PortableProfile
	if err := json.NewDecoder(r.Body).Decode(&importData); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// 1. Verify Integrity
	// Verify signature using the public key in the import
	pubKey := importData.User.Identity.PublicKey
	sigPayload := importData.User.Identity.UserID + importData.ExportedAt.String()
	valid, err := crypto.VerifySignature([]byte(sigPayload), importData.IdentitySig, pubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusForbidden, "integrity check failed")
		return
	}

	// 1.5 Validate Identity Document Structure
	if !ValidateIdentityDocument(&importData.User.Identity) {
		RespondWithError(w, http.StatusBadRequest, "invalid identity document")
		return
	}

	// 2. Import Identity
	// We treat this as a "Migration" or "Restore"
	// For migration, we might want to update the HomeServer in the Identity to THIS server?
	// User Story 1.14 says "Allow importing Identity to new server using Recovery Key"
	// Here we have the PRIVATE KEY. This is the "God Mode" import.

	// Check if user exists locally
	// Identity is a struct value, checking UserID presence
	if importData.User.Identity.UserID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing identity in export")
		return
	}
	internalID := ToInternalID(importData.User.Identity.UserID)
	newHomeServer := "http://localhost:8080" // Current server

	tx, err := db.Begin()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback()

	// Encrypt Private Key
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	encryptedPrivKey, err := crypto.Encrypt(importData.PrivateKey, masterKey)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to encrypt private key")
		return
	}

	// Upsert Identity
	// We update HomeServer to US
	_, err = tx.Exec(`
        INSERT INTO identities (
            id, user_id, home_server, public_key, private_key, 
            key_version, recovery_key_hash, allow_discovery, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
        ON CONFLICT (user_id) DO UPDATE SET
            home_server = EXCLUDED.home_server,
            private_key = EXCLUDED.private_key,
            public_key = EXCLUDED.public_key,
            updated_at = NOW()
    `,
		importData.User.Identity.ID,
		internalID,
		newHomeServer,
		importData.User.Identity.PublicKey,
		encryptedPrivKey,
		importData.User.Identity.KeyVersion,
		importData.User.Identity.RecoveryKeyHash,
		importData.User.Identity.AllowDiscovery,
		importData.User.Identity.CreatedAt,
	)
	if err != nil {
		log.Println("Import identity failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to import identity")
		return
	}

	// Upsert Profile
	p := importData.User.Profile
	_, err = tx.Exec(`
        INSERT INTO profiles (
            user_id, display_name, bio, avatar_url, banner_url,
            created_at, updated_at, version
        ) VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
        ON CONFLICT (user_id) DO UPDATE SET
            display_name = EXCLUDED.display_name,
            bio = EXCLUDED.bio,
            avatar_url = EXCLUDED.avatar_url,
            banner_url = EXCLUDED.banner_url,
            version = EXCLUDED.version,
            updated_at = NOW()
    `, internalID, p.DisplayName, p.Bio, p.AvatarURL, p.BannerURL, p.CreatedAt, p.Version)

	if err != nil {
		log.Println("Import profile failed:", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to import profile")
		return
	}

	// Import Posts (Optional/Best Effort)
	for _, post := range importData.Posts {
		_, err = tx.Exec(`
            INSERT INTO posts (id, author, content, created_at, updated_at)
            VALUES ($1, $2, $3, $4, $5)
            ON CONFLICT (id) DO NOTHING
        `, post.ID, internalID, post.Content, post.CreatedAt, post.UpdatedAt)
		if err != nil {
			log.Printf("Failed to import post %s: %v", post.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	// Broadcast "Move" activity?
	// If homeserver changed, we should notify previous followers.
	// This requires the "Migration" logic from User Story 1.14 (implied)
	// For now, we just restore state.

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "profile imported successfully"})
}
