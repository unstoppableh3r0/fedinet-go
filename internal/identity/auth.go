package main

import (
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/golang-jwt/jwt/v5"
)

// PASSWORD FUNCTIONS (EXISTING)

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPasswordHash compares a password with a hash
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// JWT FUNCTIONS (NEW)

var jwtSecret = []byte(os.Getenv("JWT_SECRET"))

// GenerateUserJWT creates a signed JWT token for authenticated users
func GenerateUserJWT(userID string, homeServer string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":     userID,
		"home_server": homeServer,
		"exp":         time.Now().Add(24 * time.Hour).Unix(), // expires in 24 hours
		"iat":         time.Now().Unix(),                     // issued at
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(jwtSecret)
}
