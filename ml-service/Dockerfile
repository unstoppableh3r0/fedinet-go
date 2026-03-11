# ── Build stage ───────────────────────────────────────────────────────────────
FROM python:3.11-slim AS builder

WORKDIR /app

# Install build deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential git curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --upgrade pip \
    && pip install --no-cache-dir \
       --index-url https://download.pytorch.org/whl/cpu \
       --extra-index-url https://pypi.org/simple \
       -r requirements.txt

# Pre-download models into the image so cold-start is instant
# (remove this block if you prefer smaller images + runtime download)
RUN python - <<'EOF'
from transformers import pipeline
print("Downloading primary model…")
try:
    pipeline("text-classification", model="unitary/toxic-bert", top_k=None)
    print("Primary model cached.")
except Exception as e:
    print(f"Primary model failed ({e}), trying fallback…")
    pipeline("text-classification", model="martin-ha/toxic-comment-model", top_k=None)
    print("Fallback model cached.")
EOF

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM python:3.11-slim

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /usr/local/lib/python3.11 /usr/local/lib/python3.11
COPY --from=builder /usr/local/bin           /usr/local/bin
# Copy HuggingFace model cache
COPY --from=builder /root/.cache             /root/.cache

# Copy application code
COPY main.py       ./main.py
COPY classifier.py ./classifier.py

# Feedback file lives in a volume-mountable directory
RUN mkdir -p /data
ENV FEEDBACK_FILE=/data/feedback.jsonl

# Runtime env (override at container start)
ENV API_KEY=changeme
ENV PORT=8090

EXPOSE 8090

CMD ["sh", "-c", "uvicorn main:app --host 0.0.0.0 --port ${PORT}"]
