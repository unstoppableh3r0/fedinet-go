# Content Moderation Microservice

Python / FastAPI microservice backed by HuggingFace `unitary/toxic-bert`.

## Endpoints

| Method | Path        | Auth   | Description                        |
|--------|-------------|--------|------------------------------------|
| GET    | /health     | —      | Liveness / model-loaded status     |
| POST   | /moderate   | Bearer | Score & classify a text string     |
| POST   | /feedback   | Bearer | Append labelled example to JSONL   |

---

## Quick start (Docker)

```bash
# Build & run
docker compose up --build

# Or without Compose
docker build -t moderation-service .
docker run -p 8090:8090 -e API_KEY=supersecret moderation-service
```

---

## POST /moderate

**Request**
```json
POST /moderate
Authorization: Bearer <API_KEY>
Content-Type: application/json

{ "input": "I will destroy you." }
```

**Response**
```json
{
  "toxicity":       0.93,
  "hate":           0.12,
  "spam":           0.02,
  "threat":         0.81,
  "confidence":     0.93,
  "recommendation": "ESCALATE_IMMEDIATELY"
}
```

### Recommendation thresholds (evaluated in priority order)

| Recommendation        | Condition                            |
|-----------------------|--------------------------------------|
| ESCALATE_IMMEDIATELY  | threat > 0.70                        |
| FLAG_FOR_REVIEW       | toxicity > 0.80 **or** hate > 0.75  |
| MARK_AS_SPAM          | spam > 0.85                          |
| SAFE                  | everything else                      |

---

## POST /feedback

```json
POST /feedback
Authorization: Bearer <API_KEY>
Content-Type: application/json

{
  "post_id": "abc123",
  "content": "buy cheap meds now!!!",
  "label":   "spam"
}
```

Valid labels: `toxic` | `safe` | `spam` | `threat`

Records are appended as newline-delimited JSON to `feedback.jsonl`
(default path: `/data/feedback.jsonl` inside the container).

---

## Environment variables

| Variable        | Default          | Description                        |
|-----------------|------------------|------------------------------------|
| `API_KEY`       | `changeme`       | Bearer token expected by the API   |
| `PORT`          | `8090`           | Port uvicorn listens on            |
| `FEEDBACK_FILE` | `/data/feedback.jsonl` | Path for feedback log        |

---

## Model fallback chain

1. `unitary/toxic-bert` — primary
2. `martin-ha/toxic-comment-model` — fallback if primary fails
3. All-zero scores + `SAFE` — if both models fail at load time

Model weights are baked into the Docker image at build time for fast cold starts.
Remove the pre-download `RUN python` block in the `Dockerfile` to shrink the image.

---

## Label mapping

`toxic-bert` raw labels → canonical fields

| Raw label      | Canonical field |
|----------------|-----------------|
| `toxic`        | toxicity        |
| `severe_toxic` | toxicity (max)  |
| `obscene`      | toxicity (max)  |
| `insult`       | toxicity (max)  |
| `threat`       | threat          |
| `identity_hate`| hate            |
| *(none)*       | spam            |

> **Note:** Neither model has a dedicated spam classifier.
> The `spam` score will be `0.0` unless you fine-tune or add a second pipeline.
> The thresholds are intentionally conservative so that non-spam content is never mis-escalated.

---

## Local development (no Docker)

```bash
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

export API_KEY=dev-secret
uvicorn main:app --reload --port 8090
```

Interactive docs: http://localhost:8090/docs
