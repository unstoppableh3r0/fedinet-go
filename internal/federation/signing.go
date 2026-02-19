package federation

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
)

// SigningHeaders builds HTTP Signature headers for S2S delivery.
// Uses server_identity from DB (same DB as federation).
func SigningHeaders(method, path, host string, body []byte) (map[string]string, error) {
	masterKey := os.Getenv("SERVER_MASTER_KEY")
	if masterKey == "" {
		masterKey = "0000000000000000000000000000000000000000000000000000000000000000"
		log.Println("WARNING: Using insecure default SERVER_MASTER_KEY for federation signing")
	}

	var serverID, publicKeyHex, privKeyEncHex string
	err := db.QueryRow(`
		SELECT server_id::text, public_key, private_key_encrypted
		FROM server_identity WHERE id = 1
	`).Scan(&serverID, &publicKeyHex, &privKeyEncHex)
	if err != nil {
		return nil, fmt.Errorf("server identity not initialized: %w", err)
	}

	var privKeyHex string
	if dec, err := crypto.Decrypt(privKeyEncHex, masterKey); err == nil {
		privKeyHex = dec
	} else {
		// Init may store base64 plaintext; decode to hex for SignData
		raw, err := base64.StdEncoding.DecodeString(privKeyEncHex)
		if err != nil {
			return nil, fmt.Errorf("server private key invalid (not encrypted hex nor base64): %w", err)
		}
		privKeyHex = hex.EncodeToString(raw)
	}

	now := time.Now().UTC()
	dateStr := now.Format(time.RFC1123)

	digest := sha256.Sum256(body)
	digestStr := "SHA-256=" + hex.EncodeToString(digest[:])

	requestTarget := strings.ToLower(method) + " " + path
	signingParts := []string{
		"(request-target): " + requestTarget,
		"host: " + host,
		"date: " + dateStr,
		"digest: " + digestStr,
	}
	signingString := strings.Join(signingParts, "\n")

	sig, err := crypto.SignData([]byte(signingString), privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	return map[string]string{
		"Date":   dateStr,
		"Digest": digestStr,
		"Signature": fmt.Sprintf(
			`keyId="%s",algorithm="ed25519",headers="(request-target) host date digest",signature="%s"`,
			serverID, sig,
		),
	}, nil
}

// GetFederationPublicURL returns this server's federation base URL for ActorServer field.
func GetFederationPublicURL() string {
	if u := os.Getenv("FEDERATION_PUBLIC_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := os.Getenv("SERVER_URL"); u != "" {
		// If SERVER_URL is identity (8082), derive federation (8081)
		if strings.Contains(u, ":8082") {
			return strings.Replace(u, ":8082", ":8081", 1)
		}
		return u
	}
	return "http://localhost:8081"
}

// ExtractHost extracts host:port from a URL for the Host header.
func ExtractHost(targetURL string) string {
	u := strings.TrimPrefix(targetURL, "http://")
	u = strings.TrimPrefix(u, "https://")
	if idx := strings.Index(u, "/"); idx >= 0 {
		u = u[:idx]
	}
	if u == "" {
		return "localhost:8081"
	}
	if !strings.Contains(u, ":") {
		// Assume 8081 for federation
		return u + ":8081"
	}
	return u
}

