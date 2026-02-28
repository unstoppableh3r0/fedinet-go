from fastapi import FastAPI, Request
from transformers import pipeline
import uvicorn

app = FastAPI()

# Loads your model from the directory in your screenshot
classifier = pipeline("text-classification", model="cmd/ai_moderation/model")

@app.post("/moderate")
async def moderate(request: Request):
    data = await request.json()
    text = data.get("text", "")
    result = classifier(text)
    print(f"DEBUG: Model raw output: {result}")
    return {"result": result}

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=9000)