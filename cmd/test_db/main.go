package main

import (
    "database/sql"
    "fmt"
    "log"
    "os"

    _ "github.com/lib/pq"
)

func main() {
    connStr := os.Getenv("DATABASE_URL")
    if connStr == "" {
        log.Fatal("DATABASE_URL not set")
    }

    db, err := sql.Open("postgres", connStr)
    if err != nil {
        log.Fatal(err)
    }

    rows, err := db.Query("SELECT activity_id, toxicity_score, status FROM moderation_logs ORDER BY created_at DESC LIMIT 1")
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    for rows.Next() {
        var id string
        var score float64
        var status string
        if err := rows.Scan(&id, &score, &status); err != nil {
            log.Fatal(err)
        }
        fmt.Printf("Found record: id=%s, score=%f, status=%s\n", id, score, status)
    }
}
