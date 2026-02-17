
package crypto

import (
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if pub == "" || priv == "" {
		t.Error("Generated keys should not be empty")
	}

	// Verify lengths (Ed25519 keys are 32 bytes private seed, 32 bytes public, but internal rep might differ.
	// Hex encoded should be 64 chars for 32 bytes)
	// Actually ed25519 private key is 64 bytes (seed + pub), so 128 hex chars
	// Public key is 32 bytes, so 64 hex chars

	if len(pub) != 64 {
		t.Errorf("Expected public key length 64, got %d", len(pub))
	}
	if len(priv) != 128 {
		t.Errorf("Expected private key length 128, got %d", len(priv))
	}
}

func TestSignAndVerify(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	data := []byte("hello world")

	// Sign
	sig, err := SignData(data, priv)
	if err != nil {
		t.Fatalf("SignData failed: %v", err)
	}
	if sig == "" {
		t.Fatal("Signature should not be empty")
	}

	// Verify
	valid, err := VerifySignature(data, sig, pub)
	if err != nil {
		t.Fatalf("VerifySignature failed: %v", err)
	}
	if !valid {
		t.Error("Signature verification failed")
	}

	// Verify failure with wrong data
	valid, _ = VerifySignature([]byte("wrong data"), sig, pub)
	if valid {
		t.Error("Verification should fail for wrong data")
	}

	// Verify failure with wrong key
	otherPub, _, _ := GenerateKeyPair()
	valid, _ = VerifySignature(data, sig, otherPub)
	if valid {
		t.Error("Verification should fail for wrong public key")
	}
}

func TestHashing(t *testing.T) {
	s := "test"
	hash := HashString(s)
	// specific known hash for "test"
	expected := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if hash != expected {
		t.Errorf("HashString(%s) = %s; want %s", s, hash, expected)
	}
}

func TestEncryption(t *testing.T) {
	// 32-byte key for AES-256
	key := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	plaintext := "secret message"

	// Encrypt
	cipherText, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if cipherText == "" {
		t.Fatal("Ciphertext should not be empty")
	}
	if cipherText == plaintext {
		t.Fatal("Ciphertext should not match plaintext")
	}

	// Decrypt
	decrypted, err := Decrypt(cipherText, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypted %s; want %s", decrypted, plaintext)
	}
}

func TestRecoveryKey(t *testing.T) {
	key, hash, err := GenerateRecoveryKey()
	if err != nil {
		t.Fatalf("GenerateRecoveryKey failed: %v", err)
	}

	// Key should be 32 bytes -> 64 hex chars
	if len(key) != 64 {
		t.Errorf("Expected key length 64, got %d", len(key))
	}

	// Hash should be valid SHA256 of key (as bytes? or string?)
	// Implementation says: sha256.Sum256([]byte(key)) where key is the hex string
	expectedHash := HashString(key)
	if hash != expectedHash {
		t.Errorf("Hash mismatch. Got %s, want %s", hash, expectedHash)
	}
}
