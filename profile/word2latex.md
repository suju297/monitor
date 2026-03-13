https://github.com/suju297/Word2LaTeX.git

# Word-to-LaTeX Server

A layout-aware DOCX to LaTeX conversion platform with:
- a FastAPI backend (`src/server`)
- a core conversion engine (`src/wordtolatex`)
- a modern Next.js frontend (`frontend/`)

The system is optimized for resumes, reports, and academic-style documents where preserving structure and visual intent matters.

## Highlights

- DOCX -> LaTeX conversion with ZIP packaging (`.tex` + extracted media).
- Dynamic, template-free generation mode (`dynamic=true`) for layout-sensitive output.
- Optional reference-PDF analysis (font, spacing, margins, visual regions).
- Optional calibration profiles (`auto`, `resume`, `academic_*`, `default`).
- Optional local LLM routing (GGUF model) and optional Gemini enhancement.
- Built-in web UI + API + CLI.
- In-memory rate limiting and lightweight usage stats.

## Architecture

Detailed C4-style architecture and system design diagrams are in:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

This includes:
- C4 Level 1: System Context
- C4 Level 2: Container Diagram
- C4 Level 3: Conversion Component Diagram
- Runtime request lifecycle diagram

### C4 Level 1 - System Context

![C4 Level 1 - System Context](docs/diagrams/c4-level1-context.png)

### C4 Level 2 - Container Diagram

![C4 Level 2 - Container Diagram](docs/diagrams/c4-level2-container.png)

### C4 Level 3 - Component Diagram

![C4 Level 3 - Component Diagram](docs/diagrams/c4-level3-component.png)

### Runtime Request Lifecycle

![Runtime Request Lifecycle](docs/diagrams/runtime-request-lifecycle.png)

## Repository Layout

```text
wordtolatex-server/
├── src/
│   ├── server/                  # FastAPI app (web + API)
│   └── wordtolatex/             # Core DOCX -> LaTeX pipeline
├── frontend/                    # Next.js UI
├── tests/                       # Unit/integration/corpus tests
├── scripts/                     # Evaluation + tooling scripts
├── tools/                       # Diagnostics + training helpers
├── docs/                        # Notes + architecture docs
└── pyproject.toml
```

## Conversion Pipeline (High Level)

1. Parse DOCX into intermediate representation (`docx_parser`).
2. Optionally classify document type/layout with local LLM router.
3. Resolve/generate reference PDF (optional but enabled by default).
4. Analyze reference layout (`layout_ml`) and inject layout hints.
5. Select/apply calibration profile (`calibration`).
6. Apply header image fallback when enabled.
7. Assign policy per block (`policy`).
8. Generate LaTeX (`dynamic_generator` or `generator`).
9. Write output + package ZIP for download.

## Prerequisites

- Python `>=3.10`
- [uv](https://docs.astral.sh/uv/) (recommended)
- LibreOffice/`soffice` (optional, needed for auto reference-PDF generation)
- Node.js `>=18` (only for Next.js frontend development)

## Quickstart (Backend API)

```bash
# 1) Create and activate virtual env
uv venv
source .venv/bin/activate

# 2) Install package (with dev extras)
uv pip install -e ".[dev]"

# 3) Run API server
uv run uvicorn server.main:app --reload --host 0.0.0.0 --port 8000
```

Open:
- API/Web root: `http://localhost:8000/`
- Health: `http://localhost:8000/health`

## Quickstart (Next.js Frontend)

```bash
cd frontend
npm install
WORDTOLATEX_API_BASE_URL=http://localhost:8000 npm run dev
```

Open `http://localhost:3000`.

## API Reference

### `GET /health`

Returns service status.

```bash
curl http://localhost:8000/health
```

### `GET /stats`

Returns in-memory counters:
- `visitor_count`
- `conversion_count`

### `POST /v1/convert`

Converts DOCX and returns a ZIP file.

Multipart form fields:
- `docx` (required)
- `ref_pdf` (optional)
- `options_json` (optional JSON string; only honored if `ALLOW_USER_OPTIONS=true`)

```bash
curl -X POST "http://localhost:8000/v1/convert" \
  -F "docx=@tests/corpus/Resume_1/src.docx" \
  -F "ref_pdf=@tests/corpus/Resume_1/ref.pdf" \
  -F 'options_json={"profile":"auto","dynamic":true,"header_fallback":true}' \
  -o wordtolatex_output.zip
```

### `POST /v1/convert/json`

Returns conversion response as JSON (no ZIP/media payload):

- `latex`
- `doc_type`
- `layout_style`
- `metadata` (empty unless `EXPOSE_METADATA=true`)

```bash
curl -X POST "http://localhost:8000/v1/convert/json" \
  -F "docx=@tests/corpus/Resume_1/src.docx" \
  -F 'options_json={"profile":"auto"}'
```

## Conversion Options (`options_json`)

Note: by default these are ignored (`ALLOW_USER_OPTIONS=false`).

- `profile`: `auto | academic_twocol | academic_singlecol | resume | default`
- `dynamic`: `true | false` (default `true`)
- `header_fallback`: `true | false` (default `true`)
- `local_llm`: `true | false | null` (null uses server default)
- `llm_model`: path to local GGUF model
- `use_gemini`: `true | false | null` (null uses server default)
- `calibrate`: `true | false`

## Configuration (Environment Variables)

`pydantic-settings` maps environment variables from `Settings` in `src/server/config.py`.
Use uppercase snake_case names.

Core:
- `APP_NAME` (default: `wordtolatex-server`)
- `API_PREFIX` (default: `/v1`)
- `ENVIRONMENT` (default: `local`)
- `LOG_LEVEL` (default: `INFO`)
- `TEMP_DIR_PREFIX` (default: `wordtolatex-`)

Rate limiting:
- `RATE_LIMIT_ENABLED` (default: `true`)
- `RATE_LIMIT_COOLDOWN_SECONDS` (default: `1800`)
- `RATE_LIMIT_TRUST_FORWARDED` (default: `true`)

Request behavior:
- `ALLOW_USER_OPTIONS` (default: `false`)
- `EXPOSE_METADATA` (default: `false`)

Reference PDF generation:
- `AUTO_REFERENCE_PDF` (default: `true`)
- `REFERENCE_PDF_COMMAND` (default: `soffice`)
- `REFERENCE_PDF_ARGS` (default: `--headless --convert-to pdf --outdir {outdir} {input}`)
- `REFERENCE_PDF_TIMEOUT_SECONDS` (default: `120`)

LLM defaults:
- `DEFAULT_USE_LOCAL_LLM` (default: `true`)
- `DEFAULT_USE_GEMINI` (default: `false`)
- `DEFAULT_LLM_MODEL` (default: unset)

## CLI Usage

Install package first, then run:

```bash
wordtolatex input.docx output.tex --profile auto --dynamic
```

Useful options:
- `--ref-pdf path/to/ref.pdf`
- `--calibrate`
- `--header-fallback/--no-header-fallback`
- `--local-llm/--no-local-llm`
- `--llm-model path/to/model.gguf`
- `--report report.json`

## Development & Testing

Install dev dependencies:

```bash
uv pip install -e ".[dev]"
```

Run tests:

```bash
uv run pytest
```

Run a focused test file:

```bash
uv run pytest tests/test_integration.py -q
```

## Troubleshooting

- `Reference PDF generation failed (command not found)`
  - Install LibreOffice or set `REFERENCE_PDF_COMMAND` + `REFERENCE_PDF_ARGS`.
- `429 Rate limit exceeded`
  - Wait for cooldown or reduce `RATE_LIMIT_COOLDOWN_SECONDS` in local dev.
- `options_json` appears ignored
  - Set `ALLOW_USER_OPTIONS=true`.
- Local LLM routing not applied
  - Install llm extras (`uv pip install -e ".[llm]"`) and set `DEFAULT_LLM_MODEL` or pass `llm_model`.

## Notes

- Runtime counters and rate-limit state are in-memory (single-process scope).
- Large model weights/datasets are intentionally not part of the core source layout.

# Architecture and System Design (C4 Style)

This document describes the system using C4-inspired views and a runtime flow diagram.
All diagrams use color/styling so responsibilities and trust boundaries are easy to scan.

## C4 Level 1 - System Context

```mermaid
flowchart LR
    user["User<br/>Uploads DOCX and downloads ZIP"]
    maintainer["Maintainer<br/>Operates services"]

    system["Word-to-LaTeX Platform<br/>DOCX to LaTeX conversion service"]

    office["LibreOffice / soffice<br/>Reference PDF generation"]
    gemini["Gemini API<br/>Optional enhancement"]
    localLlm["Local GGUF Model<br/>Optional routing and classification"]

    user -->|Request flow| system
    maintainer -->|Operations flow| system

    system -->|Reference PDF flow| office
    system -->|AI enhancement flow| gemini
    system -->|LLM routing flow| localLlm

    linkStyle 0 stroke:#F59E0B,stroke-width:3px;
    linkStyle 1 stroke:#64748B,stroke-width:3px;
    linkStyle 2 stroke:#0EA5A6,stroke-width:3px;
    linkStyle 3 stroke:#8B5CF6,stroke-width:3px;
    linkStyle 4 stroke:#0284C7,stroke-width:3px;

    classDef actor fill:#FFE6C9,stroke:#D97706,color:#7C2D12,stroke-width:2px;
    classDef system fill:#DBEAFE,stroke:#2563EB,color:#1E3A8A,stroke-width:2.5px;
    classDef external fill:#E0F2FE,stroke:#0891B2,color:#164E63,stroke-width:2px;

    class user,maintainer actor;
    class system system;
    class office,gemini,localLlm external;
```

## C4 Level 2 - Container Diagram

```mermaid
flowchart TB
    subgraph Client["Client Layer"]
      direction LR
      nextUi["Next.js UI"]
      browserUi["Built-in Web UI Client<br/>Browser"]
    end

    subgraph Api["FastAPI Application"]
      direction LR
      app["FastAPI App<br/>server.main"]
      convApi["Conversion API Router<br/>/v1/convert and /v1/convert/json"]
      webApi["Web Router<br/>/ and /stats"]
      svc["Conversion Service<br/>convert_document()"]
      state["In-memory State<br/>RateLimiter and UsageStats"]
    end

    subgraph Processing["Conversion Processing"]
      direction LR
      core["wordtolatex Core Library<br/>Parser, Layout, Calibration,<br/>Policy, Generation"]
      temp["Temp Workspace<br/>input.docx, ref.pdf,<br/>output, zip"]
    end

    subgraph External["Optional Integrations"]
      direction LR
      soffice["LibreOffice or soffice"]
      local["Local GGUF Model"]
      gemini["Gemini API"]
    end

    nextUi --> convApi
    browserUi --> webApi

    app --> convApi
    app --> webApi

    convApi --> svc
    convApi --> state
    webApi --> state

    svc --> core
    core --> temp

    svc -."ref PDF generation".-> soffice
    svc -."LLM routing".-> local
    svc -."AI enhancement".-> gemini

    linkStyle 0 stroke:#F59E0B,stroke-width:3px;
    linkStyle 1 stroke:#F59E0B,stroke-width:3px;
    linkStyle 2 stroke:#2563EB,stroke-width:3px;
    linkStyle 3 stroke:#2563EB,stroke-width:3px;
    linkStyle 4 stroke:#2563EB,stroke-width:3px;
    linkStyle 5 stroke:#D97706,stroke-width:3px;
    linkStyle 6 stroke:#D97706,stroke-width:3px;
    linkStyle 7 stroke:#16A34A,stroke-width:3px;
    linkStyle 8 stroke:#9333EA,stroke-width:3px;
    linkStyle 9 stroke:#0EA5A6,stroke-width:3px,stroke-dasharray:6 4;
    linkStyle 10 stroke:#0284C7,stroke-width:3px,stroke-dasharray:6 4;
    linkStyle 11 stroke:#8B5CF6,stroke-width:3px,stroke-dasharray:6 4;

    classDef client fill:#FEF3C7,stroke:#D97706,color:#78350F,stroke-width:2px;
    classDef api fill:#DBEAFE,stroke:#2563EB,color:#1E3A8A,stroke-width:2px;
    classDef processing fill:#DCFCE7,stroke:#16A34A,color:#14532D,stroke-width:2px;
    classDef ext fill:#E0F2FE,stroke:#0891B2,color:#164E63,stroke-width:2px;

    class nextUi,browserUi client;
    class app,convApi,webApi,svc,state api;
    class core,temp processing;
    class soffice,local,gemini ext;
```

## C4 Level 3 - Component Diagram (Conversion Path)

```mermaid
flowchart TB
    subgraph phase1["Phase 1 - Request and Guardrails"]
      direction LR
      request["POST /v1/convert<br/>docx, ref_pdf, options_json"]
      options["parse_options()<br/>guarded by ALLOW_USER_OPTIONS"]
      limiter["enforce_rate_limit()"]
      request --> options --> limiter
    end

    subgraph phase2["Phase 2 - Orchestration"]
      direction LR
      convertSvc["convert_document()<br/>orchestration"]
    end

    subgraph phase3["Phase 3 - Conversion Pipeline"]
      direction LR
      parseDocx["parse_docx()"]
      refPdf["generate_reference_pdf()<br/>if ref_pdf missing"]
      analyze["analyze_document()<br/>layout metrics"]
      applyProfile["apply_profile()<br/>auto, manual, calibrated"]
      headerFallback["apply_header_image_fallback()"]
      parseDocx --> refPdf --> analyze --> applyProfile --> headerFallback
    end

    subgraph phase4["Phase 4 - Render and Response"]
      direction LR
      assignPolicy["decide_policy() per block"]
      render["dynamic_generate() or generate_latex()"]
      package["zip_directory(output)<br/>to wordtolatex_output.zip"]
      response["ZIP or JSON response"]
      assignPolicy --> render --> package --> response
    end

    limiter --> convertSvc
    convertSvc --> parseDocx
    headerFallback --> assignPolicy

    linkStyle 0 stroke:#F59E0B,stroke-width:3px;
    linkStyle 1 stroke:#D97706,stroke-width:3px;
    linkStyle 2 stroke:#2563EB,stroke-width:3px;
    linkStyle 3 stroke:#16A34A,stroke-width:3px;
    linkStyle 4 stroke:#16A34A,stroke-width:3px;
    linkStyle 5 stroke:#16A34A,stroke-width:3px;
    linkStyle 6 stroke:#16A34A,stroke-width:3px;
    linkStyle 7 stroke:#16A34A,stroke-width:3px;
    linkStyle 8 stroke:#16A34A,stroke-width:3px;
    linkStyle 9 stroke:#16A34A,stroke-width:3px;
    linkStyle 10 stroke:#16A34A,stroke-width:3px;
    linkStyle 11 stroke:#4F46E5,stroke-width:3px;

    classDef ingress fill:#FFEDD5,stroke:#EA580C,color:#7C2D12,stroke-width:2px;
    classDef guard fill:#FDE68A,stroke:#CA8A04,color:#713F12,stroke-width:2px;
    classDef orchestrator fill:#DBEAFE,stroke:#2563EB,color:#1E3A8A,stroke-width:2.5px;
    classDef pipeline fill:#DCFCE7,stroke:#16A34A,color:#14532D,stroke-width:2px;
    classDef egress fill:#E0E7FF,stroke:#4F46E5,color:#312E81,stroke-width:2px;

    class request ingress;
    class options,limiter guard;
    class convertSvc orchestrator;
    class parseDocx,refPdf,analyze,applyProfile,headerFallback,assignPolicy,render,package pipeline;
    class response egress;
```

## Runtime Request Lifecycle (System Design View)

```mermaid
sequenceDiagram
    autonumber
    participant U as User / UI
    participant A as FastAPI Router
    participant S as Conversion Service
    participant C as Core Engine
    participant X as External Tools

    rect rgb(255, 244, 229)
      U->>A: POST /v1/convert (docx, options, ref_pdf?)
      A->>A: Validate upload + rate limit
    end

    rect rgb(232, 244, 255)
      A->>S: convert_document(...)
      S->>C: parse_docx(input.docx)
      alt ref_pdf missing and AUTO_REFERENCE_PDF=true
        S->>X: Run soffice to generate ref.pdf
      end
      S->>C: analyze_document(ref.pdf)
      S->>C: apply_profile + header_fallback + policy
      S->>C: generate_latex(dynamic or template)
      S->>S: write output + zip_directory
    end

    rect rgb(235, 255, 240)
      S-->>A: zip_path + metadata
      A-->>U: 200 OK (application/zip)
    end
```

## Notes

- The current implementation keeps rate limiting and stats in process memory.
- `options_json` is intentionally ignored unless `ALLOW_USER_OPTIONS=true`.
- External integrations are optional and controlled by settings.
