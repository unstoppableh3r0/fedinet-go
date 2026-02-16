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







func SignRequest(req *http.Request, privateKeyHex string, keyID string) error {
	
	if req.Header.Get("Date") == "" {
		req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}

	
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	digest := sha256.Sum256(bodyBytes)
	digestHeader := "SHA-256=" + hex.EncodeToString(digest[:])
	req.Header.Set("Digest", digestHeader)

	
	
	requestTarget := strings.ToLower(req.Method) + " " + req.URL.RequestURI()
	signingString := fmt.Sprintf(
		"(request-target): %s\nhost: %s\ndate: %s\ndigest: %s",
		requestTarget,
		req.Host,
		req.Header.Get("Date"),
		digestHeader,
	)

	
	signature, err := crypto.SignData([]byte(signingString), privateKeyHex)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	
	
	sigHeader := fmt.Sprintf(
		`keyId="%s",algorithm="ed25519",headers="(request-target) host date digest",signature="%s"`,
		keyID,
		signature,
	)
	req.Header.Set("Signature", sigHeader)

	return nil
}





func EvaluateAccess(author, viewer string, visibility models.Visibility, isFollower bool) bool {
	if author == viewer {
		return true 
	}

	switch visibility {
	case models.VisibilityPublic:
		return true
	case models.VisibilityFollowers:
		return isFollower
	case models.VisibilityPrivate:
		return false
	case models.VisibilityServer:
		
		return false
	default:
		return false
	}
}
