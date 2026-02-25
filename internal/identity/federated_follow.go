package identity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// FederatedFollowRequest is the payload posted to /federation/follow on the remote server.
type FederatedFollowRequest struct {
	Follower  string `json:"follower"`  // full user_id e.g. alice@server_a
	Followee  string `json:"followee"`  // full user_id e.g. bob@server_b
	Action    string `json:"action"`    // "follow" or "unfollow"
	ServerID  string `json:"server_id"` // sending server's server_id
	Timestamp string `json:"timestamp"`
}

// DeliverFederatedFollow posts a follow/unfollow event to the followee's home server
// so that server can record the relationship in its own follows table.
func DeliverFederatedFollow(followerID, followeeID, action string) {
	parts := strings.Split(followeeID, "@")
	if len(parts) != 2 {
		log.Printf("DeliverFederatedFollow: invalid followee ID %s", followeeID)
		return
	}
	targetServerID := parts[1]

	remoteServer, _, err := EnsureServerTrusted(targetServerID)
	if err != nil {
		log.Printf("DeliverFederatedFollow: cannot reach %s: %v", targetServerID, err)
		return
	}

	payload := FederatedFollowRequest{
		Follower:  followerID,
		Followee:  followeeID,
		Action:    action,
		ServerID:  getServerID(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("DeliverFederatedFollow: marshal error: %v", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/federation/follow", remoteServer.Endpoint),
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		log.Printf("DeliverFederatedFollow: delivery to %s failed: %v", targetServerID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("DeliverFederatedFollow: remote %s returned %d", targetServerID, resp.StatusCode)
		return
	}

	log.Printf("✅ Federated %s delivered: %s → %s", action, followerID, followeeID)
}

// HandleIncomingFederatedFollow receives a follow/unfollow event from a remote server
// and writes/deletes the follow row in the local follows table.
func HandleIncomingFederatedFollow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req FederatedFollowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Follower == "" || req.Followee == "" || req.Action == "" || req.ServerID == "" {
		RespondWithError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	// Validate that follower's server matches the sender's claimed server_id
	followerParts := strings.Split(req.Follower, "@")
	if len(followerParts) != 2 || followerParts[1] != req.ServerID {
		RespondWithError(w, http.StatusBadRequest, "follower server does not match server_id")
		return
	}

	// Ensure sender is trusted (auto-handshake if needed)
	_, _, err := EnsureServerTrusted(req.ServerID)
	if err != nil {
		log.Printf("HandleIncomingFederatedFollow: untrusted server %s: %v", req.ServerID, err)
		RespondWithError(w, http.StatusForbidden, "server not trusted")
		return
	}

	// Verify the followee actually lives on this server
	followeeParts := strings.Split(req.Followee, "@")
	if len(followeeParts) != 2 || followeeParts[1] != getServerID() {
		RespondWithError(w, http.StatusBadRequest, "followee is not on this server")
		return
	}

	switch req.Action {
	case "follow":
		// Record the remote follower in our local follows table
		_, err = db.Exec(`
			INSERT INTO follows (follower_user_id, follower_home_server, followee_user_id, followee_home_server)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`,
			req.Follower, remoteEndpointForServer(req.ServerID),
			req.Followee, getLocalEndpoint(),
		)
		if err != nil {
			log.Printf("HandleIncomingFederatedFollow: db insert error: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to record follow")
			return
		}
		// Invalidate the followee's followers cache
		invalidateFollowCaches(req.Follower, req.Followee)
		log.Printf("✅ Recorded federated follow: %s → %s", req.Follower, req.Followee)

	case "unfollow":
		_, err = db.Exec(`
			DELETE FROM follows WHERE follower_user_id = $1 AND followee_user_id = $2`,
			req.Follower, req.Followee,
		)
		if err != nil {
			log.Printf("HandleIncomingFederatedFollow: db delete error: %v", err)
			RespondWithError(w, http.StatusInternalServerError, "failed to remove follow")
			return
		}
		invalidateFollowCaches(req.Follower, req.Followee)
		log.Printf("✅ Removed federated follow: %s → %s", req.Follower, req.Followee)

	default:
		RespondWithError(w, http.StatusBadRequest, "action must be 'follow' or 'unfollow'")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// remoteEndpointForServer looks up the endpoint for a given server_id from
// trusted_servers. Falls back to an empty string if not found.
func remoteEndpointForServer(serverID string) string {
	ts, err := GetTrustedServer(serverID)
	if err != nil {
		return ""
	}
	return ts.Endpoint
}
