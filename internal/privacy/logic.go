package privacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// SignRequest signs an outgoing HTTP request using the HTTP Signatures draft pattern.
// It sets the Signature, Date, and Digest headers on the request.
// Parameters:
//   - req: the *http.Request to sign
//   - privateKeyHex: the server's Ed25519 private key (hex-encoded)
//   - keyID: identifier for the signing key (e.g., server ID or server URL)
func SignRequest(req *http.Request, privateKeyHex string, keyID string) error {
	// 1. Set Date header if not already set
	if req.Header.Get("Date") == "" {
		req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}

	// 2. Compute body digest (SHA-256)
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore the body so it can be read again by the HTTP client
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	digest := sha256.Sum256(bodyBytes)
	digestHeader := "SHA-256=" + hex.EncodeToString(digest[:])
	req.Header.Set("Digest", digestHeader)

	// 3. Build the signing string per HTTP Signatures draft
	// Format: (request-target): method path\nhost: host\ndate: date\ndigest: digest
	requestTarget := strings.ToLower(req.Method) + " " + req.URL.RequestURI()
	signingString := fmt.Sprintf(
		"(request-target): %s\nhost: %s\ndate: %s\ndigest: %s",
		requestTarget,
		req.Host,
		req.Header.Get("Date"),
		digestHeader,
	)

	// 4. Sign the string using Ed25519
	signature, err := crypto.SignData([]byte(signingString), privateKeyHex)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	// 5. Set the Signature header
	// Format: keyId="...",algorithm="ed25519",headers="(request-target) host date digest",signature="..."
	sigHeader := fmt.Sprintf(
		`keyId="%s",algorithm="ed25519",headers="(request-target) host date digest",signature="%s"`,
		keyID,
		signature,
	)
	req.Header.Set("Signature", sigHeader)

	return nil
}

// Visibility type is now imported from pkg/models

// EvaluateAccess evaluates if a viewer has permission to see content
// Covers Story 3.3 and 3.7
func EvaluateAccess(author, viewer string, visibility models.Visibility, isFollower bool) bool {
	if author == viewer {
		return true // Author always has access
	}

	switch visibility {
	case models.VisibilityPublic:
		return true
	case models.VisibilityFollowers:
		return isFollower
	case models.VisibilityPrivate:
		return false
	case models.VisibilityServer:
		// Logic to compare domains would be added here
		return false
	default:
		return false
	}
}
