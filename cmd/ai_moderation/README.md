# Federated Social Network with AI Moderation

A full-stack federated social network platform featuring automated, AI-driven content moderation. This project leverages a robust microservices architecture, separating the client interface, core backend logic, and heavy machine learning workloads into dedicated services.



## 🏗 Architecture Overview

The system is composed of three primary services:
1. **Frontend**: A modern, reactive user interface built with Next.js and TypeScript.
2. **Core Backend**: A high-performance Go (Golang) server managing federation logic, user routing, and database interactions.
3. **AI Moderation Service**: A standalone Python FastAPI microservice that processes text payloads through a Transformer-based machine learning model to evaluate toxicity and flag inappropriate content.

### System Flow

```text
+-------------------+       +--------------------+       +-----------------------+
|                   |       |                    |       |                       |
|   Next.js Client  | <---> |   Go Backend API   | <---> | PostgreSQL Database   |
|   (Port 3000)     |       |   (Port 8081)      |       | (Port 5432)           |
|                   |       |                    |       |                       |
+-------------------+       +---------+----------+       +-----------------------+
                                      |
                                      | HTTP Request (Content Payload)
                                      v
                            +---------+----------+
                            |                    |
                            | AI Moderation Svc  |
                            | (Python FastAPI)   |
                            | (Port 9000)        |
                            +--------------------+

🛠 Tech Stack
Frontend: Next.js, React, TypeScript

Backend: Go (Golang)

AI Service: Python, FastAPI, Uvicorn, PyTorch, Hugging Face Transformers

Database: PostgreSQL

🔀 Branch Information
Testing/Integration Branch: ai-moderation-test

If you are pulling this code to test the moderation pipeline, ensure you have checked out this specific branch:


git checkout ai-moderation-test
git pull origin ai-moderation-test

Getting Started
Follow these steps in order to spin up the entire local development environment.

1. Database Setup

The backend requires a running PostgreSQL database.

Ensure your PostgreSQL service is running.

Create a local database for the project (e.g., postgres or federation_db).

Prepare your connection string format. You will need to export this as the DATABASE_URL environment variable when running the backend:


postgres://username:password@localhost:5432/dbname?sslmode=disable

. AI Moderation Service Setup

Because machine learning dependencies can be heavy, this is isolated in its own service.

Navigate to the AI service directory (if applicable, or stay in project root depending on your structure):

Install the required Python dependencies:

Bash
pip install -r requirements.txt
(Note: It is highly recommended to use a virtual environment like python3 -m venv venv and source venv/bin/activate before installing).

⚠️ CRITICAL: Model Download Instructions
Because the AI model file exceeds GitHub's 100MB file size limit, it is not included in this repository. You must download it manually.

Download Link: [INSERT_HUGGINGFACE_OR_DRIVE_LINK_HERE]

Placement: Once downloaded, the model file(s) must be placed exactly in the following directory:

Plaintext
cmd/ai_moderation/models/
Verification: Open cmd/ai_moderation/main.py and verify that the local model loading path matches your extraction folder.

Start the AI service:

Bash
python3 cmd/ai_moderation/main.py
The service will start on port 9000.

3. Core Backend (Go) Setup

Open a new terminal window.

Run the Go server, injecting your specific database credentials:

Bash
DATABASE_URL="postgres://username:password@localhost:5432/dbname?sslmode=disable" go run cmd/federation/main.go
The backend API will start on port 8081.

Production Build Note:
If you need to compile a binary for production deployment:

Bash
go build -o federation ./cmd/federation/main.go
./federation
4. Frontend Setup

Open a third terminal window.

Navigate to the frontend directory:

Bash
cd federated-frontend
Install Node dependencies:

Bash
npm install
Start the Next.js development server:

Bash
npm run dev
The frontend client will start on port 3000.

🔌 Port Summary
If you encounter address binding errors, ensure these ports are free:

Service	Port	Description
Frontend (Next.js)	3000	User-facing web client and admin dashboard
Backend (Go)	8081	Core API, federation routing, and DB operations
AI Service (FastAPI)	9000	Text moderation and toxicity scoring
PostgreSQL	5432	Standard database port
🔄 Development Workflow
To fully test the integration:

Ensure Postgres is running.

Start the AI Moderation service (port 9000).

Start the Go Backend (port 8081).

Start the Next.js Frontend (port 3000).

Open http://localhost:3000 in your browser. Actions taken in the UI will flow through the Go backend, trigger the AI model for moderation scoring, log to the database, and reflect back on the frontend.