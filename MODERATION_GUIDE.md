# Moderation Guide

This guide explains how to appoint moderators, what they can do, and how to manage them in Fedinet.

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Step 1 – Admin Login](#step-1--admin-login)
4. [Step 2 – Assign a Moderator](#step-2--assign-a-moderator)
5. [Step 3 – Moderator Login](#step-3--moderator-login)
6. [Step 4 – Using the Moderator Frontend](#step-4--using-the-moderator-frontend)
7. [Managing Moderators](#managing-moderators)
8. [What Moderators Can Do](#what-moderators-can-do)
9. [Database Schema](#database-schema)
10. [Running with Docker](#running-with-docker)

---

## Overview

Fedinet uses a two-tier privilege system on top of normal user accounts:

| Role | JWT claim | How to assign |
|---|---|---|
| **Admin** | `IsAdmin: true` | Set during server initialisation (`/initialize`) |
| **Moderator** | `IsModerator: true` | Admin calls `POST /admin/moderators/assign` |

Regular users become moderators by being added to the `moderator_roles` table.  They then log in through a separate endpoint (`POST /moderator/login`) and receive a short-lived moderator JWT (24 hours).

---

## Prerequisites

1. **The user must already be registered on the server** – you cannot add a phantom user as a moderator.  If the account does not exist yet, register it first via the normal `/register` flow (or ask the user to register).
2. You need an **admin token** issued by `POST /admin/login`.
3. Know the user's full internal ID in the form `username@server_id` (e.g. `alice@server_a`).

---

## Step 1 – Admin Login

```bash
curl -X POST http://localhost:8080/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "your-admin-password"}'
```

**Successful response**:
```json
{
  "token": "<admin-jwt>",
  "message": "admin login successful"
}
```

Save the `token` value – you will pass it as a `Bearer` token for all subsequent admin calls.

---

## Step 2 – Assign a Moderator

```bash
curl -X POST http://localhost:8080/admin/moderators/assign \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin-jwt>" \
  -d '{"username": "alice"}'
```

| Field | Description |
|---|---|
| `username` | The registered username to promote. The server looks up their full internal ID automatically. |
| `user_id` | *(optional)* Full internal ID override (`username@server_id`). Only needed if the same username exists on multiple servers. |

**Successful response**:
```json
{
  "message": "moderator role assigned",
  "user_id": "alice@server_a"
}
```

> **Note**: Calling this endpoint for a user who is already a moderator is safe – it updates their record (`ON CONFLICT DO UPDATE`).

> **Error – user not found**: If the username isn't registered, you'll get `404 user not found`. The user must register before being made a moderator.

---

## Step 3 – Moderator Login

The moderator logs in through the dedicated moderator login endpoint (separate from the regular `/login`):

```bash
curl -X POST http://localhost:8080/moderator/login \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "alice-password"}'
```

**Successful response**:
```json
{
  "token": "<moderator-jwt>",
  "message": "moderator login successful",
  "user_id": "alice@server_a"
}
```

The token expires after **24 hours**.  The moderator must re-login afterwards.

> This endpoint deliberately **rejects** login if the user is not in `moderator_roles`, even if the password is correct.

---

## Step 4 – Using the Moderator Frontend

The moderator frontend is located at `federated-frontend/federated-moderator/`.

1. Start the moderator app (port 3002 by default):
   ```powershell
   cd federated-frontend\federated-moderator
   npm run dev
   ```
2. Open `http://localhost:3002` in a browser.
3. Log in using the moderator's credentials.
4. The moderation queue shows posts flagged as `PENDING_REVIEW`.

---

## Managing Moderators

### List all moderators

```bash
curl -X GET http://localhost:8080/admin/moderators/list \
  -H "Authorization: Bearer <admin-jwt>"
```

**Response**:
```json
{
  "moderators": [
    {
      "user_id": "alice@server_a",
      "username": "alice",
      "assigned_by": "admin",
      "assigned_at": "2025-01-15T10:30:00Z"
    }
  ],
  "count": 1
}
```

### Remove a moderator

```bash
curl -X POST http://localhost:8080/admin/moderators/remove \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin-jwt>" \
  -d '{"username": "alice"}'
```

You can also pass `user_id` instead:
```bash
  -d '{"user_id": "alice@server_a"}'
```

**Successful response**:
```json
{
  "message": "moderator role removed",
  "user_id": "alice@server_a"
}
```

After removal, any existing moderator JWT for that user becomes **functionally revoked** on the next request because the middleware re-validates claims against `IsAdmin` / `IsModerator` flags embedded in the JWT.  However, since JWTs are stateless, any token issued before removal will still contain `IsModerator: true` until it naturally expires (24 hours).  If you need immediate revocation, rotate `JWT_SECRET` in the server's environment and restart.

---

## What Moderators Can Do

Endpoints protected by `ModeratorAuthMiddleware` accept tokens where **either** `IsModerator` **or** `IsAdmin` is `true`.

| Action | Endpoint | Method |
|---|---|---|
| View post queue | (moderator frontend) | — |
| Approve a post | `/moderator/posts/approve` | POST |
| Reject a post | `/moderator/posts/reject` | POST |
| View flagged posts | `/moderator/posts/queue` | GET |

Post visibility states progress through:

```
PUBLIC  ←→  PENDING_REVIEW  →  HIDDEN / REJECTED
```

Moderators move posts between these states.  Admins can perform the same actions (their token satisfies the same middleware).

---

## Database Schema

```sql
-- Assigned moderators
CREATE TABLE IF NOT EXISTS moderator_roles (
    user_id     TEXT PRIMARY KEY,   -- e.g. alice@server_a
    username    TEXT NOT NULL,
    assigned_by TEXT NOT NULL,      -- admin username who made the assignment
    assigned_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

The `identities` table is unmodified – moderators are ordinary users with an extra row in `moderator_roles`.

---

## Running with Docker

All commands above target `http://localhost:8080` (Server A's identity service).  For Server B, use port `8090`.

```powershell
# Start all services
.\start-all.ps1

# Check identity service health
curl http://localhost:8080/health

# Stop all services
.\stop-all.ps1
```

If you need to query the database directly to confirm moderator assignment:

```powershell
docker exec -it fedinet-go-postgres-server-a-1 psql -U postgres -d fedinet_server_a `
  -c "SELECT * FROM moderator_roles;"
```

---

## Quick Reference

```
POST /admin/login                    → get admin token
POST /admin/moderators/assign        → make a user a moderator  (admin token) — body: {"username":"alice"}
GET  /admin/moderators/list          → list all moderators       (admin token)
POST /admin/moderators/remove        → revoke moderator role     (admin token) — body: {"username":"alice"}
POST /moderator/login                → moderator gets their JWT  (no token needed)
```
