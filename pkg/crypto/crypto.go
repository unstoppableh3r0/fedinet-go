package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// GenerateKeyPair generates a new Ed25519 key pair.
// Returns hex-encoded public and private keys.
func GenerateKeyPair() (string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(pub), hex.EncodeToString(priv), nil
}

// SignData signs the data with the given private key.
// Expects hex-encoded private key. Returns hex-encoded signature.
func SignData(data []byte, privKeyHex string) (string, error) {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", err
	}
	if len(privKeyBytes) != ed25519.PrivateKeySize {
		return "", errors.New("invalid private key length")
	}

	signature := ed25519.Sign(privKeyBytes, data)
	return hex.EncodeToString(signature), nil
}

// VerifySignature verifies the signature against the data using the public key.
// Expects hex-encoded public key and signature.
func VerifySignature(data []byte, signatureHex, pubKeyHex string) (bool, error) {
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return false, err
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return false, errors.New("invalid public key length")
	}

	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false, err
	}

	return ed25519.Verify(pubKeyBytes, data, signatureBytes), nil
}

// GenerateRecoveryKey generates a random 32-byte hex-encoded key and its SHA-256 hash.
func GenerateRecoveryKey() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	key := hex.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(key))
	return key, hex.EncodeToString(hash[:]), nil
}

// HashString returns the SHA-256 hash of a string.
func HashString(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

// Encryption Helpers

// Encrypt encrypts data using AES-GCM and the provided master key (hex encoded).
// Returns hex-encoded nonce + ciphertext.
func Encrypt(plaintext, masterKeyHex string) (string, error) {
	if masterKeyHex == "" {
		return "", errors.New("master key is empty")
	}

	key, err := hex.DecodeString(masterKeyHex)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts data using AES-GCM and the provided master key (hex encoded).
func Decrypt(ciphertextHex, masterKeyHex string) (string, error) {
	if masterKeyHex == "" {
		return "", errors.New("master key is empty")
	}

	key, err := hex.DecodeString(masterKeyHex)
	if err != nil {
		return "", err
	}

	data, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
