"""
classifier.py — HuggingFace-backed content moderation scorer.

Primary model  : unitary/toxic-bert
Fallback model : martin-ha/toxic-comment-model

Both models are multi-label classifiers that return per-label probabilities.
We normalise their outputs onto four canonical fields:
  toxicity, hate, spam, threat
"""

import logging
from typing import Dict

log = logging.getLogger("moderation.classifier")

# ── Label mapping tables ──────────────────────────────────────────────────────
# unitary/toxic-bert outputs these labels (lowercase):
#   toxic, severe_toxic, obscene, threat, insult, identity_hate
_TOXIC_BERT_MAP = {
    "toxic":         "toxicity",
    "severe_toxic":  "toxicity",   # merged into toxicity (max)
    "obscene":       "toxicity",   # merged into toxicity
    "insult":        "toxicity",   # merged into toxicity
    "threat":        "threat",
    "identity_hate": "hate",
}

# martin-ha/toxic-comment-model outputs:
#   toxic, severe_toxic, obscene, threat, insult, identity_hate  (same schema)
_MARTIN_MAP = _TOXIC_BERT_MAP  # identical schema → reuse

# Fallback / safe default
_SAFE_SCORES: Dict[str, float] = {
    "toxicity":  0.0,
    "hate":      0.0,
    "spam":      0.0,
    "threat":    0.0,
    "confidence": 0.0,
}


def _merge_scores(raw: list) -> Dict[str, float]:
    """
    raw  : list of {"label": str, "score": float}  (from HF pipeline)
    Returns canonical dict {toxicity, hate, spam, threat, confidence}.
    We take the *max* score across all labels that map to the same field.
    """
    out = {"toxicity": 0.0, "hate": 0.0, "spam": 0.0, "threat": 0.0}

    for item in raw:
        label = item["label"].lower()
        score = float(item["score"])

        # Try direct map
        canonical = _TOXIC_BERT_MAP.get(label)
        if canonical:
            out[canonical] = max(out[canonical], score)

    # confidence = max single score (proxy for model certainty)
    confidence = max(out.values()) if out else 0.0
    out["confidence"] = round(confidence, 4)
    for k in ("toxicity", "hate", "spam", "threat"):
        out[k] = round(out[k], 4)
    return out


class ModerationClassifier:
    """Loads a HuggingFace text-classification pipeline and scores text."""

    PRIMARY_MODEL  = "unitary/toxic-bert"
    FALLBACK_MODEL = "martin-ha/toxic-comment-model"

    def __init__(self):
        self.pipeline     = None
        self.model_loaded = False
        self._load_model()

    # ── Model loading ─────────────────────────────────────────────────────────
    def _load_model(self):
        for model_name in (self.PRIMARY_MODEL, self.FALLBACK_MODEL):
            try:
                log.info("Loading model: %s …", model_name)
                from transformers import pipeline as hf_pipeline
                self.pipeline = hf_pipeline(
                    "text-classification",
                    model=model_name,
                    top_k=None,          # return ALL labels
                    truncation=True,
                    max_length=512,
                )
                self.model_loaded = True
                log.info("Model loaded successfully: %s", model_name)
                return
            except Exception as exc:
                log.warning("Failed to load %s: %s", model_name, exc)

        log.error(
            "All models failed to load — service will return SAFE defaults."
        )

    # ── Scoring ───────────────────────────────────────────────────────────────
    def score(self, text: str) -> Dict[str, float]:
        """
        Returns a dict: {toxicity, hate, spam, threat, confidence}.
        Falls back to all-zeros if inference fails.
        """
        if not self.model_loaded or self.pipeline is None:
            log.warning("Model not loaded — returning safe defaults.")
            return dict(_SAFE_SCORES)

        try:
            raw = self.pipeline(text)
            # HF pipeline returns list[list[dict]] when top_k=None
            if isinstance(raw, list) and raw and isinstance(raw[0], list):
                raw = raw[0]
            return _merge_scores(raw)
        except Exception as exc:
            log.error("Inference error: %s — returning safe defaults.", exc)
            return dict(_SAFE_SCORES)
