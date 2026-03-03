"""
tests.py — lightweight integration tests (no live model needed).

Run with:  pytest tests.py -v
"""

import json
import os
from unittest.mock import patch, MagicMock

import pytest
from fastapi.testclient import TestClient

os.environ.setdefault("API_KEY", "test-secret")
os.environ.setdefault("FEEDBACK_FILE", "/tmp/test_feedback.jsonl")


# Patch the classifier before importing main so no GPU / model download needed
with patch("classifier.ModerationClassifier._load_model"):
    from main import app, classifier

HEADERS = {"Authorization": "Bearer test-secret"}
client = TestClient(app)


# ── /health ────────────────────────────────────────────────────────────────────
def test_health_ok():
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"


# ── /moderate auth ─────────────────────────────────────────────────────────────
def test_moderate_requires_auth():
    r = client.post("/moderate", json={"input": "hello"})
    assert r.status_code == 403


def test_moderate_wrong_token():
    r = client.post(
        "/moderate",
        json={"input": "hello"},
        headers={"Authorization": "Bearer wrong"},
    )
    assert r.status_code == 401


# ── /moderate scoring ──────────────────────────────────────────────────────────
def _mock_score(scores: dict):
    classifier.model_loaded = True
    classifier.pipeline = MagicMock()
    with patch.object(classifier, "score", return_value=scores):
        yield


def _moderate(text: str):
    return client.post("/moderate", json={"input": text}, headers=HEADERS)


@pytest.mark.parametrize("scores,expected_rec", [
    # ESCALATE_IMMEDIATELY
    (
        {"toxicity": 0.5, "hate": 0.1, "spam": 0.0, "threat": 0.80, "confidence": 0.80},
        "ESCALATE_IMMEDIATELY",
    ),
    # FLAG_FOR_REVIEW — toxicity
    (
        {"toxicity": 0.85, "hate": 0.1, "spam": 0.0, "threat": 0.1, "confidence": 0.85},
        "FLAG_FOR_REVIEW",
    ),
    # FLAG_FOR_REVIEW — hate
    (
        {"toxicity": 0.3, "hate": 0.80, "spam": 0.0, "threat": 0.1, "confidence": 0.80},
        "FLAG_FOR_REVIEW",
    ),
    # MARK_AS_SPAM
    (
        {"toxicity": 0.1, "hate": 0.05, "spam": 0.90, "threat": 0.0, "confidence": 0.90},
        "MARK_AS_SPAM",
    ),
    # SAFE
    (
        {"toxicity": 0.1, "hate": 0.05, "spam": 0.10, "threat": 0.0, "confidence": 0.10},
        "SAFE",
    ),
])
def test_recommendation(scores, expected_rec):
    with patch.object(classifier, "score", return_value=scores):
        r = _moderate("some text")
    assert r.status_code == 200
    assert r.json()["recommendation"] == expected_rec


def test_empty_input_rejected():
    r = client.post("/moderate", json={"input": "   "}, headers=HEADERS)
    assert r.status_code == 400


def test_model_not_loaded_returns_safe():
    classifier.model_loaded = False
    r = _moderate("I hate you")
    data = r.json()
    assert r.status_code == 200
    assert data["recommendation"] == "SAFE"
    assert data["toxicity"] == 0.0
    classifier.model_loaded = True  # restore


# ── /feedback ──────────────────────────────────────────────────────────────────
def test_feedback_saves(tmp_path, monkeypatch):
    fb_file = tmp_path / "feedback.jsonl"
    monkeypatch.setenv("FEEDBACK_FILE", str(fb_file))

    import main as m
    m.FEEDBACK_FILE = fb_file

    r = client.post(
        "/feedback",
        json={"post_id": "p1", "content": "spam content", "label": "spam"},
        headers=HEADERS,
    )
    assert r.status_code == 200
    assert r.json()["post_id"] == "p1"

    lines = fb_file.read_text().strip().splitlines()
    assert len(lines) == 1
    record = json.loads(lines[0])
    assert record["label"] == "spam"
    assert record["post_id"] == "p1"


def test_feedback_invalid_label():
    r = client.post(
        "/feedback",
        json={"post_id": "p2", "content": "x", "label": "violence"},
        headers=HEADERS,
    )
    assert r.status_code == 400
