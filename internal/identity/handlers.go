package identity

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// ============================================================================
// SOCIAL GRAPH HANDLERS
// ============================================================================

// FollowHandler processes POST /follow requests.
// It establishes a directional relationship between two users.
func FollowHandler(w http.ResponseWriter, r *http.Request) {
    log.Println("---- /follow HIT ----")

    // Strict Method Enforcement: Follows modify state and must be POST.
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    // Anonymous struct for ad-hoc JSON decoding of the follow intent.
    var req struct {
        Follower string `json:"follower"` // The initiator of the follow
        Followee string `json:"followee"` // The target of the follow
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid JSON")
        return
    }

    // Validation: Prevent empty social graph edges.
    if req.Follower == "" || req.Followee == "" {
        RespondWithError(w, http.StatusBadRequest, "missing fields")
        return
    }

    // ID NORMALIZATION:
    // Convert public handles (alice@domain) to internal database keys (alice@local_id).
    internalFollower := ToInternalID(req.Follower)
    internalFollowee := ToInternalID(req.Followee)

    // Persistence Layer: Commit the follow relationship to the 'follows' table.
    if err := FollowUser(internalFollower, internalFollowee); err != nil {
        RespondWithError(w, http.StatusInternalServerError, err.Error())
        return
    }

    RespondWithJSON(w, http.StatusOK, map[string]string{"message": "followed"})
}

// MessageHandler handles the complex routing of direct messages.
// It distinguishes between local delivery and cross-server federation.
func MessageHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    var req struct {
        From     string  `json:"from"`
        To       string  `json:"to"`
        Content  string  `json:"content"`
        ImageURL *string `json:"image_url"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid JSON")
        return
    }

    // Constraint: A message must contain either text OR an image.
    if req.From == "" || req.To == "" || (req.Content == "" && req.ImageURL == nil) {
        RespondWithError(w, http.StatusBadRequest, "missing fields")
        return
    }

    // ------------------------------------------------------------------------
    // ROUTING LOGIC: LOCAL VS FEDERATED
    // ------------------------------------------------------------------------

    // IsFederatedUser checks if the 'To' handle contains a remote domain suffix.
    isFederated, serverName := IsFederatedUser(req.To)

    if isFederated {
        // FEDERATED PATH:
        // The recipient is on a different server. We must sign the payload
        // and relay it to the remote server's inbound API.
        log.Printf("Routing federated message from %s to %s (server: %s)", req.From, req.To, serverName)

        err := DeliverFederatedMessage(ToInternalID(req.From), req.To, req.Content)
        if err != nil {
            log.Printf("Failed to deliver federated message: %v", err)
            RespondWithError(w, http.StatusBadGateway, fmt.Sprintf("failed to deliver message: %v", err))
            return
        }

        // DOUBLE-WRITE (Local Copy):
        // Store a copy in the local DB so the sender can see this message in their history.
        if err := StoreSentFederatedMessage(ToInternalID(req.From), req.To, req.Content); err != nil {
            log.Printf("Warning: failed to store sent message copy: %v", err)
            // Resilience: We don't fail the request if local storage fails but delivery succeeded.
        }

        RespondWithJSON(w, http.StatusOK, map[string]string{
            "message": "federated message sent",
            "server":  serverName,
        })
        return
    }

    // LOCAL PATH:
    // Standard server-local message delivery logic.
    internalFrom := ToInternalID(req.From)
    internalTo := ToInternalID(req.To)

    // ANTI-SPAM GUARDRAIL:
    // Local users cannot message strangers. A follow relationship must exist in either direction.
    var canMessage bool
    _ = db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM follows
            WHERE (follower_user_id = $1 AND followee_user_id = $2)
               OR (follower_user_id = $2 AND followee_user_id = $1)
        )
    `, internalFrom, internalTo).Scan(&canMessage)

    if !canMessage {
        RespondWithError(w, http.StatusForbidden, "you must follow this user to send them a message")
        return
    }

    if err := SendMessage(internalFrom, internalTo, req.Content, req.ImageURL); err != nil {
        RespondWithError(w, http.StatusInternalServerError, err.Error())
        return
    }

    RespondWithJSON(w, http.StatusOK, map[string]string{"message": "message sent"})
}

// ============================================================================
// IDENTITY & DISCOVERY
// ============================================================================

// UserSearchHandler retrieves a user's full public profile.
// It includes privacy checks (AllowDiscovery) and calculated social metrics.
func UserSearchHandler(w http.ResponseWriter, r *http.Request) {
    // Panic Recovery: Protect the server process from unexpected DB or nil-pointer issues during search.
    defer func() {
        if rec := recover(); rec != nil {
            log.Println("PANIC recovered:", rec)
            RespondWithError(w, http.StatusInternalServerError, "internal server error")
        }
    }()

    log.Println("---- /user/search HIT ----")

    if r.Method != http.MethodGet {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    userID := r.URL.Query().Get("user_id")
    if userID == "" {
        RespondWithError(w, http.StatusBadRequest, "missing user_id")
        return
    }

    log.Printf("Search request: original userID = %s", userID)

    // Normalization to internal storage format (stripping public domain if it matches local).
    internalUserID := ToInternalID(userID)

    log.Printf("Search request: converted to internalUserID = %s", internalUserID)

    // 1. Identity Fetch: Get core account data (UserID, home server, discovery settings).
    identity, err := GetIdentityByUserID(internalUserID)
    if err != nil {
        log.Printf("GetIdentityByUserID error for user %s: %v", internalUserID, err)
        RespondWithError(w, http.StatusInternalServerError, "internal error")
        return
    }
    if identity == nil {
        log.Printf("User not found in database: %s", internalUserID)
        RespondWithError(w, http.StatusNotFound, "user not found")
        return
    }

    // PRIVACY CHECK:
    // If the user has disabled discovery, the profile is treated as non-existent for search.
    if !identity.AllowDiscovery {
        RespondWithError(w, http.StatusForbidden, "profile unavailable")
        return
    }

    // 2. Profile Fetch: Get UI-facing data (Bio, Avatar, Display Name).
    profile, err := GetProfileByUserID(internalUserID)
    if err != nil {
        log.Printf("GetProfileByUserID error for user %s: %v", internalUserID, err)
        RespondWithError(w, http.StatusInternalServerError, "internal error")
        return
    }
    if profile == nil {
        RespondWithError(w, http.StatusNotFound, "profile not found")
        return
    }

    // REVERSE-NORMALIZATION:
    // Convert internal IDs back to external handles for UI display.
    identity.UserID = ToExternalID(identity.UserID)
    profile.UserID = ToExternalID(profile.UserID)

    // 3. Social Metrics: Aggregating counts directly from the follows table.
    var followersCount, followingCount int
    db.QueryRow("SELECT COUNT(*) FROM follows WHERE followee_user_id = $1", internalUserID).Scan(&followersCount)
    db.QueryRow("SELECT COUNT(*) FROM follows WHERE follower_user_id = $1", internalUserID).Scan(&followingCount)
    profile.FollowersCount = &followersCount
    profile.FollowingCount = &followingCount

    // 4. Contextual State: Check if the 'viewer' is currently following the 'target'.
    viewerID := r.URL.Query().Get("viewer_id")
    isFollowing := false
    if viewerID != "" && viewerID != userID {
        internalViewerID := ToInternalID(viewerID)
        err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM follows WHERE follower_user_id=$1 AND followee_user_id=$2)", internalViewerID, internalUserID).Scan(&isFollowing)
        if err != nil {
            log.Println("Error checking follow status:", err)
        }
    }

    response := map[string]interface{}{
        "identity":     *identity,
        "profile":      *profile,
        "is_following": isFollowing,
    }

    RespondWithJSON(w, http.StatusOK, response)
}

// ============================================================================
// ACCOUNT MANAGEMENT
// ============================================================================

func strPtr(s string) *string {
    return &s
}

// RegisterHandler handles new user sign-ups.
// It manages invite code validation, password hashing, and token issuance.
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    var req struct {
        Username        string `json:"username"`
        Password        string `json:"password"`
        InviteCode      string `json:"invite_code"`
        ClientPublicKey string `json:"client_public_key"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid JSON")
        return
    }

    // ------------------------------------------------------------------------
    // INVITE SYSTEM VALIDATION
    // ------------------------------------------------------------------------

    // Bootstrap Logic: If the invite table is empty, allow the first registration
    // without a code (Admin creation). Otherwise, a code is mandatory.
    var inviteCount int
    db.QueryRow("SELECT COUNT(*) FROM invites").Scan(&inviteCount)

    if req.InviteCode != "" {
        if _, err := ValidateInvite(req.InviteCode); err != nil {
            RespondWithError(w, http.StatusForbidden, "invalid or expired invite: "+err.Error())
            return
        }
    } else if inviteCount > 0 {
        RespondWithError(w, http.StatusForbidden, "invite code required")
        return
    }

    if req.Username == "" || req.Password == "" {
        RespondWithError(w, http.StatusBadRequest, "username and password required")
        return
    }

    // Identity Uniqueness Check: Ensure handles are lowercase to prevent duplicates.
    federatedUserID := ToInternalID(strings.ToLower(req.Username))
    if identity, err := GetIdentityByUserID(federatedUserID); err == nil && identity != nil {
        RespondWithError(w, http.StatusConflict, "username taken")
        return
    }

    // SECURITY: Argon2/BCrypt hashing should be used within HashPassword.
    hashedPassword, err := HashPassword(req.Password)
    if err != nil {
        RespondWithError(w, http.StatusInternalServerError, "failed to hash password")
        return
    }

    // Determine environmental context for the new identity.
    homeServer := os.Getenv("SERVER_URL")
    if homeServer == "" {
        homeServer = "http://localhost:8080"
    }

    // CORE ACCOUNT CREATION:
    // This atomic operation creates the Identity, Profile, and Key entries.
    recoveryKey, err := CreateAccountWithClientKey(
        federatedUserID,
        homeServer,
        hashedPassword,
        req.ClientPublicKey,
    )
    if err != nil {
        log.Println("CreateAccount error:", err)
        RespondWithError(w, http.StatusInternalServerError, "failed to create account")
        return
    }

    // AUDIT TRAIL: Link the invite to the new user and log IP/UserAgent.
    if req.InviteCode != "" {
        if err := UseInvite(req.InviteCode, federatedUserID, r.RemoteAddr, r.UserAgent()); err != nil {
            log.Printf("Failed to mark invite %s as used: %v", req.InviteCode, err)
        }
    }

    // Optional: Legacy session support.
    sessionKey, _ := GenerateSessionKey(federatedUserID)

    // OAUTH2-STYLE TOKENS: Generate JWT or opaque token pair for API access.
    accessToken, refreshToken, err := GenerateTokenPair(federatedUserID, homeServer)
    if err != nil {
        log.Println("Token generation failed:", err)
        RespondWithError(w, http.StatusInternalServerError, "failed to generate tokens")
        return
    }

    RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
        "user_id":       ToExternalID(federatedUserID),
        "home_server":   homeServer,
        "recovery_key":  recoveryKey,
        "session_key":   sessionKey,
        "access_token":  accessToken,
        "refresh_token": refreshToken,
    })
}

// UpdateProfileHandler handles profile metadata changes.
func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    var req models.UpdateProfileRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    if req.UserID == "" {
        RespondWithError(w, http.StatusBadRequest, "user_id is required")
        return
    }

    // Re-bind to internal format before DB execution.
    req.UserID = ToInternalID(req.UserID)

    if err := UpdateProfile(req); err != nil {
        log.Printf("UpdateProfileHandler error for user %s: %v", req.UserID, err)
        RespondWithError(w, http.StatusInternalServerError, "failed to update profile")
        return
    }

    RespondWithJSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}