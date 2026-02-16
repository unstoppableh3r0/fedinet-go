package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	verifyServer("Server A", "postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable")
	verifyServer("Server B", "postgres://postgres:postgres@localhost:5432/fedinet_server_b?sslmode=disable")
}

func verifyServer(name, dsn string) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("--- Verifying %s ---\n", name)

	// Check Identity
	var serverName, publicKey string
	err = db.QueryRow("SELECT server_name, public_key FROM server_identity WHERE id = 1").Scan(&serverName, &publicKey)
	if err != nil {
		fmt.Printf("❌ Failed to fetch identity: %v\n", err)
	} else {
		fmt.Printf("✅ Identity: Name='%s'\n   Public Key Prefix: %s...\n", serverName, publicKey[:20])
	}

	// Check Trusted Servers
	rows, err := db.Query("SELECT server_name, endpoint FROM trusted_servers")
	if err != nil {
		fmt.Printf("❌ Failed to fetch trusted servers: %v\n", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var tName, tEndpoint string
		if err := rows.Scan(&tName, &tEndpoint); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ℹ️  Trusts: %s (%s)\n", tName, tEndpoint)
		count++
	}
	if count == 0 {
		fmt.Println("🔒 No trusted servers (Isolated)")
	}
	fmt.Println()
}
