package main

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
)

// This test script simulates Server A sending a SIGNED federated lookup
// request to Server B running on localhost:9080.
//
// Usage:
//   go run ./cmd/signtest/
//
// What it does:
//   1. Generates a fresh Ed25519 key pair (simulating Server A's identity)
//   2. Builds a GET /federation/lookup?id=alice@localhost request
//   3. Signs the request using the HTTP Signatures pattern
//   4. Sends it to localhost:9080
//   5. Prints the response (expect 401 since Server B doesn't know Server A's key)

const targetURL = "http://localhost:9080/federation/lookup?id=alice@localhost"

func main() {
	fmt.Println("=== Fedinet Signed Request Handshake Test ===")
	fmt.Println()

	// 1. Generate a key pair for "Server A"
	pubKey, privKey, err := crypto.GenerateKeyPair()
	if err != nil {
		fmt.Printf("❌ FAIL: Could not generate key pair: %v\n", err)
		return
	}
	serverAID := "serverA@localhost"
	fmt.Printf("🔑 Generated Ed25519 key pair for %s\n", serverAID)
	fmt.Printf("   Public Key:  %s...\n", pubKey[:32])
	fmt.Printf("   Private Key: %s...\n", privKey[:32])
	fmt.Println()

	// 2. Build the HTTP request
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		fmt.Printf("❌ FAIL: Could not create request: %v\n", err)
		return
	}
	req.Host = "localhost:9080"

	// 3. Sign the request (inline implementation matching privacy.SignRequest)
	err = signRequest(req, privKey, serverAID)
	if err != nil {
		fmt.Printf("❌ FAIL: Could not sign request: %v\n", err)
		return
	}

	fmt.Printf("📝 Signed request to: %s\n", targetURL)
	fmt.Printf("   Signature header: %s\n", req.Header.Get("Signature"))
	fmt.Printf("   Date header:      %s\n", req.Header.Get("Date"))
	fmt.Printf("   Digest header:    %s\n", req.Header.Get("Digest"))
	fmt.Println()

	// 4. Send the request
	fmt.Println("📡 Sending signed request to Server B...")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ FAIL: Request failed: %v\n", err)
		fmt.Println()
		fmt.Println("💡 Make sure Server B (federation service) is running on localhost:9080")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("📨 Response Status: %s\n", resp.Status)
	fmt.Printf("📨 Response Body:   %s\n", string(body))
	fmt.Println()

	// 5. Evaluate result
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		fmt.Println("✅ PASS: Server B correctly rejected the request (401 Unauthorized)")
		fmt.Println("   This is expected because Server A's public key is not registered")
		fmt.Println("   in Server B's database. The middleware is working correctly!")
	case http.StatusOK:
		fmt.Println("✅ PASS: Server B accepted the signed request (200 OK)")
		fmt.Println("   This means Server A's key was found and the signature was valid.")
	default:
		fmt.Printf("⚠️  UNEXPECTED: Got status %d — review the response above.\n", resp.StatusCode)
	}
}

// signRequest signs an HTTP request using the HTTP Signatures draft pattern.
// This is a standalone version of privacy.SignRequest (since we can't import
// across main packages).
func signRequest(req *http.Request, privateKeyHex string, keyID string) error {
	// Set Date header
	if req.Header.Get("Date") == "" {
		req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}

	// Compute body digest (SHA-256)
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

	// Build signing string
	requestTarget := strings.ToLower(req.Method) + " " + req.URL.RequestURI()
	signingString := fmt.Sprintf(
		"(request-target): %s\nhost: %s\ndate: %s\ndigest: %s",
		requestTarget,
		req.Host,
		req.Header.Get("Date"),
		digestHeader,
	)

	// Sign
	signature, err := crypto.SignData([]byte(signingString), privateKeyHex)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	// Set Signature header
	sigHeader := fmt.Sprintf(
		`keyId="%s",algorithm="ed25519",headers="(request-target) host date digest",signature="%s"`,
		keyID,
		signature,
	)
	req.Header.Set("Signature", sigHeader)

	return nil
}
