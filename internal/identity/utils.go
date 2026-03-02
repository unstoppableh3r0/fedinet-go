package identity

import (
	"strings"
)

// ToExternalID returns the canonical external user ID.
// User IDs are stored internally as "username@<SERVER_ID>" (e.g. "alice@server_a").
// The external/public form is identical — we never substitute the human display name
// because the domain suffix must match the server_id key in trusted_servers and
// FEDERATION_PEERS so that cross-server routing works correctly.
func ToExternalID(internalID string) string {
	return internalID
}

// ToInternalID normalises an incoming user ID to the internal storage format.
// Lowercases the whole string; appends "@<SERVER_ID>" if there is no "@" at all.
func ToInternalID(externalID string) string {
	externalID = strings.ToLower(externalID)
	if !strings.Contains(externalID, "@") {
		return externalID + "@" + InternalServerName
	}
	return externalID
}
