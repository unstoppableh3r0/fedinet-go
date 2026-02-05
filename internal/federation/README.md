# Federation Module

Complete implementation of federated server-to-server communication for fedinet-go.

## Features

- ✅ **Protocol Design** - Versioned message format with error handling
- ✅ **Secure Delivery** - Automatic retries with exponential backoff
- ✅ **Inbox/Outbox** - Structured activity endpoints with persistence
- ✅ **Acknowledgments** - Delivery confirmation tracking
- ✅ **Rate Limiting** - Per-server and per-endpoint limits
- ✅ **Serialization** - Canonical JSON format with validation
- ✅ **Capabilities** - Feature negotiation and discovery
- ✅ **Server Blocking** - Admin-controlled blocklist
- ✅ **Federation Modes** - Soft/hard mode with runtime switching
- ✅ **Health API** - Real-time metrics and status

## Quick Start

### 1. Apply Database Schema

```bash
psql $DATABASE_URL < migrations.sql
```

### 2. Start Server

```bash
go run .
```

Server runs on **http://localhost:8081**

### 3. Test Health Endpoint

```bash
curl http://localhost:8081/federation/health
```

## API Endpoints

### Core Federation
- `POST /federation/inbox` - Receive activities
- `GET /federation/outbox?actor_id=...` - Get outgoing activities
- `POST /federation/send` - Send activity to remote server
- `POST /federation/ack` - Receive acknowledgments

### Capabilities & Health
- `GET /federation/capabilities` - Server capabilities
- `POST /federation/discover` - Discover remote capabilities
- `GET /federation/health` - Instance health

### Admin
- `GET/POST/DELETE /federation/admin/blocks` - Manage blocked servers
- `GET/PUT /federation/admin/mode` - Federation mode config
- `POST /federation/admin/limits` - Configure rate limits

## Architecture

```
federation/
├── models.go       - Data structures
├── db.go          - Database connection
├── actions.go     - Business logic
├── handlers.go    - HTTP handlers
├── main.go        - Server & workers
├── migrations.sql - Database schema
└── README.md      - This file
```

## Background Workers

- **Retry Worker** (30s) - Processes failed deliveries
- **Expiration Worker** (5min) - Cleans up old messages
- **Health Worker** (1min) - Updates metrics

## Configuration

Set environment variables:
```bash
export DATABASE_URL="postgres://user:pass@localhost/fedinet"
```

## Testing

See [walkthrough.md](file:///home/optimus/.gemini/antigravity/brain/f775bee9-d895-49c2-b275-862dc399a7d9/walkthrough.md) for comprehensive testing examples.

## Default Settings

- **Rate Limit**: 100 requests/min (global)
- **Retry Strategy**: 6 attempts over 24 hours
- **Federation Mode**: Soft (allows unknown servers)
- **Message Expiration**: 24 hours
- **Capability Cache**: 1 hour
