# Career Page Monitor v3 (Go Runtime + Optional SLM Router)

Track company career pages and job-related email in Gmail, then surface both in the local dashboard.

This version runs on a Go core runtime with:
- concurrent crawler workers
- adapter-based fetching (`greenhouse`, `lever`, `icims`/`icms`, `ashby`, `generic`, `template`, `command`, `my_greenhouse`)
- per-company fallback chains
- blocked-target detection (`403`, `429`, challenge pages)
- JSON run reports
- backend API server for the React dashboard
- optional SLM source-plan routing (local Ollama)

`tracker.py` and `dashboard.py` now delegate to the Go runtime.

## Setup
```bash
brew install go uv

# Go runtime
go -C go mod tidy

# React frontend
pnpm --dir web install

# Python adapters used by command/my_greenhouse flows
uv sync --extra playwright
uv run playwright install chromium

cp companies.example.yaml companies.yaml
cp .env.example .env
```

Recommended local-only files:
- put personal config in `.local/companies.yaml`
- put resume profiles in `.local/resume_profiles.yaml`
- keep browser storage state and OAuth client JSON under `.local/`
- legacy root-level paths still work during migration, but `.local/` is preferred

## Configure
1. Edit `.local/companies.yaml` (or `companies.yaml` during migration).
2. Edit `.env` with SMTP settings.
3. Optional: add mailbox OAuth credentials if you want inbox monitoring.

For Gmail, use an App Password for `SMTP_PASS`.

For mailbox monitoring:
- create a Google OAuth desktop client; either set `GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET` or point `GOOGLE_OAUTH_CLIENT_JSON` at the downloaded `.local/client_secret_*.json`
- Gmail uses a local loopback callback for the desktop client flow, so you do not need to add a custom redirect URI in Google Cloud
- mailbox polling and ambiguous-message SLM review are controlled by the `MAIL_*` env vars in `.env.example`

## Run
Baseline (first run, no email):
```bash
go -C go run ./cmd/monitor --baseline
```

Regular run:
```bash
go -C go run ./cmd/monitor --workers 10
```

Send blocked summaries too:
```bash
go -C go run ./cmd/monitor --workers 10 --alert-on-blocked
```

Dry run:
```bash
go -C go run ./cmd/monitor --dry-run --alert-on-blocked
```

Compatibility wrappers (same behavior):
```bash
python3 tracker.py --workers 10 --alert-on-blocked
```

## Dashboard (React)
Start full UI + API (recommended, one command):
```bash
./scripts/run_dashboard_stack.sh start
```

Useful helpers:
```bash
./scripts/run_dashboard_stack.sh status
./scripts/run_dashboard_stack.sh stop
./scripts/run_dashboard_stack.sh restart
```

`start` always does a managed cleanup pass first, so an old owned backend/observer/frontend stack is stopped before a fresh launch begins.

VS Code tasks:
- `Career Stack: Start`
- `Career Stack: Stop`
- `Career Stack: Restart`
- `Career Stack: Status`

The wrapper uses the same defaults as the managed launcher:
- host `127.0.0.1`
- backend port `8765`
- observer port `8776`
- frontend port `5173`
- config `.local/companies.yaml` with fallback to `companies.yaml`
- state/report/schedule files under `.state/`
- dotenv `.env`
- `--alert-on-blocked`
- observer profile `.local/greenhouse_observer_profile.json` when present

VS Code:
- use `Career App (Full Stack)` for the managed clean-start flow
- use `Career App (Split Debug)` when you want separate visible backend and frontend terminals
- the managed flow runs `dashboard.py --fresh`, so stopping that launch stops the owned backend/observer/frontend services and the next start begins cleanly

API-only mode (no frontend dev server):
```bash
python3 dashboard.py --api-only --host 127.0.0.1 --port 8765 --config companies.yaml --alert-on-blocked
```

Manual split start (advanced):
Start backend API server:
```bash
go -C go run ./cmd/dashboard --host 127.0.0.1 --port 8765 --config companies.yaml --alert-on-blocked
```

Then run React UI:
```bash
pnpm --dir web dev
```

Open:
- `http://127.0.0.1:5173/monitor`
- `http://127.0.0.1:5173/jobs`
- `http://127.0.0.1:5173/mail`
- `http://127.0.0.1:5173/assistant`

Observer API health:
- `http://127.0.0.1:8776/api/health`

By default Vite proxies `/api/*` to `http://127.0.0.1:8765`.
If needed, set `VITE_API_BASE_URL` in `web/.env`.
If needed, set `FRONTEND_BASE_URL` (default `http://127.0.0.1:5173`) so backend redirects from `/`, `/monitor`, and `/jobs` land on your React app.
The React app now lazy-loads route pages from `web/src/pages` and keeps heavier implementations under `web/src/features`.

API notes:
- default API view shows active jobs only; add `?include_inactive=1` to inspect inactive/stale historical entries
- add `?include_noise=1` to bypass the dashboard's usual quality/relevance filter for persisted rows; raw crawler noise dropped before persistence is not retained
- recurring crawl scheduling is available via `/api/crawl-schedule` (used by the Monitor UI) and persisted in `.state/crawl_schedule.json` by default
- recurring mail scheduling is available via `/api/mail-schedule` and persisted in `.state/mail_schedule.json` by default
- crawler output is quality-filtered before persistence (noise links are dropped, and `max_links` is enforced across all sources)
- `/api/jobs` now supports `sort=best_match` (default) and returns per-job `match_score` (0-100) plus `match_reasons`
- `/api/jobs` also returns `work_auth_status` (`blocked|friendly|unknown`) + `work_auth_notes` based on sponsorship/citizenship/clearance text signals
- `/api/jobs` supports `everify=enrolled|unknown|not_found|not_enrolled` and returns `everify_status`, `everify_source`, `everify_checked_at`, and `everify_note`
- `/api/jobs` supports `posted_within=24h|48h|7d` for recency filtering (strictly uses parsed `posted_at`)
- `/api/jobs` summary now includes posted-date quality counters: `posted_dated_jobs` and `missing_posted_date`
- optional SLM ranking: set `SLM_SCORING=true` to score ambiguous jobs with a local quantized model (`qwen2.5:3b` default); deterministic filters still own explicit work-auth, internship, and role-scope rules, and full shortlist reranking is opt-in
- if `resume_profiles.yaml` is present, ranking becomes resume-aware across multiple resume variants and `/api/jobs` returns `recommended_resume`
- optional: set `F1_OPT_EXCLUDE_BLOCKED=true` to hide jobs flagged as incompatible with F1 OPT work authorization

## Structured Jobs DB (SQLite)
Monitor runs now persist jobs into SQLite for structured storage and retention.

Default DB path:
- `.state/jobs.db` (or `JOBS_DB_PATH`)

Stored tables:
- `jobs` (current canonical job rows: active/inactive + metadata)
- `job_observations` (high-volume run observations, retained short-term)
- `company_runs` (per-company outcome rows, retained short-term)
- `daily_job_stats` (compact long-term datapoints)
- `slm_scores` (persisted SLM scoring cache keyed by normalized job content + model + prompt)

Retention controls:
- `JOBS_DB_OBSERVATION_RETENTION_DAYS` (default `4`)
- `JOBS_DB_RUN_RETENTION_DAYS` (default `4`)
- `JOBS_DB_INACTIVE_RETENTION_DAYS` (default `120`)
- `JOBS_DB_DAILY_STATS_RETENTION_DAYS` (default `365`)
- `JOBS_DB_SLM_SCORE_RETENTION_DAYS` (default `90`)

Notes:
- JSON state files are still written for compatibility.
- Dashboard startup bootstraps SQLite from existing state if DB is empty.
- `/api/jobs` reads from SQLite by default (with automatic JSON-state fallback if DB is unavailable).
- SQLite has no stored procedures; this project uses SQL views/triggers/indexes for offload.
- `v_jobs_company_rollup` view is used for company-level aggregation.
- Observation trigger keeps `jobs.last_seen/active` in sync.
- Delete trigger cleans orphaned observations when old inactive jobs are purged.

## E-Verify Enrichment
The jobs dashboard can attach company-level E-Verify metadata as a separate signal (not a sponsorship guarantee).

Optional env:
- `EVERIFY_RESOLUTION_ENABLED` (default `true`)
- `EVERIFY_REFRESH_DAYS` (default `21`)
- `EVERIFY_UNKNOWN_RETRY_HOURS` (default `72`)
- `EVERIFY_OVERRIDES_FILE` (optional `.json` or `.csv` with `company,status,note,source`)
- `EVERIFY_RESOLVER_CMD` (optional command, receives `EVERIFY_COMPANY` env var and should return JSON with `status`, `source`, `note`)
- `EVERIFY_RESOLVER_TIMEOUT_SECONDS` (default `20`)
- `EVERIFY_CACHE_PATH` (optional; default `.state/everify_cache.json` derived from state path)

## SLM Integration (Source Routing)
SLM routing is implemented in:
- `go/internal/monitor/policy.go`

Resolution order per company:
1. `ORCHESTRATOR_POLICY_CMD` hook (if set)
2. experimental SLM route, only when `ORCHESTRATOR_SLM_EXPERIMENTAL=true` and the company has multiple allowed sources
3. built-in adaptive fallback rotation

### Option A: Policy command hook
Set a command that reads JSON from stdin and returns:
```json
{"plan":["template","generic","playwright"]}
```

Example:
```bash
export ORCHESTRATOR_POLICY_CMD="uv run python scripts/policy_router.py"
```

### Option B: Experimental local Ollama SLM
```bash
export ORCHESTRATOR_SLM_EXPERIMENTAL=true
export ORCHESTRATOR_SLM_PROVIDER=ollama
export ORCHESTRATOR_SLM_URL=http://127.0.0.1:11434/api/chat
export ORCHESTRATOR_SLM_MODEL=phi4-mini:latest
export ORCHESTRATOR_SLM_TIMEOUT_SECONDS=8
```

The model is constrained to company-configured sources only.

Deterministic adaptive routing is the default and recommended path. In the current production config there are no `fallback_sources`, so the orchestrator SLM normally has no meaningful routing decision to make. The crawl-derived routing benchmark also showed the deterministic adaptive baseline outperforming every tested small model, including `llama3.2:3b`.

## SLM Match Scoring (Jobs Board)
Enable local SLM scoring for `/api/jobs` ranking:
```bash
export SLM_SCORING=true
export SLM_SCORING_PROVIDER=ollama
export SLM_SCORING_URL=http://127.0.0.1:11434/api/chat
export SLM_SCORING_MODEL=qwen2.5:3b
export SLM_ROLE_MODEL=ministral-3:3b
export SLM_INTERNSHIP_MODEL=qwen2.5:3b
export SLM_SCORING_COMPARE_MODELS=qwen2.5:3b,ministral-3:3b
export SLM_SCORING_TIMEOUT_SECONDS=25
export SLM_SCORING_MAX_JOBS=12
export SLM_SCORING_WORKERS=1
export SLM_SCORING_ONLY_BEST_MATCH=true
export SLM_SCORING_RERANK_TOP_MATCHES=false
export SLM_SCORING_PRECOMPUTE_ON_RUN=true
export SLM_SCORING_PERSIST_CACHE=true
# export SLM_SCORING_DEBUG=true
```

Notes:
- Recommended model is quantized `qwen2.5:3b` (Ollama GGUF quantization), not a distill variant.
- The jobs dashboard can override the scorer model per request via the `SLM` filter or `?slm_model=...`; cache keys stay model-specific, so `qwen2.5:3b` and `ministral-3:3b` do not reuse each other's scores.
- `SLM_SCORING_COMPARE_MODELS` controls the jobs-page model picker. If unset, the current default model plus `ministral-3:3b` are shown.
- `SLM_ROLE_MODEL` lets the live scorer use a different model for role-fit ambiguity.
- `SLM_INTERNSHIP_MODEL` lets the live scorer use a different model for internship-eligibility ambiguity.
- If `slm_model` is set on the request, that explicit override wins and forces one model for all ambiguity tasks in that request.
- Existing heuristic scoring stays as fallback and becomes multi-resume aware when `resume_profiles.yaml` is configured.
- F1 OPT constraints remain guard-railed by deterministic regex checks (`work_auth_status` / `work_auth_notes`).
- Internship eligibility for already-graduated candidates now uses deterministic student-status blockers plus SLM review for ambiguous internship wording.
- Explicit role ownership now stays deterministic too: senior/staff/manager levels, clear non-target functions, obvious consulting/solutions/sales roles, and non-US exclusions are resolved before any SLM call.
- By default the SLM now runs only for ambiguity cases (`needs_role_slm` / `needs_internship_slm`). Set `SLM_SCORING_RERANK_TOP_MATCHES=true` if you also want model reranking on the deterministic in-scope shortlist.
- The live SLM prompt is now ambiguity-only. The model returns role fit, internship status, and short reasons; it no longer owns work-auth classification or an absolute numeric score.
- `/api/jobs` now includes `decision_source`, `role_decision`, and `internship_decision` so the UI can show whether a row stayed deterministic or was SLM-reviewed.
- `/api/jobs` applies cached SLM scores only; cache misses fall back to heuristic ranking immediately and trigger background precompute instead of blocking the request.
- Scores are precomputed automatically after each successful crawler run (when `SLM_SCORING_PRECOMPUTE_ON_RUN=true`).
- First requests stay fast even on cache misses, but uncached jobs will initially show heuristic ranking until background precompute finishes.
- Ambiguous internships that need student-status review are still sent to the SLM even if they fall below the normal `SLM_SCORING_MAX_JOBS` cutoff.
- Repeated requests are fast due to in-memory cache plus persisted SQLite cache (`slm_scores`), so already-scored job descriptions are reused across restarts.
- Within a single scoring batch, duplicate job-description keys are deduplicated and scored once.

## Mailbox Monitoring
The dashboard now includes a `/mail` route backed by a separate SQLite DB (`.state/mail.db` by default).

Current behavior:
- connects one Gmail account through OAuth
- polls inbox mail on a separate scheduler from job crawling
- stores full normalized message text locally, without binary attachments
- supports both normal forwarded emails and forwarded `.eml` attachments
- extracts invite metadata when a calendar MIME part is present
- classifies messages into recruiter replies, interview scheduling/updates, application acknowledgements, rejections, other job mail, or ignored
- falls back to a local Ollama model only for ambiguous messages
- supports manual triage (`new`, `reviewed`, `important`, `ignored`, `follow_up`) directly in the UI
- retains historical Outlook rows only for corpus/training export; they are excluded from the live dashboard
- this fallback is less stable than official Graph OAuth and may need occasional reconnects after MFA or tenant-policy changes

Important endpoints:
- `GET /api/mail/overview`
- `GET /api/mail/messages`
- `GET /api/mail/messages/{id}`
- `POST /api/mail/messages/{id}/triage`
- `GET /api/mail/accounts`
- `POST /api/mail/accounts/{provider}/connect/start`
- `GET /api/mail/auth/{provider}/callback`
- `POST /api/mail/run`
- `GET /api/mail/run-status`
- `GET/POST /api/mail-schedule`

### SLM Benchmarking
Use the local benchmark harness to compare models or prompt variants against either the built-in synthetic cases or a silver-label sample from `jobs.db`.

Examples:
```bash
# synthetic benchmark
python3 scripts/benchmark_job_slm.py --models qwen2.5:3b,qwen3:4b

# build a real-job case set from jobs.db
python3 scripts/build_job_slm_cases_from_db.py --db .state/jobs.db --out .state/job_slm_cases_from_db.json

# benchmark a model against DB-backed cases
python3 scripts/benchmark_job_slm.py \
  --models qwen2.5:3b \
  --cases-file .state/job_slm_cases_from_db.json

# compare a prompt variant
python3 scripts/benchmark_job_slm.py \
  --models qwen2.5:3b \
  --cases-file .state/job_slm_cases_from_db.json \
  --prompt-file prompts/qwen25_job_scoring_v2.txt

# materialize the hand-reviewed gold set from jobs.db
python3 scripts/materialize_job_slm_gold.py \
  --db .state/jobs.db \
  --labels benchmarks/job_slm_gold_labels_20260307.json \
  --out benchmarks/job_slm_gold_20260307.json

# export the active ambiguity slice that still reaches the SLM
python3 scripts/build_job_slm_ambiguity_candidates.py \
  --db .state/jobs.db \
  --out .state/job_slm_ambiguity_candidates_20260307.json

# materialize the ambiguity-only reviewed set
python3 scripts/materialize_job_slm_gold.py \
  --db .state/jobs.db \
  --labels benchmarks/job_slm_ambiguity_labels_20260307.json \
  --out benchmarks/job_slm_ambiguity_gold_20260307.json
```

Notes:
- `scripts/build_job_slm_cases_from_db.py` uses persisted `feed_relevance_ok` and `work_auth_status` as silver labels; this is useful for prompt/model comparison, but it is not a substitute for a hand-labeled gold set.
- `scripts/build_job_slm_ambiguity_candidates.py` exports the real active ambiguity pool from `jobs.db`, which is the best source for reviewing the cases that still hit the live ambiguity classifier.
- `benchmarks/job_slm_ambiguity_labels_20260307.json` is the hand-reviewed ambiguity slice, and `benchmarks/job_slm_ambiguity_gold_20260307.json` is the materialized benchmark file for that slice.
- `benchmarks/job_slm_gold_labels_20260307.json` is the current merged hand-reviewed label source, and `benchmarks/job_slm_gold_20260307.json` is the current expanded benchmark file generated from `jobs.db`.
- `scripts/benchmark_job_slm.py` truncates fields to roughly the same envelope used by the live scorer so timings are more representative.
- Qwen3 models on Ollama require `think:false` for structured JSON classification. The benchmark harness and live app now apply that automatically.

## Resume-Aware Matching
Create `resume_profiles.yaml` in the workspace root (or point `RESUME_PROFILES_FILE` at another YAML file) to rank jobs against multiple resume variants instead of a single broad software-engineering profile.

Expected YAML shape:
```yaml
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    resume_file: "/absolute/path/to/Sujendra_Jayant_Gharat_AI.tex"
    summary: Applied AI engineer focused on LLM systems and evaluation pipelines.
    focus_keywords: ["llm", "rag", "langchain"]
    role_keywords: ["ai engineer", "machine learning engineer"]
    stack_keywords: ["python", "fastapi", "pytorch"]
```

Behavior:
- the dashboard chooses the best-fitting resume per job and returns it as `recommended_resume`
- if `resume_file` is set, the matcher also reads the actual TeX resume content and uses it as source-of-truth signal for distinctive token matching
- the relevance gate becomes softer and profile-aware, so resume matching drives ranking instead of a single hard-coded discipline filter
- if the candidate has already graduated, internships are blocked on explicit current-student / return-to-school requirements; ambiguous internships can stay eligible for SLM review when `SLM_SCORING=true`
- SLM scoring, when enabled, is prompted with the available resume variants and reranks against the best-fitting one without replacing the heuristic score outright

## Region filtering
By default, the monitor keeps only US-based roles across all sources.

Optional env:
- `US_ONLY_JOBS` (default `true`; set `false` to include global jobs)
- `US_ONLY_REGEX` (optional regex checked against `location`, `title`, and `url`; if it matches, the role is treated as US-based)
- `US_ONLY_JOBS_REGEX` (legacy alias used only when `US_ONLY_REGEX` is unset)

## Relevance filtering
By default, the monitor keeps roles aligned to your target profile after scraping:
- Full Stack / Cloud / AI / ML / Data / Data Analyst
- early-career through Software Engineer I/II/III (senior/staff/principal/manager roles are excluded)
- recent by default: last 7 days when `posted_at` is parseable
- jobs without parseable `posted_at` are allowed by default and rely on other ranking/filter signals

Optional env:
- `RELEVANCE_FILTER` (default `true`; set `false` to keep all roles that pass quality/US filters)
- `RELEVANCE_INCLUDE_REGEX` (optional force-include regex on combined title/team/description/url)
- `RELEVANCE_EXCLUDE_REGEX` (optional force-exclude regex on combined title/team/description/url)
- `POSTED_TODAY_ONLY` (default `false`; when `true`, the fallback recency window becomes today-only unless `MAX_POSTED_AGE_DAYS` overrides it)
- `MAX_POSTED_AGE_DAYS` (default `7`; `0`=today only, `1`=today+yesterday, `-1` disables posted-date recency gate)
- `ALLOW_UNKNOWN_POSTED_AT` (default `true`; when `true`, jobs with missing or unparseable `posted_at` are not rejected by the recency gate)

## Automatic Job URL Verification
Each monitor run now verifies extracted job URLs and writes a TSV artifact (default path derived from report path, e.g. `.state/last_run_report_url_verification.tsv`).

Run report (`.state/last_run_report.json`) includes `url_verification` summary with:
- `total_urls`
- `checked_urls`
- `skipped_urls`
- `ok_count`
- `blocked_count`
- `error_count`
- `artifact_path`

Optional env:
- `VERIFY_JOB_URLS` (default `true`)
- `VERIFY_JOB_URLS_WORKERS` (default `8`)
- `VERIFY_JOB_URLS_TIMEOUT_SECONDS` (default `25`)
- `VERIFY_JOB_URLS_MAX_URLS` (default `500`; set `0` for no limit)
- `VERIFY_JOB_URLS_OUTPUT_PATH` (optional artifact override path)

## Adapter Notes
Supported `source` values:
- `greenhouse`
- `lever`
- `icims` (`icms` alias)
- `ashby`
- `generic`
- `template`
- `command`
- `playwright` (alias of `command`)
- `my_greenhouse`

### `ashby`
Native Ashby board API adapter.

Supported `careers_url` formats:
- `https://api.ashbyhq.com/posting-api/job-board/<board>`
- `https://jobs.ashbyhq.com/<board>`

Optional `command_env` keys:
- `ASHBY_BOARD` (manual board override; usually auto-derived from `careers_url`)
- `ASHBY_QUERY` (optional keyword filter; comma-separated or space-separated OR matching)
- `ASHBY_US_ONLY` (default `true`)
- `ASHBY_MAX_JOBS` (default `800`)
- `ASHBY_TIMEOUT_SECONDS` (optional request timeout override; defaults to company timeout)

### `icims`
Native iCIMS `/api/jobs` adapter.

Supported `careers_url` examples:
- `https://careers.<company>.com/jobs`
- `https://careers.<company>.com/jobs/search?ss=1`
- `https://careers.<company>.com/careers-home/jobs`

Optional `command_env` keys:
- `ICIMS_MAX_JOBS` (default `800`)
- `ICIMS_MAX_PAGES` (default `25`)
- `ICIMS_SORT_BY` (default `relevance`)
- `ICIMS_DESCENDING` (default `false`)
- `ICIMS_INTERNAL` (default `false`)

### `template`
Use this when site filters are UI-only and not encoded in URL.
Configure:
- `template.url`
- `template.method`
- `template.params` / `template.json`
- `template.jobs_path`
- `template.fields`

### `command`
Command can print:
- JSON job list, or
- status object: `{"status":"ok|blocked|error","message":"...","jobs":[...]}`

Optional job fields from command/playwright payload:
- `posted_at`
- `description`

### Generic enrichment
Generic source now does best-effort enrichment from listing + detail pages (description + posted date when exposed in page metadata/time fields).

Optional env:
- `GENERIC_DESCRIPTION_FETCH_LIMIT` (default `8`, set `0` to disable detail page enrichment)

### Playwright enrichment
Command/Playwright adapter can enrich low-quality rows from individual job detail pages.

Optional env:
- `PW_ENRICH_DETAILS` (default `true`)
- `PW_DETAIL_FETCH_LIMIT` (default `24`)
- `PW_DETAIL_TIMEOUT_MS` (default inherits `PW_TIMEOUT_MS`)
- `PW_DETAIL_WAIT_AFTER_LOAD_MS` (default `800`)
- `PW_ONLY_US` (default `false`, heuristic US-only filter from title/location text)
- `PW_LOCATION_INCLUDE_PATTERN` (optional regex matched against extracted location/country)
- `PW_LOCATION_EXCLUDE_PATTERN` (optional regex to drop extracted location/country matches)
- `PW_USER_AGENT` (optional browser UA override; helps bypass headless bot blocks on some boards)
- `PW_ACCEPT_LANGUAGE` (optional value for `Accept-Language`, e.g. `en-US,en;q=0.9`)
- `PW_LOCALE` (optional Playwright locale, e.g. `en-US`)
- `PW_TIMEZONE_ID` (optional Playwright timezone, e.g. `America/New_York`)

### AMD API command adapter
Uses script: `scripts/fetch_amd_api_jobs.py`.

Optional env:
- `AMD_MAX_JOBS` (default `220`)
- `AMD_PAGE_SIZE` (default `100`, max `200`)
- `AMD_MAX_PAGES` (default `25`)
- `AMD_TIMEOUT_SECONDS` (default `30`)
- `AMD_US_ONLY` (default `true`)
- `AMD_QUERY` (optional keyword filter; multi-word query uses broad OR matching)

### `my_greenhouse`
Uses Playwright script: `scripts/fetch_my_greenhouse_jobs.py`.
Required env:
- `GREENHOUSE_EMAIL`
- `GREENHOUSE_PASSWORD`

Optional env:
- `MY_GREENHOUSE_STORAGE_STATE`
- `MY_GREENHOUSE_TIMEOUT_MS`
- `MY_GREENHOUSE_MAX_JOBS`
- `MY_GREENHOUSE_DETAIL_FETCH_LIMIT` (default `MY_GREENHOUSE_MAX_JOBS`; number of result links to enrich via board/detail lookups)
- `MY_GREENHOUSE_LOAD_MORE_PAGES` (default `60`; clicks "See more jobs" repeatedly to load more results)
- `MY_GREENHOUSE_QUERY`
- `MY_GREENHOUSE_LOCATION`
- `MY_GREENHOUSE_FORCE_LOGIN`

## Files
- Go runtime:
  - `go/cmd/monitor/main.go`
  - `go/cmd/dashboard/main.go`
  - `go/internal/monitor/*.go`
- Playwright adapters:
  - `scripts/fetch_jobs_playwright.py`
  - `scripts/fetch_my_greenhouse_jobs.py`
  - `scripts/fetch_amd_api_jobs.py`
- Config:
  - `companies.yaml`
  - `.env`
- Artifacts:
  - `.state/openings_state.json`
  - `.state/last_run_report.json`
  - `.state/last_run_report_url_verification.tsv` (default)

## Cron Example (Hourly)
```bash
0 * * * * cd "/Users/sujendragharat/Library/CloudStorage/GoogleDrive-sgharat298@gmail.com/My Drive/MacExternalCloud/Documents/Monitor" && /usr/bin/env bash -lc 'go -C go run ./cmd/monitor --workers 10 --alert-on-blocked >> tracker.log 2>&1'
```
