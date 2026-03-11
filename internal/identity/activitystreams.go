package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// FEDERATION PROTOCOL: ACTIVITYSTREAMS 2.0 (AS2) IMPLEMENTATION
// ============================================================================

// AS2Context is the standard ActivityStreams 2.0 JSON-LD context.
// This URI tells remote servers (like Mastodon, Pleroma, or Lemmy) how to
// interpret the semantic meaning of the JSON fields in our messages.
const AS2Context = "https://www.w3.org/ns/activitystreams"

// AS2Object represents the "Object" or "Target" of an activity.
// In AS2, almost everything is an object—a Person, a Note (post), or a Service.
type AS2Object struct {
	// Type defines what the object is (e.g., "Person", "Note", "Service").
	Type string `json:"type"`

	// ID is the globally unique URI for this object (e.g., https://server.com/users/alice).
	ID string `json:"id,omitempty"`

	// Name is the display name or title of the object.
	Name string `json:"name,omitempty"`

	// Content contains the actual body text, often HTML-formatted for federation.
	Content string `json:"content,omitempty"`

	// InReplyTo is a functional property used for threading.
	// It points to the ID of the parent post being replied to.
	InReplyTo string `json:"inReplyTo,omitempty"`
}

// AS2Activity serves as the "Envelope" for a federated message.
// It describes WHO did WHAT to WHOM and WHEN.
type AS2Activity struct {
	// @context links this document to the ActivityStreams vocabulary.
	Context string `json:"@context"`

	// ID is the unique URI for this specific activity instance.
	ID string `json:"id"`

	// Type is the transitive verb of the action (e.g., "Create", "Follow", "Like").
	Type string `json:"type"`

	// Actor is the entity (Person/Service) performing the action.
	Actor AS2Object `json:"actor"`

	// Object is the primary entity the action is being performed upon.
	Object AS2Object `json:"object"`

	// Summary is a natural language description, often used for notifications.
	Summary string `json:"summary"`

	// Published follows the ISO 8601 / RFC 3339 timestamp format.
	Published string `json:"published"`
}

// internalTypeToAS2 performs the semantic mapping between the Fedinet-Go
// internal database "verbs" and the W3C standardized Activity types.
//
// Mapping Logic:
// - FOLLOW   → Follow:   Standard subscription request.
// - LIKE     → Like:     Positional engagement with content.
// - REPLY    → Create:   A reply is actually the 'Creation' of a 'Note'.
// - REPOST   → Announce: Known as a 'Boost' or 'Reblog' in other software.
// - UNFOLLOW → Undo:     Terminates a previous Follow relationship.
// - MESSAGE  → Create:   Private messaging via the inbox/outbox flow.
func internalTypeToAS2(internalType string) string {
	switch internalType {
	case "FOLLOW":
		return "Follow"
	case "LIKE":
		return "Like"
	case "REPLY":
		return "Create"
	case "REPOST":
		return "Announce"
	case "UNFOLLOW":
		return "Undo"
	case "SERVER_UPDATE":
		return "Update"
	case "MESSAGE":
		return "Create"
	default:
		// Fallback to generic Activity if the type is unknown to prevent total failure.
		return "Activity"
	}
}

// serverBaseURL determines the root of the identity URIs.
// This is vital because federated IDs must be absolute URLs (FQDN).
func serverBaseURL() string {
	u := os.Getenv("SERVER_URL")
	if u == "" {
		// Defaulting to localhost is acceptable for development, but
		// federation will fail in production without a public HTTPS URL.
		u = "http://localhost:8080"
	}
	return u
}

// BuildActivityStream is the factory function that transforms internal
// system events into valid, signable JSON-LD for the Fediverse.
//
// Workflow:
// 1. Resolve internal verb to AS2 Type.
// 2. Generate a unique Activity ID using UUID v4.
// 3. Construct the Actor object (Person or System Service).
// 4. Use a switch-case to build the Object based on the specific action logic.
// 5. Package into the AS2Activity envelope and marshal to JSON.
func BuildActivityStream(actorID, typeStr, entityID string, extras map[string]interface{}) ([]byte, error) {
	as2Type := internalTypeToAS2(typeStr)
	// Every activity needs a unique URI so it can be 'Deduplicated' by the receiving server.
	activityID := fmt.Sprintf("%s/activities/%s", serverBaseURL(), uuid.New().String())

	// ── Actor Construction ───────────────────────────────────────────────────
	// If the actor is "system", we use the "Service" type (common for automated updates).
	// Otherwise, we treat them as a "Person".
	actorType := "Person"
	if actorID == "system" {
		actorType = "Service"
	}
	actor := AS2Object{Type: actorType, ID: actorID}

	// ── Object Construction Logic ────────────────────────────────────────────
	var obj AS2Object
	var summary string

	switch typeStr {
	case "FOLLOW":
		// In a Follow activity, the 'Object' is the URI of the person being followed.
		obj = AS2Object{Type: "Person", ID: entityID}
		summary = fmt.Sprintf("%s followed you", actorID)

	case "LIKE":
		// In a Like activity, the 'Object' is the URI of the post (Note).
		obj = AS2Object{Type: "Note", ID: entityID}
		summary = fmt.Sprintf("%s liked your post", actorID)

	case "REPLY":
		// A Reply is a 'Note' that references a parent ID via 'inReplyTo'.
		obj = AS2Object{Type: "Note", ID: entityID}
		if c, ok := extras["content"].(string); ok {
			obj.Content = c
		}
		if p, ok := extras["parent_id"].(string); ok && p != "" {
			obj.InReplyTo = p
		} else {
			// Fallback: If no parent_id is provided, reference the entityID.
			obj.InReplyTo = entityID
		}
		summary = fmt.Sprintf("%s replied to your post", actorID)

	case "REPOST":
		// Announce activities essentially 'wrap' the original post URI.
		obj = AS2Object{Type: "Note", ID: entityID}
		summary = fmt.Sprintf("%s reposted your post", actorID)

	case "UNFOLLOW":
		// UNFOLLOW is an 'Undo' of a 'Follow'.
		// We represent the inner Follow object to tell the remote server
		// exactly which previous activity is being revoked.
		innerFollow, _ := json.Marshal(map[string]interface{}{
			"type":   "Follow",
			"actor":  actor,
			"object": AS2Object{Type: "Person", ID: entityID},
		})
		obj = AS2Object{Type: "Follow", Content: string(innerFollow)}
		summary = fmt.Sprintf("%s unfollowed you", actorID)

	case "SERVER_UPDATE":
		// Used for broadcasting instance-wide changes.
		name := entityID
		if n, ok := extras["server_name"].(string); ok && n != "" {
			name = n
		}
		obj = AS2Object{Type: "Service", Name: name}
		summary = fmt.Sprintf("Server name updated to '%s'", name)

	case "MESSAGE":
		// Direct messages are Create(Note) activities with specific addressing.
		obj = AS2Object{Type: "Note"}
		if c, ok := extras["content"].(string); ok {
			obj.Content = c
		}
		if to, ok := extras["to"].(string); ok {
			// In private messaging, the ID field is often used for the recipient URI.
			obj.ID = to
		}
		summary = fmt.Sprintf("%s sent you a message", actorID)

	default:
		// Generic fallback for extensibility.
		obj = AS2Object{Type: "Object", ID: entityID}
		summary = fmt.Sprintf("%s performed an action", actorID)
	}

	// ── Final Activity Assembly ──────────────────────────────────────────────
	activity := AS2Activity{
		Context:   AS2Context,
		ID:        activityID,
		Type:      as2Type,
		Actor:     actor,
		Object:    obj,
		Summary:   summary,
		Published: time.Now().UTC().Format(time.RFC3339),
	}

	// Convert the Go struct into a JSON byte slice for transmission or storage.
	return json.Marshal(activity)
}
