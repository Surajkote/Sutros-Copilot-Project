# Sovereign Copilot: Local Multi-Agent RAG Clinical Framework

A high-privacy, local-first clinical copilot designed for secure medical document ingestion, processing, and real-time structured analysis. This system employs a dual hybrid database strategy and a split multi-agent routing architecture to ensure strict data isolation and zero-leakage compliance inside a self-contained local environment.



## 🚀 Quick Start Guide (For Non-Technical Users)

Follow these step-by-step instructions to get the application running on your computer. You do not need to be a software engineer to do this! It will run exactly the same on a Mac or Windows computer.

### Step 1: Install Required Software
Before you begin, you need to download and install two standard applications:
1.  **Docker Desktop:** This is the engine that runs our application securely. [Download Docker Desktop here](https://www.docker.com/products/docker-desktop/) and install it. Once installed, **open the Docker Desktop app** and leave it running in the background.
2.  **Git:** This allows you to download the project code. [Download Git here](https://git-scm.com/downloads) and install it with the default settings.

### Step 2: Download the Project
You will need to use your computer's "Terminal" (on Mac) or "Command Prompt" (on Windows).
1.  Open the **Terminal** or **Command Prompt** app.
2.  Copy and paste the following command, then press **Enter** to download the code:
    ```bash
    git clone https://github.com/Surajkote/sovereign-copilot.git
    ```
    *(Note: Replace the URL above with the actual link to this repository).*
3.  Next, tell your terminal to go inside the folder you just downloaded by typing:
    ```bash
    cd sovereign-copilot
    ```

### Step 3: Add Your Secret Key
The application needs a private key to access the AI brain safely. 
1.  Inside the `sovereign-copilot` folder on your computer, create a new plain text file and name it exactly `.env` (make sure it starts with a dot).
2.  Open this `.env` file using any text editor (like Notepad or TextEdit) and paste in your Groq API key exactly like this:
    ```text
    GROQ_API_KEY=gsk_your_actual_secret_api_key_here
    ```
3.  Save and close the file.

### Step 4: Start the Application
Now that Docker is running in the background and your key is saved, go back to your Terminal (make sure you are still inside the `sovereign-copilot` folder) and type:

```bash
docker-compose up --build
```

Press **Enter**. Your computer will now automatically download and build all the necessary secure environments. This might take a few minutes the first time you run it.

### Step 5: Open the Dashboard
Once the text stops moving rapidly in your terminal, the system is ready! Open Google Chrome or Safari and click these links:

*   **The Main Dashboard (Start Here):** [http://localhost:3000](http://localhost:3000)
*   *Database Admin Panel (Optional):* [http://localhost:6333/dashboard](http://localhost:6333/dashboard)

### Step 6: Shutting Down safely
When you are finished using the application, go back to your Terminal window where the code is running, hold down the **Ctrl** key and press **C**. 
Once it stops, type this command to safely turn off the engines:

```bash
docker-compose down
```

---

## 🏛️ System Architecture Overview

The framework leverages a **Local-First Distributed Architecture** split across four distinct microservice containers connected over an internal bridge network:

```text
[Client Browser] ---> (Frontend Container: React/Nginx @ Port 3000)
                              |
                     (REST API / SSE Stream)
                              v
             (Backend Container: Go Engine @ Port 8080)
               /                                    \
              v                                      v
(Postgres Container @ 5432)             (Qdrant Container @ 6333)
 [Tabular Facts / Vitals]                 [Dense Vector Embeddings]
```

### 1. The Dual-Storage Database Strategy
*   **Relational Storage (Postgres 16):** Managed via GORM. Persists normalized, highly structured tabular data (Patient Profiles, Vitals, Timestamps, and Raw Text Logs). This optimizes standard analytical indexing queries.
*   **Vector Storage (Qdrant):** Manages high-dimensional semantic search indexing. It segments and stores vector representations of unstructured medical records, enabling semantic context matching during Retrieval-Augmented Generation (RAG).

### 2. Multi-Agent Routing Logic
*   **Agent A (The Scribe):** Optimized via a fast, deterministic LLM context window (`gpt-oss-20b` parameter scale equivalent). It acts as an aggressive normalization filter, parsing messy clinical language into explicit structural JSON models while dropping non-clinical conversational noise.
*   **Agent B (The Specialist):** Executed via a dense reasoning model (`gpt-oss-120b` parameter scale equivalent). It ingests real-time semantic payloads retrieved from the local Qdrant collection, outputting diagnostic red flags and treatment considerations strictly bound to verified text citations.

---

## 📁 Codebase File-by-File Directory Structure

```text
sovereign-copilot/
├── docker-compose.yml       # Master container orchestration configuration
├── README.md                # Project documentation and start guide
├── .env                     # Local environment secrets (not in Git)
├── .gitignore               # Ignored version control paths
│
├── backend/                 # Go API Engine
│   ├── Dockerfile           # Multi-stage Go 1.26 container compilation
│   ├── main.go              # Primary application server and routing logic
│   ├── go.mod               # Go module definitions
│   ├── go.sum               # Go dependency checksums
│   ├── .env.example         # Example environment template
│   └── .gitignore
│
└── frontend/                # React UI Dashboard
    ├── Dockerfile           # Nginx-served static React build
    ├── package.json         # NPM dependencies
    ├── vite.config.js       # Vite bundler configuration
    ├── index.html           # Main DOM injection point
    └── src/
        ├── App.jsx          # Primary UI interface and state management
        ├── index.css        # Global CSS, animations, and glassmorphism styling
        └── main.jsx         # React application bootstrap
```

---

## ⚙️ Detailed Component Specifications

### 1. Backend Service (`backend/main.go`)
Written in Go, the application server exposes structured REST endpoints and high-performance streaming loops:
*   `POST /api/upload`: Handles multi-part form document ingestion. Orchestrates asynchronous tasks to write structured profiles to Postgres, while concurrently vectorizing text blocks to populate Qdrant collection spaces. Includes strict guardrails rejecting non-medical files (resumes, contracts, etc.).
*   `GET /api/specialist/stream`: Establishes a Server-Sent Events (SSE) pipe. Streams chunked reasoning text directly to the frontend. To prevent the protocol from stripping leading Whitespace sequences, tokens are safely wrapped inside structured JSON envelopes: `{"text": "chunk"}`.
*   **Internal Network Mapping:** Configured to dynamically bypass host-level loops. Connects to relational stores using internal container resolution string addresses: `host=postgres` and `http://qdrant:6333`.

### 2. Frontend Dashboard (`frontend/src/App.jsx`)
A modern, minimalist clinical control workspace optimized for high legibility:
*   **Markdown Core Parsing:** Integrates `react-markdown` paired with `remark-gfm` (GitHub Flavored Markdown). Converts streamed string sequences dynamically into semantic elements, auto-rendering clinical telemetry tables from unstructured delimiters (`|`).
*   **State Machine Management:** Controls upload statuses, tracking active SSE network socket pipelines (`EventSource`). Employs safe parsing loops to safely handle high-frequency data states.

### 3. Container Topology Configurations

**Backend Compilation File (`backend/Dockerfile`)**
Utilizes a multi-stage compilation flow to guarantee an ultra-light container footprint while enforcing strict type safety matching Go specifications (≥ 1.26):
```dockerfile
# Stage 1: Build
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o sovereign-backend .

# Stage 2: Serve
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/sovereign-backend .
COPY .env.example .env
EXPOSE 8080
CMD ["./sovereign-backend"]
```

**Frontend Serving File (`frontend/Dockerfile`)**
Compiles frontend React scripts into compact static assets and hosts them using an enterprise-grade Nginx server instance:
```dockerfile
# Stage 1: Build static assets
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

# Stage 2: Serve with Nginx
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

**Master Orchestration Plan (`docker-compose.yml`)**
Configures all network and volume interactions, mapping persistent storage drivers to your local disk to prevent data loss when containers are restarted:
```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: copilot_postgres
    environment:
      POSTGRES_USER: medical_admin
      POSTGRES_PASSWORD: secure_password123
      POSTGRES_DB: sovereign_health
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  qdrant:
    image: qdrant/qdrant:latest
    container_name: copilot_qdrant
    ports:
      - "6333:6333"
    volumes:
      - qdrant_data:/qdrant/storage

  backend:
    build: ./backend
    container_name: copilot_backend
    ports:
      - "8080:8080"
    environment:
      - GROQ_API_KEY=${GROQ_API_KEY}
    depends_on:
      - postgres
      - qdrant

  frontend:
    build: ./frontend
    container_name: copilot_frontend
    ports:
      - "3000:80"
    depends_on:
      - backend

volumes:
  postgres_data:
  qdrant_data:
```

---

## 🔒 Security & Local Verification Protocol
*   **Data Isolation:** No clinical data or files are transmitted to public cloud systems or third-party inference layers (aside from the strictly sandboxed LLM inference routing). All context retrieval matches occur strictly against the local Qdrant container over port 6333.
*   **Secret Integrity:** The master key (`GROQ_API_KEY`) is read from the local terminal environment into the Docker layer at execution time, preventing hardcoded leaks into persistent application source files.
