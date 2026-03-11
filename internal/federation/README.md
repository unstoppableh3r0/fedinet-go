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


🔐 1. Enhanced Security & Trust
In a real-world federation, you cannot trust the ActorID just because a server says so. You need cryptographic proof.

HTTP Signatures (Ed25519/RSA): Implement a signing mechanism where every outbound request includes a Signature header. The receiving server fetches the actor's public key to verify the message wasn't tampered with.

Actor Key Discovery: Add an endpoint GET /users/:username/key to serve public keys for signature verification.

Request Digesting: Use the Digest header (SHA-256) to ensure the JSON body hasn't been modified in transit.

Domain Verification (DNS): Support /.well-known/host-meta and WebFinger (acct:user@domain) to allow users on other instances to find your users via a standard handle.

Strict TLS Policy: Option to reject any federated server not using valid, non-expired HTTPS certificates.

🚀 2. Scalability & Performance
The current "Go-routine per send" model works for small loads, but will overwhelm the database and network under heavy federation (e.g., a post going viral).

Message Broker Integration: Replace internal Go channels/routines with Redis or RabbitMQ. This allows you to scale the "Federation Workers" independently of the web server.

Batch Delivery: If sending the same public post to 500 users on mastodon.social, send one HTTP request to their "Shared Inbox" rather than 500 individual requests.

Connection Pooling: Optimize the http.Client with a custom Transport to reuse TCP connections to frequently contacted instances.

Database Partitioning: As the inbox_activities table grows into the millions, partition it by created_at or actor_server to keep indexes performant.

🛠 3. Advanced Moderation (The "Defederation" Suite)
Federation is as much about blocking as it is about talking.

Media Sandboxing: A "Media Proxy" that caches remote images locally. This prevents remote servers from tracking your users via IP when their browser loads an external image.

Content Filtering (Regex): Global word filters that automatically mark inbound activities as "Spam" or "Sensitive" based on community-defined rules.

Community Blocklist Subscriptions: Allow admins to subscribe to a remote "Blocklist Provider" to automatically sync known malicious server lists.

Shadow-Banning: A state where a remote server thinks its messages are being delivered, but they are dropped silently by your inbox to prevent "troll-loops."

📊 4. Observability & Debugging
Federation is notoriously hard to debug because you only control one half of the conversation.

Federation Dashboard: A UI to visualize:

Success/Failure donuts per remote instance.

Latency Heatmap: Which servers are slowing down your workers?

Live Tail: A websocket-powered stream of inbound/outbound headers for real-time debugging.

Tracing (OpenTelemetry): Inject traceparent headers into federated requests to track a message's journey across multiple servers in the Fedinet.

Dead Letter Queue (DLQ) Explorer: A management interface to manually retry or "Ignore" messages that have hit the Max Retry limit.

🌐 5. Protocol Extensions
Move beyond simple text messages.

Object Capabilities (OCAPs): Instead of just "Update," support specialized activities like Announce (Boost/Reblog), Undo (Unfollow), and Delete (Tombstone management).

Collections API: Support GET requests for followers, following, and outbox collections so remote servers can sync state.

Sideband Data: Support for custom metadata like reactions (Emoji), polls, and expiration timers (Delete-At).

🏗 Updated Architecture Diagram
Plaintext
federation/
├── handlers/
│   ├── inbox.go        # Logic for /inbox
│   ├── outbox.go       # Logic for /outbox
│   └── discovery.go    # WebFinger & Host-Meta
├── crypto/
│   ├── signatures.go   # Ed25519 signing logic
│   └── keys.go         # Key generation and rotation
├── workers/
│   ├── deliverer.go    # Heavy-duty background sender
│   ├── janitor.go      # Cleanup and optimization
│   └── observer.go     # Health and metric aggregation
├── middleware/
│   ├── ratelimit.go    # Advanced IP/Server throttling
│   └── signature_v.go  # Signature verification middleware
└── ...
📅 Future Roadmap
Phase 1: Implement WebFinger (to be "findable").

Phase 2: Implement HTTP Signatures (to be "trusted").

Phase 3: Shared Inbox support (to be "efficient").

Phase 4: Media Proxying (to be "private").

Would you like me to generate the Go code for the WebFinger discovery handler or the HTTP Signature verification middleware?