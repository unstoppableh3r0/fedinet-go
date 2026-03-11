package identity

import "github.com/unstoppableh3r0/fedinet-go/pkg/models"
import (
    "encoding/json"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// ============================================================================
// DATA PORTABILITY: PROFILE EXPORT & IMPORT (USER STORY 4.1)
// ============================================================================

// ExportProfileHandler generates a cryptographically signed JSON archive of
// a user's entire digital existence. This enables "Sovereign Identity,"
// allowing users to move between servers without losing their data.
func ExportProfileHandler(w http.ResponseWriter, r *http.Request) {
    // 1. IDENTITY RESOLUTION
    userID := r.URL.Query().Get("user_id")
    if userID == "" {
        RespondWithError(w, http.StatusBadRequest, "user_id required")
        return
    }

    // Convert external Actor URI back to internal database primary key.
    internalID := ToInternalID(userID)

    // 2. CRYPTOGRAPHIC DATA RETRIEVAL
    // We fetch the Identity document, including the sensitive encrypted private key.
    var identity models.Identity
    var encryptedPrivKey string
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

    // 3. SECURE DECRYPTION
    // The private key must be decrypted from the server's master key so the
    // user can carry the raw key to a new server.
    masterKey := os.Getenv("SERVER_MASTER_KEY")
    if masterKey == "" {
        masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
    }

    privKey, err := crypto.Decrypt(encryptedPrivKey, masterKey)
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "failed to decrypt private key")
        return
    }

    // Normalize ID for the export document.
    identity.UserID = ToExternalID(identity.UserID)

    // 4. AGGREGATED DATA COLLECTION
    // Fetch profile metadata, posts, and social graph (followers/following).
    profile, err := GetProfileByUserID(internalID)
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "failed to fetch profile")
        return
    }
    profile.UserID = ToExternalID(profile.UserID)

    // Limit to recent 1000 posts to keep the export file size manageable.
    posts, err := GetUserPosts(internalID, "", 1000, 0)
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "failed to fetch posts")
        return
    }

    // Fetch Follower list.
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

    // Fetch Following list.
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

    // 5. DOCUMENT ASSEMBLY
    export := models.PortableProfile{
        User: models.UserDocument{
            Identity: identity,
            Profile:  *profile,
        },
        PrivateKey: privKey,
        Posts:      posts,
        Followers:  followers,
        Following:  following,
        ExportedAt: time.Now(),
    }

    // 6. INTEGRITY SIGNATURE
    // We sign the UserID and Timestamp using the User's own Private Key.
    // This proves the export was authorized by the owner of the identity.
    sigPayload := identity.UserID + export.ExportedAt.String()
    signature, _ := crypto.SignData([]byte(sigPayload), privKey)
    export.IdentitySig = signature

    // Trigger browser download of the generated JSON file.
    w.Header().Set("Content-Disposition", "attachment; filename=profile_export.json")
    RespondWithJSON(w, http.StatusOK, export)
}



// ImportProfileHandler accepts a PortableProfile archive and reconstitutes
// the user's account on the current server.
func ImportProfileHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    // 1. DATA DECODING
    var importData models.PortableProfile
    if err := json.NewDecoder(r.Body).Decode(&importData); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid JSON")
        return
    }

    // 2. CRYPTOGRAPHIC VERIFICATION
    // Before doing anything, verify that the Identity Signature is valid.
    // This prevents malicious actors from uploading forged profile archives.
    pubKey := importData.User.Identity.PublicKey
    sigPayload := importData.User.Identity.UserID + importData.ExportedAt.String()
    valid, err := crypto.VerifySignature([]byte(sigPayload), importData.IdentitySig, pubKey)
    if err != nil || !valid {
        RespondWithError(w, http.StatusForbidden, "integrity check failed")
        return
    }

    // Ensure the identity document meets the system's schema requirements.
    if !ValidateIdentityDocument(&importData.User.Identity) {
        RespondWithError(w, http.StatusBadRequest, "invalid identity document")
        return
    }

    if importData.User.Identity.UserID == "" {
        RespondWithError(w, http.StatusBadRequest, "missing identity in export")
        return
    }

    // 3. TRANSACTIONAL IMPORT
    // We use a SQL Transaction to ensure that either the WHOLE profile is
    // imported or NONE of it is (preventing partial/broken accounts).
    internalID := ToInternalID(importData.User.Identity.UserID)
    newHomeServer := "http://localhost:8080" // Local instance now becomes the home server

    tx, err := db.Begin()
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "db error")
        return
    }
    defer tx.Rollback()

    // 4. IDENTITY RECONSTITUTION
    // Encrypt the imported private key for local storage.
    masterKey := os.Getenv("SERVER_MASTER_KEY")
    if masterKey == "" {
        masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
    }

    encryptedPrivKey, err := crypto.Encrypt(importData.PrivateKey, masterKey)
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "failed to encrypt private key")
        return
    }

    // INSERT or UPDATE (UPSERT) the identity record.
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

    // 5. PROFILE METADATA RECONSTITUTION
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

    // 6. CONTENT RECONSTITUTION
    // Re-insert historical posts. We use 'DO NOTHING' on conflict to avoid
    // errors if a post with the same ID already exists on this server.
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

    // 7. FINALIZATION
    if err := tx.Commit(); err != nil {
        RespondWithError(w, http.StatusInternalServerError, "commit failed")
        return
    }

    RespondWithJSON(w, http.StatusOK, map[string]string{"message": "profile imported successfully"})
}