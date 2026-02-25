package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// AS2Context is the standard ActivityStreams 2.0 JSON-LD context.
const AS2Context = "https://www.w3.org/ns/activitystreams"

// AS2Object is a generic ActivityStreams object / actor.
type AS2Object struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	// inReplyTo is a plain string (post ID) for Note objects
	InReplyTo string `json:"inReplyTo,omitempty"`
}

// AS2Activity is the top-level ActivityStreams activity envelope.
type AS2Activity struct {
	Context   string    `json:"@context"`
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Actor     AS2Object `json:"actor"`
	Object    AS2Object `json:"object"`
	Summary   string    `json:"summary"`
	Published string    `json:"published"`
}

// internalTypeToAS2 maps internal notification types to ActivityStreams 2.0 activity types.
//
//	FOLLOW   → Follow    (actor follows object)
//	LIKE     → Like      (actor likes a Note)
//	REPLY    → Create    (actor creates a Note inReplyTo another Note)
//	REPOST   → Announce  (actor announces a Note)
//	UNFOLLOW → Undo      (actor undoes a Follow)
//	SERVER_UPDATE → Update (service updates a Service object)
//	MESSAGE  → Create    (actor creates a Note directed at recipient)
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
		return "Activity"
	}
}

// serverBaseURL returns the externally reachable URL of this server.
func serverBaseURL() string {
	u := os.Getenv("SERVER_URL")
	if u == "" {
		u = "http://localhost:8080"
	}
	return u
}

// BuildActivityStream constructs a fully-formed AS2 activity for a given
// internal notification type and returns it as a JSON byte slice.
//
// Parameters:
//   - actorID   the user performing the action  (e.g. "alice@server_a")
//   - typeStr   internal type                   (FOLLOW | LIKE | REPLY | REPOST | SERVER_UPDATE …)
//   - entityID  the post-id / user-id involved  (may be "" for SERVER_UPDATE)
//   - extras    optional extra fields (e.g. "content", "parent_id", "server_name")
func BuildActivityStream(actorID, typeStr, entityID string, extras map[string]interface{}) ([]byte, error) {
	as2Type := internalTypeToAS2(typeStr)
	activityID := fmt.Sprintf("%s/activities/%s", serverBaseURL(), uuid.New().String())

	// ── Actor ────────────────────────────────────────────────────────────────
	actorType := "Person"
	if actorID == "system" {
		actorType = "Service"
	}
	actor := AS2Object{Type: actorType, ID: actorID}

	// ── Object ───────────────────────────────────────────────────────────────
	var obj AS2Object
	var summary string

	switch typeStr {
	case "FOLLOW":
		// Object is the person being followed (the recipient)
		obj = AS2Object{Type: "Person", ID: entityID}
		summary = fmt.Sprintf("%s followed you", actorID)

	case "LIKE":
		obj = AS2Object{Type: "Note", ID: entityID}
		summary = fmt.Sprintf("%s liked your post", actorID)

	case "REPLY":
		obj = AS2Object{Type: "Note", ID: entityID}
		if c, ok := extras["content"].(string); ok {
			obj.Content = c
		}
		if p, ok := extras["parent_id"].(string); ok && p != "" {
			obj.InReplyTo = p
		} else {
			obj.InReplyTo = entityID // at minimum, reply is to the post
		}
		summary = fmt.Sprintf("%s replied to your post", actorID)

	case "REPOST":
		obj = AS2Object{Type: "Note", ID: entityID}
		summary = fmt.Sprintf("%s reposted your post", actorID)

	case "UNFOLLOW":
		// Undo wraps the original Follow activity — represent as a Follow object with same IDs
		// We encode the inner Follow as Content for simplicity
		innerFollow, _ := json.Marshal(map[string]interface{}{
			"type":   "Follow",
			"actor":  actor,
			"object": AS2Object{Type: "Person", ID: entityID},
		})
		obj = AS2Object{Type: "Follow", Content: string(innerFollow)}
		summary = fmt.Sprintf("%s unfollowed you", actorID)

	case "SERVER_UPDATE":
		name := entityID
		if n, ok := extras["server_name"].(string); ok && n != "" {
			name = n
		}
		obj = AS2Object{Type: "Service", Name: name}
		summary = fmt.Sprintf("Server name updated to '%s'", name)

	case "MESSAGE":
		obj = AS2Object{Type: "Note"}
		if c, ok := extras["content"].(string); ok {
			obj.Content = c
		}
		if to, ok := extras["to"].(string); ok {
			obj.ID = to
		}
		summary = fmt.Sprintf("%s sent you a message", actorID)

	default:
		obj = AS2Object{Type: "Object", ID: entityID}
		summary = fmt.Sprintf("%s performed an action", actorID)
	}

	activity := AS2Activity{
		Context:   AS2Context,
		ID:        activityID,
		Type:      as2Type,
		Actor:     actor,
		Object:    obj,
		Summary:   summary,
		Published: time.Now().UTC().Format(time.RFC3339),
	}

	return json.Marshal(activity)
}
