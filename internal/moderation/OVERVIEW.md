# 🛡️ Epic 5 — Governance & Moderation
## Backend Overview

---

## 📌 Purpose

Epic 5 implements the **governance and moderation backend** for the FediNet platform.
Its goal is to provide core safety and control mechanisms required for operating a
federated social network where no single authority controls all data or policy.

This module enables:
- Reporting abusive or policy-violating content
- Moderation workflows for resolving reports
- Blocking malicious or untrusted servers
- Preparing governance actions for federation

Epic 5 forms the **safety net** of the system and supports digital sovereignty,
user protection, and resilient federation.

---

## 🎯 Scope (Sprint 1)

Sprint 1 focuses on **foundational moderation primitives** with federation-ready hooks.

### Included
- Abuse report submission and resolution
- Server-level blacklisting
- Federation event queuing (stub)
- Shared moderation data models
- Unit tests for moderation service logic

### Explicitly Excluded (Deferred)
- Federation transport and delivery workers
- Role-based access control enforcement
- Automated / AI-based moderation
- Appeals, voting, or transparency systems
- Admin UI implementation

These items are planned for future sprints.

---

## 🧩 Architecture

internal/moderation/
            ├── handlers.go // HTTP request handlers
            ├── service.go // Business logic
            ├── repository.go // Persistence abstraction
            ├── routes.go // Route registration
            ├── test/
            │ └── service_test.go
            └── OVERVIEW.md



The module follows a **clean layered architecture**:

- **Handlers** manage HTTP input/output
- **Service** contains moderation and governance logic
- **Repository** abstracts storage and queuing
- **Models** are shared via `pkg/models`

This separation allows easy testing, future refactoring, and federation integration.

---

## 🔐 Core Features

### 1️⃣ Abuse Reporting

Users can submit abuse reports against:
- Local content
- Remote (federated) content

Each report records:
- Reporter identity
- Target reference
- Target server (optional)
- Reason
- Status lifecycle (`pending` → `resolved`)

Reports are always stored locally and may be forwarded to remote instances
if federation is allowed.

---

### 2️⃣ Report Resolution

Moderators can:
- Retrieve all pending reports
- Resolve reports with moderator identity
- Track resolution metadata (timestamp and resolver)

Authorization is intentionally **not enforced at this layer** and is expected
to be handled via middleware in later phases.

---

### 3️⃣ Malicious Server Blocking

Administrators can block entire servers to prevent:
- Federation interactions
- Abuse report forwarding

Each block includes:
- Server domain
- Reason for blocking
- Administrator identity
- Timestamp

Blocked servers are excluded from federation communication.

---

### 4️⃣ Federation Event Queue (Stub)

Moderation actions generate **governance-related federation events**, including:
- Abuse report forwarding
- Server block notifications

In Sprint 1:
- Events are queued locally
- No delivery worker is implemented

This design ensures forward compatibility with the federation protocol
without premature coupling.

---

## 🗄️ Data Models

Epic 5 introduces the following shared models in `pkg/models`:

- `Report`
- `ReportStatus`
- `BlockedServer`
- `FederationEvent`
- `FederationEventType`
- `BackupMetadata`

These models are designed to be reusable across:
- Federation services
- Admin dashboards
- Audit and observability systems

---

## 🌐 API Endpoints

| Method | Endpoint | Description |
|------|---------|------------|
| POST | `/reports` | Submit an abuse report |
| GET  | `/moderation/reports` | List pending reports |
| POST | `/moderation/resolve` | Resolve a report |
| POST | `/servers/block` | Block a server |

All endpoints return standard HTTP status codes and JSON responses.

---

## 🧪 Testing

- Unit tests cover core service behavior
- Repository layer is mocked
- All tests pass via:




Integration testing with databases and federation transport
is intentionally deferred.

---

## 🔄 Integration Points

Epic 5 integrates with:

- **Epic 2 — Federation**
  - Consumes queued governance events
  - Handles cross-instance delivery

- **Admin / Moderator UI (Future)**
  - Displays reports and block lists

- **Shared Models (`pkg/models`)**
  - Ensures consistent data contracts across services

---

## 🚧 Known Limitations & Future Work

Planned enhancements include:
- Federation delivery workers and retry strategies
- Role-based access control
- Moderation audit logs and metrics
- Appeals and dispute resolution workflows
- Community governance and voting
- Transparency reporting

---

## ✅ Status

**Sprint 1 Complete**

- Moderation logic implemented
- Shared models merged
- Tests passing
- Ready for federation and UI integration

---

## 📍 Summary

Epic 5 delivers a **clean, extensible, and federation-ready moderation backend**.
It establishes essential governance primitives while deliberately deferring
complex policy enforcement and automation to future sprints.
