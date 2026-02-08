package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PortableProfile mirrors the struct in models.go
type PortableProfile struct {
	User        interface{}   `json:"user_document"`
	Posts       []interface{} `json:"posts"`
	IdentitySig string        `json:"identity_signature"`
	PrivateKey  string        `json:"private_key"`
}

func main() {
	baseURL := "http://localhost:8080"
	username := fmt.Sprintf("PortableUser%d", time.Now().Unix())

	// 1. Register User
	fmt.Println("1. Registering user:", username)
	resp, err := http.Post(baseURL+"/register", "application/json", bytes.NewBuffer([]byte(fmt.Sprintf(`{"username": "%s"}`, username))))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("Registration failed: %s", body))
	}

	// 2. Export Profile
	fmt.Println("2. Exporting profile...")
	time.Sleep(100 * time.Millisecond) // wait for DB?
	resp, err = http.Get(fmt.Sprintf("%s/identity/export?user_id=%s", baseURL, username))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("Export failed: %s", body))
	}

	exportBody, _ := io.ReadAll(resp.Body)
	// fmt.Println("Exported JSON:", string(exportBody))

	var profile PortableProfile
	if err := json.Unmarshal(exportBody, &profile); err != nil {
		panic(err)
	}

	if profile.PrivateKey == "" {
		panic("Private Key missing in export!")
	}
	if profile.IdentitySig == "" {
		panic("Identity Signature missing in export!")
	}
	fmt.Println("   Export looks valid (has keys and sig).")

	// 3. Import Profile (Simulate Restore)
	// We'll change the user ID in the export to simulate importing as a 'new' user/restore or just overwrite?
	// The import logic verifies the signature. UserID is part of signature.
	// So we can't change UserID without invalidating signature.
	// We will try to import it back. Ideally, we should delete the user first to test 'restore',
	// or modify the Import handler to handle 'overwrite' (it has ON CONFLICT DO UPDATE).

	fmt.Println("3. Importing profile...")
	resp, err = http.Post(baseURL+"/identity/import", "application/json", bytes.NewBuffer(exportBody))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("Import failed: %s", body))
	}

	fmt.Println("Success! Portability flow verified.")
}
