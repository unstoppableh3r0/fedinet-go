"""
Content Moderation Microservice
FastAPI app exposing POST /moderate, GET /health, POST /feedback
"""

import os
import json
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from fastapi import FastAPI, HTTPException, Depends, status
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from pydantic import BaseModel

from classifier import ModerationClassifier

# ── Logging ──────────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
log = logging.getLogger("moderation")

# ── Config ────────────────────────────────────────────────────────────────────
API_KEY       = os.getenv("API_KEY", "changeme")
FEEDBACK_FILE = Path(os.getenv("FEEDBACK_FILE", "feedback.jsonl"))

# ── App & security ────────────────────────────────────────────────────────────
app = FastAPI(title="Content Moderation Service", version="1.0.0")
security = HTTPBearer()
classifier = ModerationClassifier()


def verify_token(creds: HTTPAuthorizationCredentials = Depends(security)):
    if creds.credentials != API_KEY:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid or missing API key",
        )
    return creds.credentials


# ── Schemas ───────────────────────────────────────────────────────────────────
class ModerateRequest(BaseModel):
    input: str


class ModerateResponse(BaseModel):
    toxicity:       float
    hate:           float
    spam:           float
    threat:         float
    confidence:     float
    recommendation: str


class FeedbackRequest(BaseModel):
    post_id: str
    content: str
    label:   str   # toxic | safe | spam | threat


class FeedbackResponse(BaseModel):
    status:  str
    post_id: str


# ── Thresholds ────────────────────────────────────────────────────────────────
def compute_recommendation(
    toxicity: float,
    hate:     float,
    spam:     float,
    threat:   float,
) -> str:
    if threat   > 0.70:  return "ESCALATE_IMMEDIATELY"
    if toxicity > 0.80 or hate > 0.75: return "FLAG_FOR_REVIEW"
    if spam     > 0.85:  return "MARK_AS_SPAM"
    return "SAFE"


# ── Endpoints ─────────────────────────────────────────────────────────────────
@app.get("/health")
def health():
    return {
        "status": "ok",
        "model_loaded": classifier.model_loaded,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


@app.post("/moderate", response_model=ModerateResponse)
def moderate(
    body:  ModerateRequest,
    _token: str = Depends(verify_token),
):
    text = body.input.strip()
    if not text:
        raise HTTPException(status_code=400, detail="`input` must not be empty")

    scores = classifier.score(text)

    recommendation = compute_recommendation(
        toxicity=scores["toxicity"],
        hate=scores["hate"],
        spam=scores["spam"],
        threat=scores["threat"],
    )

    log.info(
        "moderated | rec=%s tox=%.2f hate=%.2f spam=%.2f threat=%.2f | %.40r…",
        recommendation, scores["toxicity"], scores["hate"],
        scores["spam"], scores["threat"], text,
    )

    return ModerateResponse(
        toxicity=scores["toxicity"],
        hate=scores["hate"],
        spam=scores["spam"],
        threat=scores["threat"],
        confidence=scores["confidence"],
        recommendation=recommendation,
    )


@app.post("/feedback", response_model=FeedbackResponse)
def feedback(
    body:   FeedbackRequest,
    _token: str = Depends(verify_token),
):
    valid_labels = {"toxic", "safe", "spam", "threat"}
    if body.label not in valid_labels:
        raise HTTPException(
            status_code=400,
            detail=f"`label` must be one of: {sorted(valid_labels)}",
        )

    record = {
        "post_id":   body.post_id,
        "content":   body.content,
        "label":     body.label,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }
    with FEEDBACK_FILE.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(record, ensure_ascii=False) + "\n")

    log.info("feedback saved | post_id=%s label=%s", body.post_id, body.label)
    return FeedbackResponse(status="saved", post_id=body.post_id)
