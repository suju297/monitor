# Greenhouse Application Assistant MVP Plan

Status: Draft for review

Last updated: 2026-03-07

## Purpose

Define the implementation plan for a Greenhouse-only application assistant that:

- detects Greenhouse application pages
- autofills safe structured fields from a candidate profile
- queues open-ended or high-risk questions for user review
- autonomously submits applications only when no user attention is required

This document is planning-only. No implementation work is included in this phase.

## Goals

- Support Greenhouse-hosted job application flows first.
- Autofill low-risk fields reliably.
- Surface open-ended and policy-sensitive questions in a review queue.
- Autonomously submit applications that pass all safety gates without requiring user attention.
- Keep the user in control whenever the application contains unresolved, sensitive, or blocked fields.
- Reuse existing repo knowledge around Greenhouse board parsing and Playwright where possible.

## Non-Goals For MVP

- Support for ATS platforms other than Greenhouse.
- Fully automatic question answering.
- Broad browser extension packaging on day one.
- Candidate account management beyond the minimum needed for supervised autofill.

## Planning Constraints

- Submission may be autonomous only when the application is explicitly classified as submit-safe.
- The SLM may advise on ambiguity, but autonomous submission must not depend solely on raw SLM output.
- The system should prefer official Greenhouse job-board metadata over brittle DOM-only parsing.
- The browser layer must tolerate hosted Greenhouse UI variants such as custom combobox widgets and file uploads.
- The first version should avoid deep coupling to employer-specific internal APIs.

## Current Repo Reuse

Relevant existing assets in this repo:

- `career_monitor/adapters.py`
  - already derives Greenhouse board tokens and consumes `boards-api.greenhouse.io`
- `scripts/fetch_my_greenhouse_jobs.py`
  - already parses Greenhouse job URLs and enriches job detail data
- `scripts/benchmark_job_slm.py`
  - existing local harness for comparing small models on structured tasks
- `go/internal/monitor/ollama.go`
  - already contains model-specific Ollama tuning for structured JSON usage
- existing Playwright usage patterns
  - useful for navigation, waiting, file upload, and robust selector handling

Implication:

- the repo already knows how to identify Greenhouse boards and job IDs
- the repo already has a workable local SLM path
- the new work is mostly application-schema parsing, field mapping, queueing, supervised execution, and bounded ambiguity handling

## External Research Summary

Observed and verified during planning:

- Greenhouse jobs currently appear on both `boards.greenhouse.io` and `job-boards.greenhouse.io`.
- Public Greenhouse job metadata can be retrieved from the Job Board API using `GET /v1/boards/{board_token}/jobs/{job_id}?questions=true`.
- The API exposes structured application schema fields such as:
  - `questions`
  - `location_questions`
  - `compliance`
  - `demographic_questions`
- Hosted Greenhouse forms often render custom combobox widgets instead of native `<select>` elements.
- Public submission through Greenhouse's employer API is not the correct path for a third-party candidate assistant because the official create-application API requires employer-controlled authentication.
- Hosted flows may include invisible reCAPTCHA, email verification, or other anti-abuse checks, so autonomous submission must stop immediately when such gates appear.

## Product Boundary

The MVP should be framed as:

- a conditional autonomous application assistant
- not an unrestricted job application bot

Core user promise:

- save time on repetitive structured data entry
- submit automatically when the application is fully resolvable from approved safe data
- preserve user control on narrative, compliance, and blocked cases

## Proposed Architecture

### 1. Session Controller

Responsibilities:

- open a job URL in Playwright
- detect the page state
- coordinate parse, fill, queue, review, and final confirmation

Primary outputs:

- current page status
- normalized application context
- submit-safety status
- execution events for logs and debugging

### 2. Greenhouse Detector

Responsibilities:

- determine whether the current page is a Greenhouse application flow
- extract `board_token` and `job_id` when possible

Detection signals:

- host matches `boards.greenhouse.io` or `job-boards.greenhouse.io`
- URL path matches `/{board}/jobs/{job_id}`
- presence of `form#application-form`
- presence of Greenhouse-specific application metadata in page payload

Fallback signals:

- embedded iframe or Greenhouse asset markers on company-hosted pages

### 3. Application Schema Loader

Responsibilities:

- fetch structured field metadata from the Greenhouse Job Board API
- fall back to DOM extraction only when API metadata is unavailable

Primary strategy:

- derive `board_token` and `job_id`
- call `GET /v1/boards/{board_token}/jobs/{job_id}?questions=true`
- normalize all returned fields into a canonical internal schema

Fallback strategy:

- inspect DOM labels, ids, roles, and file inputs
- bind selectors to already-known API field definitions when both are available

### 4. DOM Binder

Responsibilities:

- map canonical schema fields to live page elements
- produce selectors and interaction modes for each field

Why this is separate:

- the API tells us what fields exist
- the DOM tells us how to interact with them on the current page

Interaction modes:

- text input
- textarea
- combobox
- checkbox
- radio group
- file upload

### 5. Candidate Profile Provider

Responsibilities:

- load profile data from a local structured source
- validate required fields before any fill attempt

Initial profile shape:

```json
{
  "first_name": "",
  "last_name": "",
  "email": "",
  "phone": "",
  "location": "",
  "linkedin": "",
  "github": "",
  "website": "",
  "resume_path": "",
  "cover_letter_path": ""
}
```

### 6. Field Mapper

Responsibilities:

- map Greenhouse schema fields to profile fields
- support exact, alias-based, and heuristic label matching

Examples:

- `First Name` -> `first_name`
- `Last Name` -> `last_name`
- `Email` -> `email`
- `Phone` -> `phone`
- `LinkedIn Profile` -> `linkedin`
- `Website or Portfolio` -> `website`
- `Resume/CV` -> `resume_path`

### 7. SLM Assist Layer

Responsibilities:

- classify ambiguous or unfamiliar custom field labels into canonical intents
- suggest cautious actions for otherwise-unmapped fields
- optionally draft short candidate-specific suggestions for queued open-ended prompts
- return strict structured JSON with confidence

Allowed use cases:

- unknown custom question classification
- employer-specific wording normalization
- draft suggestion generation for queue items

Disallowed use cases:

- final autonomous submit authorization by itself
- overriding deterministic `STOP` or `BLOCKED` states
- overriding deterministic `REVIEW` classification for legal, immigration, compensation, relocation, or policy-sensitive fields
- silently answering open-ended questions without a queue or prior approved template

Required behavior:

- if confidence is below threshold, fall back to `REVIEW`
- if the SLM output is malformed, fall back to `REVIEW`
- if a field's final disposition depends only on SLM inference, the application is not `AUTO_SUBMIT_ELIGIBLE`

Initial model recommendation:

- primary advisor model: `qwen2.5:3b`
- rationale:
  - already the repo's current small-model default for structured scoring
  - fastest locally among the tested viable candidates on this machine
  - lower operational complexity than `qwen3` for strict JSON tasks
- secondary candidates for later evaluation:
  - `qwen2.5:7b` if higher classification quality is needed
  - `qwen2.5:1.5b` only as a latency-first fallback, not the preferred accuracy point

Suggested SLM output contract:

```json
{
  "field_intent": "linkedin_profile",
  "suggested_action": "AUTOFILL",
  "profile_key": "linkedin",
  "confidence": 0.93,
  "requires_human_attention": false,
  "reason": "short string"
}
```

### 8. Decision Engine

Responsibilities:

- assign one action to every normalized field

MVP actions:

- `AUTOFILL`
- `REVIEW`
- `QUEUE`
- `SKIP_OPTIONAL`
- `STOP`
- `BLOCKED`

Default rules:

- `AUTOFILL`
  - first name
  - last name
  - email
  - phone
  - linkedin
  - website
  - resume upload
- `QUEUE`
  - open-ended narrative prompts
  - freeform motivation questions
  - custom long-answer textareas
- `REVIEW`
  - salary expectation
  - relocation
  - visa sponsorship
  - work authorization
  - start date
  - anything with ambiguous legal or policy impact
- `BLOCKED`
  - captcha or verification state that prevents continued automation
  - required field with no profile value and no safe fallback

Ambiguity policy:

- use deterministic rules first
- consult the SLM only when deterministic mapping cannot confidently classify a field
- convert low-confidence or malformed SLM outcomes into `REVIEW`

Submit eligibility rule:

- classify the full application as `AUTO_SUBMIT_ELIGIBLE` only after field-level decisions are complete and:
  - every required field is either safely autofilled or safely skippable by site rules
  - there are no `QUEUE`, `REVIEW`, `STOP`, or `BLOCKED` outcomes
  - there are no fields whose final action depends only on unconfirmed SLM inference
  - pre-submit validation passes with no visible errors
  - no captcha, email verification, or other challenge is present
  - the job and employer match the current run target

### 9. Review Queue

Responsibilities:

- hold user-facing prompts that need input or confirmation
- preserve suggested answers separately from approved answers

Proposed shape:

```json
{
  "question": "",
  "api_name": "",
  "field_type": "",
  "required": true,
  "decision": "QUEUE",
  "suggested_answer": "",
  "approved_answer": "",
  "status": "pending"
}
```

Statuses:

- `pending`
- `approved`
- `skipped`
- `blocked`

### 10. Executor

Responsibilities:

- fill only fields approved for automation
- stop and surface unresolved fields when user attention is required
- autonomously submit only when the application is marked submit-safe

Important boundary:

- the system may submit only after a positive submit-safety decision
- the system must not guess answers for unresolved or sensitive questions

### 11. Submit Safety Evaluator

Responsibilities:

- make the final autonomous-submission decision for the current application
- verify that the page is in a clean pre-submit state
- reject autonomous submission when any uncertainty remains

Checks:

- all required fields have valid values
- no unresolved queue items exist
- no policy-sensitive review items exist
- no SLM-only unresolved safety decisions exist
- no visible validation errors exist
- no anti-abuse or verification challenge is active
- submit button is present and enabled
- candidate profile and job context still match the intended target

## Canonical Internal Field Model

The parser should normalize Greenhouse API output and DOM bindings into a shared structure:

```json
{
  "label": "Why do you want to work at Speechify?",
  "required": false,
  "group": "questions",
  "fields": [
    {
      "api_name": "question_15388759004",
      "api_type": "textarea",
      "ui_type": "textarea",
      "selector": "#question_15388759004",
      "options": []
    }
  ]
}
```

Notes:

- one logical question can contain multiple physical fields
- `group` should distinguish standard questions, location questions, compliance, and demographic sections
- `selector` is optional until DOM binding succeeds

## Planned Execution Flow

1. Open job URL.
2. Detect whether the page is Greenhouse.
3. Extract `board_token` and `job_id`.
4. Load structured schema from the Greenhouse Job Board API.
5. Bind canonical fields to live DOM elements.
6. Load and validate candidate profile.
7. Map fields to profile values.
8. Use the SLM advisor only for ambiguous or unmapped non-sensitive fields.
9. Run the decision engine.
10. Autofill safe fields.
11. Build the review queue for queued and review-only fields.
12. Run pre-submit validation.
13. If unresolved items exist, present them to the user and pause.
14. Apply approved answers if the user resolves queued or review items.
15. Re-run pre-submit validation and submit-safety evaluation.
16. If the application is `AUTO_SUBMIT_ELIGIBLE`, submit autonomously.
17. If not eligible, stop and wait for user attention.

## Delivery Phases

### Phase 0: Planning Sign-Off

Deliverables:

- agreed scope
- agreed field decision rules
- agreed storage format for candidate profile

Exit criteria:

- this document is approved or revised into an approved version

### Phase 1: Detection And Schema Retrieval

Deliverables:

- Greenhouse detector
- board token and job ID extraction
- schema loader using `?questions=true`
- normalized field model

Exit criteria:

- can load and print canonical schema for a representative hosted Greenhouse job page

### Phase 2: DOM Binding And Safe Autofill

Deliverables:

- DOM binder for text inputs, textarea, file upload, and combobox widgets
- safe structured field autofill

Exit criteria:

- can autofill standard contact fields and resume upload on a live Greenhouse-hosted form

### Phase 3: SLM Assist And Benchmarking

Deliverables:

- field-intent benchmark set for ambiguous Greenhouse questions
- strict JSON prompt contract for the SLM advisor
- initial model selection for local inference
- confidence thresholds and fallback-to-review rules

Exit criteria:

- chosen advisor model returns valid structured output on the benchmark set and all low-confidence cases degrade safely to `REVIEW`

### Phase 4: Review Queue And User Approval

Deliverables:

- review queue builder
- prompt rendering path for unresolved questions
- approved-answer application back into the form

Exit criteria:

- queued questions can be answered outside the page and then written back into the form reliably

### Phase 5: Autonomous Submit Safety

Deliverables:

- submit-safety evaluator
- pre-submit validation pass
- autonomous submit path for safe applications
- hard stop path for blocked or attention-required applications

Exit criteria:

- tool submits only when no user attention is required and all safety checks pass

### Phase 6: Review-Gated Submission Path

Deliverables:

- final summary step
- explicit submit gate
- blocked-state handling for captcha or verification

Exit criteria:

- tool requests user attention whenever autonomous submission is not safe

### Phase 7: Reliability And Fixtures

Deliverables:

- fixtures from representative Greenhouse forms
- regression tests for schema parsing and mapping
- logs for blocked states and unmapped fields

Exit criteria:

- stable results across a small fixture set that includes at least one hosted form with custom questions and one with compliance fields

## Testing Strategy

### Unit Tests

- board token and job ID extraction
- API schema normalization
- label alias mapping
- decision engine classification
- SLM output schema validation
- SLM fallback-to-review behavior
- queue item generation

### Integration Tests

- Playwright fill flow on stable Greenhouse fixtures
- file upload handling
- combobox selection handling
- ambiguous custom-question classification with SLM assist
- autonomous submit eligibility behavior
- explicit pause behavior for attention-required applications

### Model Benchmarking

- compare `qwen2.5:3b` against candidate upgrades on a labeled ambiguity set
- score strict JSON adherence, latency, and conservative safety behavior
- reject models that frequently invent unsupported actions or exceed the interaction latency budget

### Live Smoke Tests

- one or two public Greenhouse-hosted roles
- dry-run mode for attention-required applications
- tightly controlled autonomous submission tests only on safe internal targets or dedicated test postings

## Risks And Mitigations

### Risk: URL detection is incomplete

Mitigation:

- support both major Greenhouse hosts first
- add iframe and asset-based fallback detection for embedded variants

### Risk: DOM selectors change

Mitigation:

- keep API metadata as the source of truth
- make selectors a binding layer, not the primary schema source

### Risk: combobox widgets are harder than native selects

Mitigation:

- use role-based interaction in Playwright
- store interaction mode per field

### Risk: legal or sensitive questions are answered incorrectly

Mitigation:

- classify them as `REVIEW` by default
- block autonomous submission until approved or skipped according to policy

### Risk: SLM overconfidence misclassifies a custom field

Mitigation:

- use the SLM only for ambiguity cases
- require confidence thresholds
- degrade low-confidence classifications to `REVIEW`
- disallow autonomous submit when a sensitive decision depends only on SLM output

### Risk: anti-bot or verification interrupts the flow

Mitigation:

- detect blocked states explicitly
- cancel autonomous submission and surface the reason instead of retrying blindly

### Risk: autonomous submission hits the wrong job or duplicate job

Mitigation:

- verify board token, job ID, company, and title before submit
- add duplicate-application checks in later implementation phases

### Risk: autonomous submission fires with hidden validation errors

Mitigation:

- require a pre-submit validation pass
- inspect visible error regions before any submit click
- record submit-attempt telemetry for debugging

### Risk: candidate profile is incomplete

Mitigation:

- validate profile before filling
- mark missing required values as `BLOCKED`

## Open Questions

- Where should the candidate profile live in this repo for local development?
- Should the first user interface be CLI-driven, local web UI-driven, or both?
- Do we want to support embedded Greenhouse pages in MVP or defer them until after hosted pages are stable?
- Should demographic and EEOC fields default to skip unless the user explicitly opts in?
- Do we want answer suggestions for open-ended questions in MVP, or only queueing with blank answers?
- What confidence threshold should permit SLM-assisted `AUTOFILL` for non-sensitive custom fields?
- Should autonomous submission be enabled by default, or behind a per-run flag until reliability is proven?
- What duplicate-application safeguards do we want in the first version?

## Recommended Next Step

After plan approval:

- implement Phase 1 only
- keep the first milestone focused on Greenhouse detection, schema retrieval, and normalized field modeling
- keep SLM integration behind the deterministic baseline rather than making it a prerequisite for the first code milestone

That keeps the initial code small and validates the hardest architectural choice before browser automation work expands.
