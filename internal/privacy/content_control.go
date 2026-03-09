package privacy

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"
)

// PostPrivacyMetadata wraps all privacy constructs acting on a single post
type PostPrivacyMetadata struct {
	PostID         string    `json:"post_id"`
	IsAnonymous    bool      `json:"is_anonymous"`
	ContentWarning string    `json:"content_warning,omitempty"`
	Forwardable    bool      `json:"forwardable"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	ACL            []string  `json:"access_control_list"` // list of explicit allowed user IDs/domains
}

// User Story 3.5: Anonymous Posting Mode
func AnonymizePost(originalAuthor string, content string) (string, string) {
	// Strip author identity, replace with deterministic anonymous shadow-identity for thread continuity
	h := sha1.New()
	h.Write([]byte(originalAuthor + time.Now().Format("20060102")))
	shadowIdentity := "anon_" + hex.EncodeToString(h.Sum(nil))[:10]

	// Ensure the content itself doesn't contain leaking metadata
	cleanedContent := content // dummy clean

	return shadowIdentity, cleanedContent
}

// User Story 3.6: Content Warnings
func AttachWarning(postID, warningText string) error {
	// Implementation would persist the warning text into the post_metadata table
	return nil
}

// User Story 3.9: Forwarding Prevention
func SetNonForwardable(postID string) error {
	// Implementation would write 'forwardable=false' into DB and ensure the ActivityPub object is attributed appropriately
	return nil
}

// User Story 3.7: Per-Post Access Control
func EvaluatePostACL(viewerID string, acl []string) bool {
	if len(acl) == 0 {
		return true // No ACL = open within scope
	}

	for _, allowed := range acl {
		if viewerID == allowed {
			return true
		}
	}

	// Additional evaluation logic for domain-based ACL could go here
	fmt.Printf("User %s blocked from reading content based on strict ACL\n", viewerID)
	return false
}

// User Story 3.15: Self-Deleting Posts
func EnsureExpirationCron() {
	// Starts a background ticker that hunts for expired posts and deletes them
	<-time.After(1 * time.Hour)
	// execute DB cleanup query WHERE expires_at < NOW()
}

func QueueForDeletion(postID string, expiresAt time.Time) error {
	// Update post record in DB with the expiration timestamp
	return nil
}
