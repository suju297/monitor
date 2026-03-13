from __future__ import annotations

import argparse
from html import unescape
from html.parser import HTMLParser
import json
import os
import re
import sys
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

import requests

from ..constants import DEFAULT_TIMEOUT_SECONDS
from ..exceptions import CrawlFetchError
from ..http_client import perform_request, requests_session
from ..profile_knowledge import retrieve_profile_knowledge

GREENHOUSE_JOB_HOSTS = {"boards.greenhouse.io", "job-boards.greenhouse.io"}
GREENHOUSE_EMBED_PATH = ("embed", "job_app")
GREENHOUSE_ROUTE_MARKERS = (
    'id="application-form"',
    "window.__remixContext",
    "Autofill with MyGreenhouse",
    "Apply for this job",
)
REVIEW_PATTERNS = (
    re.compile(r"\b(?:salary|compensation|pay range|expected pay|desired pay|hourly rate)\b", re.I),
    re.compile(r"\b(?:relocat|relocation)\b", re.I),
    re.compile(r"\b(?:visa|sponsor|sponsorship|work authorization|authorized to work)\b", re.I),
    re.compile(r"\b(?:citizenship|citizen|clearance|security clearance)\b", re.I),
    re.compile(r"\b(?:start date|notice period|available to start)\b", re.I),
    re.compile(r"\b(?:privacy policy|candidate privacy|privacy statement|ai policy|consent to processing)\b", re.I),
    re.compile(r"\b(?:opt[\s-]?in|whatsapp)\b", re.I),
)
NO_SPONSORSHIP_PATTERNS = (
    re.compile(
        r"\b(?:unable|cannot|can't|do not|does not|will not|won't|not able to)(?:\s+to)?\s+sponsor\b[\s\S]{0,120}?\b(?:visa|work authorization|employment authorization|opt|stem opt|f[\s-]?1|clearance)\b",
        re.I,
    ),
    re.compile(r"\bno\s+(?:visa|employment)\s+sponsorship\b", re.I),
    re.compile(r"\bvisa sponsorship (?:is )?not available\b", re.I),
    re.compile(r"\b(?:do not|does not|will not|won't|cannot|can't)\s+sponsor\b[^.]{0,80}\bvisa\b", re.I),
)
US_PERSON_REQUIRED_PATTERNS = (
    re.compile(r"\bu\.?\s*s\.?\s*person\b", re.I),
    re.compile(r"\bexport control\b", re.I),
    re.compile(r"\bitar\b", re.I),
    re.compile(r"\bdeemed export\b", re.I),
)
US_CITIZENSHIP_REQUIRED_PATTERNS = (
    re.compile(r"\bu\.?\s*s\.?\s+citizen(?:ship)?\b", re.I),
    re.compile(r"\bunited states citizens?\b", re.I),
    re.compile(r"\bmust be a u\.?\s*s\.?\s+citizen\b", re.I),
    re.compile(r"\bcitizenship is a basic security clearance eligibility requirement\b", re.I),
    re.compile(
        r"\beligibility for access to classified information may only be granted to employees who are united states citizens\b",
        re.I,
    ),
)
CLEARANCE_REQUIRED_PATTERNS = (
    re.compile(r"\bsecurity clearance\b", re.I),
    re.compile(r"\bactive\s+(?:ts\/sci|secret|top secret)\b", re.I),
    re.compile(r"\bts\/sci\b", re.I),
    re.compile(r"\bpolygraph clearance\b", re.I),
    re.compile(r"\bwith poly(?:graph)?\b", re.I),
)
NONIMMIGRANT_WORK_STATUS_PATTERN = re.compile(
    r"\b(?:f[\s-]?1|opt|stem opt|stem-opt|h[\s-]?1b?|j[\s-]?1|l[\s-]?1|tn|e[\s-]?3|o[\s-]?1)\b",
    re.I,
)
PERMANENT_STATUS_PATTERN = re.compile(r"\b(?:green card|permanent resident|permanent residency)\b", re.I)
REVIEW_IF_REQUIRED_PATTERNS = (
    re.compile(r"\bhow did you hear about\b", re.I),
    re.compile(r"\blegal (?:first )?name\b", re.I),
    re.compile(r"\bcurrent or previous employer\b", re.I),
    re.compile(r"\bcurrent or previous job title\b", re.I),
    re.compile(r"\bmost recent degree\b", re.I),
    re.compile(r"\bmost recent school\b", re.I),
    re.compile(r"\bschool you attended\b", re.I),
    re.compile(r"\bcountry of residence\b", re.I),
    re.compile(r"\bcurrently reside\b", re.I),
    re.compile(r"\banticipate working in\b", re.I),
    re.compile(r"\bhave you ever been employed\b", re.I),
    re.compile(r"\bdeadline\b", re.I),
    re.compile(r"\btimeline consideration\b", re.I),
)
QUEUE_PATTERNS = (
    re.compile(r"^\s*why\b", re.I),
    re.compile(r"\bwhy do you want\b", re.I),
    re.compile(r"\bwhy are you interested\b", re.I),
    re.compile(r"\btell us about\b", re.I),
    re.compile(r"\bdescribe\b", re.I),
    re.compile(r"\badditional information\b", re.I),
    re.compile(r"\banything else\b", re.I),
    re.compile(r"\bmotivation\b", re.I),
)
AUTOFILL_LABEL_TO_PROFILE_KEYS = {
    "first name": ("form_first_name", "first_name"),
    "preferred first name": ("form_preferred_first_name", "preferred_first_name", "first_name"),
    "preferred name": ("form_preferred_first_name", "preferred_name", "preferred_first_name", "first_name"),
    "legal first name": ("legal_first_name", "form_first_name", "first_name"),
    "legal last name": ("legal_last_name", "form_last_name", "last_name"),
    "legal name": ("legal_name",),
    "legal name if different than above": ("legal_name",),
    "last name": ("form_last_name", "last_name"),
    "full name": ("form_full_name", "full_name"),
    "email": ("email",),
    "email address": ("email",),
    "phone": ("phone",),
    "phone number": ("phone",),
    "mobile": ("phone",),
    "mobile phone": ("phone",),
    "linkedin": ("linkedin",),
    "linkedin profile": ("linkedin",),
    "would you like to include your linkedin profile personal website or blog": (
        "professional_profile_url",
        "linkedin",
        "website",
    ),
    "github": ("github",),
    "github profile": ("github",),
    "location": ("location",),
    "city": ("location",),
    "website": ("website",),
    "portfolio": ("website",),
    "website or portfolio": ("website",),
    "resume": ("resume_path",),
    "resume cv": ("resume_path",),
    "resume/cv": ("resume_path",),
}
DEMOGRAPHIC_LABELS = {
    "gender",
    "race",
    "ethnicity",
    "disability status",
    "disabilitystatus",
    "veteran status",
    "hispanic ethnicity",
    "are you hispanic latino",
}
RESUME_VARIANT_KEYWORDS = {
    "ai": (
        "ai",
        "artificial intelligence",
        "machine learning",
        "ml",
        "llm",
        "language model",
        "genai",
        "nlp",
        "anthropic",
        "openai",
        "research",
        "applied scientist",
        "ai engineer",
        "prompt",
        "rag",
        "agent",
    ),
    "distributed_systems": (
        "distributed",
        "distributed systems",
        "backend",
        "platform",
        "infrastructure",
        "systems",
        "storage",
        "database",
        "reliability",
        "scalability",
        "performance",
        "networking",
        "service",
        "kafka",
        "data pipeline",
    ),
    "cloud": (
        "cloud",
        "aws",
        "azure",
        "gcp",
        "kubernetes",
        "container",
        "devops",
        "sre",
        "terraform",
        "observability",
        "cloud engineer",
        "site reliability",
        "infrastructure as code",
        "eks",
    ),
}
TECHNICAL_ROLE_KEYWORDS = (
    "engineer",
    "developer",
    "scientist",
    "architect",
    "software",
    "platform",
    "backend",
    "frontend",
    "full stack",
    "infrastructure",
    "cloud",
    "devops",
    "sre",
    "data",
    "machine learning",
    "ml",
    "ai",
)
SPONSORSHIP_OPT_INCLUDED_PATTERN = re.compile(
    r"\b(?:includes?|including|consider(?:ed)?|count(?:s|ed)?)\b[^.]{0,80}\b(?:f[\s-]?1|opt|stem opt|stem-opt)\b",
    re.I,
)
VISA_STATUS_PATTERNS = (
    re.compile(r"\bwhat(?:'s| is)? your current visa\b", re.I),
    re.compile(r"\bvisa status\b", re.I),
    re.compile(r"\bwhat visa are you on\b", re.I),
    re.compile(r"\bimmigration status\b", re.I),
)
CURRENT_COUNTRY_PATTERNS = (
    re.compile(r"\bcurrent country of residence\b", re.I),
    re.compile(r"\bcountry where you currently reside\b", re.I),
)
CURRENT_STATE_PATTERNS = (
    re.compile(r"^\s*state\s*$", re.I),
    re.compile(r"\bcurrent state\b", re.I),
    re.compile(r"\bstate or region\b", re.I),
)
TARGET_COMPANY_EMPLOYMENT_PATTERNS = (
    re.compile(r"\bhave you ever been employed\b", re.I),
    re.compile(r"\bworked?\s+(?:at|for)\b", re.I),
    re.compile(r"\bemployed by\b", re.I),
)
SLM_EXPERIENCE_PATTERNS = (
    re.compile(r"^\s*(?:do|did|are|have|will|would|can|could)\b", re.I),
    re.compile(r"\bexperience\b", re.I),
    re.compile(r"\bprovision(?:ing)?\b", re.I),
    re.compile(r"\boperat(?:e|ing)\b", re.I),
    re.compile(r"\bautoscal(?:e|ing)\b", re.I),
    re.compile(r"\bkubernetes\b", re.I),
    re.compile(r"\bschedul(?:e|ing)\b", re.I),
)
GREENHOUSE_SLM_SYSTEM_PROMPT = (
    "You suggest supervised job application answers. Return strict JSON only with keys "
    '{"should_fill":true|false,"answer":"string","confidence":0.0,"reason":"string"}. '
    "Use only candidate_profile for stable structured facts and retrieved_profile_evidence for "
    "profile experience evidence. Style snippets may influence tone only and must not introduce facts. "
    "If evidence is insufficient, set should_fill to false and leave answer empty. When options are "
    "provided, answer must exactly match one option label. Mention supporting chunk ids in reason when "
    "retrieved_profile_evidence is used."
)
GREENHOUSE_RESUME_SLM_SYSTEM_PROMPT = (
    "You select the best resume variant for a job application. Return strict JSON only with keys "
    '{"variant":"string","confidence":0.0,"reason":"string"}. '
    "Choose exactly one provided variant key. If evidence is insufficient, return an empty variant."
)
USA_COUNTRY_ALIASES = {"us", "usa", "u s a", "u s", "united states", "united states of america"}


def _normalized_text(value: str | None) -> str:
    return re.sub(r"\s+", " ", re.sub(r"[^a-z0-9]+", " ", (value or "").strip().lower())).strip()


def _clean_profile_value(value: Any) -> str:
    if value is None:
        return ""
    return str(value).strip()


def _normalize_answer_value(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "Yes" if value else "No"
    if isinstance(value, (list, tuple, set)):
        parts = [_clean_profile_value(item) for item in value]
        return "\n".join(part for part in parts if part)
    return _clean_profile_value(value)


def _plain_text(value: Any) -> str:
    raw = _clean_profile_value(value)
    if not raw:
        return ""
    text = unescape(raw)
    text = re.sub(r"<[^>]+>", " ", text)
    return re.sub(r"\s+", " ", text).strip()


def _question_blob(label: str, description: str | None) -> str:
    return " ".join(part for part in [_plain_text(label), _plain_text(description)] if part)


def _clean_dom_label_text(label: str) -> str:
    cleaned = " ".join(part.strip() for part in label.split()).strip()
    cleaned = re.sub(r"\s+[0-9a-f]{8,}\s*$", "", cleaned, flags=re.I)
    cleaned = re.sub(r"\(\s*required\s*\)\s*$", "", cleaned, flags=re.I)
    return re.sub(r"\s*[*:]\s*$", "", cleaned).strip()


def _selector_from_name(name: str) -> str | None:
    name = name.strip()
    if not name:
        return None
    selectors: list[str] = []
    selector_names = [name]
    if name.endswith("[]"):
        base_name = name[:-2].strip()
        if base_name:
            selector_names.append(base_name)
    for selector_name in selector_names:
        if re.fullmatch(r"[A-Za-z0-9_:-]+", selector_name):
            selectors.append(f"#{selector_name}")
            if "_" in selector_name:
                selectors.append(f"#{selector_name.replace('_', '-')}")
    escaped_name = name.replace("\\", "\\\\").replace("'", "\\'")
    selectors.append(f"[name='{escaped_name}']")
    if name == "location":
        selectors.extend(
            [
                "#candidate-location",
                "#candidate_location",
                "[name='candidate_location']",
            ]
        )

    unique_selectors: list[str] = []
    seen: set[str] = set()
    for selector in selectors:
        if not selector or selector in seen:
            continue
        seen.add(selector)
        unique_selectors.append(selector)
    return ", ".join(unique_selectors) if unique_selectors else None


def _ui_type_from_api_type(api_type: str) -> str:
    normalized = api_type.strip().lower()
    if normalized == "input_text":
        return "text_input"
    if normalized == "textarea":
        return "textarea"
    if normalized == "input_file":
        return "file_upload"
    if normalized == "multi_value_single_select":
        return "combobox"
    if normalized == "multi_value_multi_select":
        return "checkbox_group"
    if normalized == "input_hidden":
        return "hidden"
    return normalized or "unknown"


def _question_primary_input(question: "GreenhouseQuestion") -> "GreenhouseInput" | None:
    if question.inputs:
        return question.inputs[0]
    return None


@dataclass
class GreenhouseOption:
    label: str
    value: str


@dataclass
class GreenhouseInput:
    api_name: str
    api_type: str
    ui_type: str
    selector: str | None = None
    options: list[GreenhouseOption] = field(default_factory=list)
    allowed_filetypes: list[str] = field(default_factory=list)


@dataclass
class GreenhouseDecision:
    action: str
    reason: str
    profile_key: str | None = None
    source: str = "deterministic"


@dataclass
class GreenhouseQuestion:
    label: str
    group: str
    required: bool
    description: str | None = None
    inputs: list[GreenhouseInput] = field(default_factory=list)
    decision: GreenhouseDecision | None = None


@dataclass
class GreenhouseDetection:
    url: str
    host: str
    is_greenhouse: bool
    is_application: bool
    board_token: str | None = None
    job_id: str | None = None
    signals: list[str] = field(default_factory=list)


@dataclass
class GreenhouseApplicationSchema:
    source: str
    detection: GreenhouseDetection
    company_name: str | None
    title: str | None
    public_url: str
    job_location: str | None
    job_content: str | None = None
    questions: list[GreenhouseQuestion] = field(default_factory=list)
    data_compliance: list[dict[str, Any]] = field(default_factory=list)


@dataclass
class GreenhouseApplicationAnalysis:
    schema: GreenhouseApplicationSchema
    auto_submit_eligible: bool
    decision_counts: dict[str, int]
    attention_required: list[str]
    review_queue: list["GreenhouseReviewQueueItem"] = field(default_factory=list)
    submit_safety: "GreenhouseSubmitSafety" | None = None
    suggested_answers: list["GreenhouseSuggestedAnswer"] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        payload = asdict(self)
        payload["schema"]["questions"] = [asdict(question) for question in self.schema.questions]
        payload["schema"].pop("job_content", None)
        return payload


@dataclass
class GreenhouseFillTarget:
    question_label: str
    api_name: str
    ui_type: str
    selector: str | None
    profile_key: str
    value: str
    value_source: str = "profile"
    question_group: str = "questions"
    options: list[GreenhouseOption] = field(default_factory=list)


@dataclass
class GreenhouseReviewQueueItem:
    question_label: str
    api_name: str | None
    field_type: str
    required: bool
    group: str
    decision: str
    reason: str
    suggested_answer: str | None = None
    suggested_answer_source: str | None = None
    suggested_answer_reason: str | None = None
    suggested_answer_confidence: float | None = None
    draft_source: str | None = None
    retrieved_chunk_ids: list[str] = field(default_factory=list)
    style_snippet_ids: list[str] = field(default_factory=list)
    retrieval_summary: list[str] = field(default_factory=list)
    approved_answer: str | None = None
    status: str = "pending"


@dataclass
class GreenhouseSubmitSafety:
    eligible: bool
    blockers: list[str] = field(default_factory=list)
    requires_browser_validation: bool = True
    job_blockers: list[str] = field(default_factory=list)


@dataclass
class GreenhouseSuggestedAnswer:
    question_label: str
    api_name: str | None
    value: str
    source: str = "slm"
    reason: str | None = None
    confidence: float | None = None
    draft_source: str | None = None
    retrieved_chunk_ids: list[str] = field(default_factory=list)
    style_snippet_ids: list[str] = field(default_factory=list)
    retrieval_summary: list[str] = field(default_factory=list)


@dataclass
class GreenhouseResumeRecommendation:
    variant: str | None
    path: str | None
    source: str
    available_variants: list[dict[str, str]] = field(default_factory=list)
    reason: str | None = None
    confidence: float | None = None


def detect_greenhouse_application(url: str, html: str | None = None) -> GreenhouseDetection:
    parsed = urlparse(url)
    host = parsed.netloc.lower().split(":", 1)[0]
    parts = [part for part in parsed.path.split("/") if part]
    query = parse_qs(parsed.query)
    gh_jid = "".join(query.get("gh_jid", [])).strip()
    gh_src_values = [str(value or "").strip().lower() for value in query.get("gh_src", [])]
    has_greenhouse_query_signal = bool(gh_jid) or any("greenhouse" in value for value in gh_src_values)
    has_custom_hosted_job_path = len(parts) >= 2 and parts[0].lower() == "jobs"
    is_greenhouse = "greenhouse.io" in host or has_greenhouse_query_signal
    signals: list[str] = []
    board_token: str | None = None
    job_id: str | None = None
    is_application = False

    if host in GREENHOUSE_JOB_HOSTS and len(parts) >= 3 and parts[1].lower() == "jobs":
        board_token = parts[0].strip() or None
        job_id = parts[2].strip() or None
        is_application = bool(board_token and job_id)
        signals.append("hosted_job_path")
    elif host in GREENHOUSE_JOB_HOSTS and tuple(part.lower() for part in parts[:2]) == GREENHOUSE_EMBED_PATH:
        is_application = True
        signals.append("embed_job_app_path")
        job_id = gh_jid or None
    elif has_custom_hosted_job_path and has_greenhouse_query_signal:
        is_application = True
        job_id = gh_jid or parts[-1].strip() or None
        signals.append("custom_hosted_job_path")

    if html:
        for marker in GREENHOUSE_ROUTE_MARKERS:
            if marker in html:
                signals.append(f"html:{marker}")
                is_greenhouse = True
        if 'id="application-form"' in html or "application--form" in html:
            is_greenhouse = True
            is_application = True

    return GreenhouseDetection(
        url=url,
        host=host,
        is_greenhouse=is_greenhouse,
        is_application=is_application and is_greenhouse,
        board_token=board_token,
        job_id=job_id,
        signals=sorted(set(signals)),
    )


def _normalized_input(field_payload: dict[str, Any]) -> GreenhouseInput:
    api_name = _clean_profile_value(field_payload.get("name"))
    api_type = _clean_profile_value(field_payload.get("type")) or "unknown"
    values = field_payload.get("values") or []
    options: list[GreenhouseOption] = []
    if isinstance(values, list):
        for value in values:
            if not isinstance(value, dict):
                continue
            label = _clean_profile_value(value.get("label"))
            raw_value = _clean_profile_value(value.get("value"))
            if label or raw_value:
                options.append(GreenhouseOption(label=label, value=raw_value))
    allowed_filetypes: list[str] = []
    raw_filetypes = field_payload.get("allowed_filetypes") or []
    if isinstance(raw_filetypes, list):
        allowed_filetypes = [_clean_profile_value(item) for item in raw_filetypes if _clean_profile_value(item)]
    return GreenhouseInput(
        api_name=api_name,
        api_type=api_type,
        ui_type=_ui_type_from_api_type(api_type),
        selector=_selector_from_name(api_name),
        options=options,
        allowed_filetypes=allowed_filetypes,
    )


def _normalized_question(question_payload: dict[str, Any], group: str) -> GreenhouseQuestion:
    fields_payload = question_payload.get("fields") or []
    inputs: list[GreenhouseInput] = []
    if isinstance(fields_payload, list):
        inputs = [
            _normalized_input(field_payload)
            for field_payload in fields_payload
            if isinstance(field_payload, dict)
        ]
    return GreenhouseQuestion(
        label=_clean_profile_value(question_payload.get("label")) or "Untitled Question",
        group=group,
        required=bool(question_payload.get("required")),
        description=_clean_profile_value(question_payload.get("description")) or None,
        inputs=inputs,
    )


def _synthetic_data_compliance_questions(items: list[dict[str, Any]]) -> list[GreenhouseQuestion]:
    questions: list[GreenhouseQuestion] = []
    for item in items:
        if not isinstance(item, dict):
            continue
        item_type = _clean_profile_value(item.get("type")) or "consent"
        if item.get("requires_processing_consent"):
            questions.append(
                GreenhouseQuestion(
                    label="Consent to processing of personal data",
                    group="data_compliance",
                    required=True,
                    description=f"{item_type.upper()} processing consent is required.",
                    inputs=[
                        GreenhouseInput(
                            api_name=f"data_compliance_processing_consent_{item_type}",
                            api_type="consent",
                            ui_type="checkbox",
                        )
                    ],
                )
            )
        if item.get("requires_retention_consent"):
            questions.append(
                GreenhouseQuestion(
                    label="Consent to retention of personal data",
                    group="data_compliance",
                    required=True,
                    description=f"{item_type.upper()} retention consent is required.",
                    inputs=[
                        GreenhouseInput(
                            api_name=f"data_compliance_retention_consent_{item_type}",
                            api_type="consent",
                            ui_type="checkbox",
                        )
                    ],
                )
            )
    return questions


def _normalize_api_payload(
    payload: dict[str, Any],
    detection: GreenhouseDetection,
) -> GreenhouseApplicationSchema:
    questions: list[GreenhouseQuestion] = []
    for group_name in ("questions", "location_questions"):
        raw_group = payload.get(group_name) or []
        if not isinstance(raw_group, list):
            continue
        questions.extend(
            _normalized_question(question_payload, group_name)
            for question_payload in raw_group
            if isinstance(question_payload, dict)
        )

    compliance = payload.get("compliance") or []
    if isinstance(compliance, list):
        for block in compliance:
            if not isinstance(block, dict):
                continue
            block_questions = block.get("questions") or []
            if not isinstance(block_questions, list):
                continue
            questions.extend(
                _normalized_question(question_payload, "compliance")
                for question_payload in block_questions
                if isinstance(question_payload, dict)
            )

    demographic = payload.get("demographic_questions")
    if isinstance(demographic, list):
        questions.extend(
            _normalized_question(question_payload, "demographic_questions")
            for question_payload in demographic
            if isinstance(question_payload, dict)
        )
    elif isinstance(demographic, dict):
        for raw_group in demographic.values():
            if not isinstance(raw_group, list):
                continue
            questions.extend(
                _normalized_question(question_payload, "demographic_questions")
                for question_payload in raw_group
                if isinstance(question_payload, dict)
            )

    raw_data_compliance = payload.get("data_compliance") or []
    if isinstance(raw_data_compliance, list):
        questions.extend(_synthetic_data_compliance_questions(raw_data_compliance))
    else:
        raw_data_compliance = []

    public_url = _clean_profile_value(payload.get("absolute_url")) or detection.url
    return GreenhouseApplicationSchema(
        source="job_board_api",
        detection=detection,
        company_name=_clean_profile_value(payload.get("company_name")) or None,
        title=_clean_profile_value(payload.get("title")) or None,
        public_url=public_url,
        job_location=_clean_profile_value((payload.get("location") or {}).get("name")) or None,
        job_content=_clean_profile_value(payload.get("content")) or None,
        questions=questions,
        data_compliance=[item for item in raw_data_compliance if isinstance(item, dict)],
    )


def _extract_remix_context(html: str) -> dict[str, Any] | None:
    match = re.search(r"window\.__remixContext\s*=\s*(\{.*?\})\s*;\s*</script>", html, re.S)
    if not match:
        return None
    try:
        return json.loads(match.group(1))
    except json.JSONDecodeError:
        return None


def _normalize_html_payload(
    html: str,
    url: str,
    detection: GreenhouseDetection,
) -> GreenhouseApplicationSchema | None:
    context = _extract_remix_context(html)
    if not context:
        return None
    loader_data = ((context.get("state") or {}).get("loaderData") or {})
    if not isinstance(loader_data, dict):
        return None
    route_payload: dict[str, Any] | None = None
    for value in loader_data.values():
        if isinstance(value, dict) and isinstance(value.get("jobPost"), dict):
            route_payload = value
            break
    if route_payload is None:
        return None

    job_post = route_payload.get("jobPost") or {}
    board_token = _clean_profile_value(route_payload.get("urlToken")) or detection.board_token
    job_id = _clean_profile_value(route_payload.get("jobPostId")) or detection.job_id
    merged_detection = GreenhouseDetection(
        url=url,
        host=detection.host,
        is_greenhouse=detection.is_greenhouse,
        is_application=True,
        board_token=board_token or None,
        job_id=job_id or None,
        signals=sorted(set(detection.signals + ["html_remix_context"])),
    )

    questions = [
        _normalized_question(question_payload, "questions")
        for question_payload in (job_post.get("questions") or [])
        if isinstance(question_payload, dict)
    ]

    for section in job_post.get("eeoc_sections") or []:
        if not isinstance(section, dict):
            continue
        for question_payload in section.get("questions") or []:
            if not isinstance(question_payload, dict):
                continue
            questions.append(_normalized_question(question_payload, "compliance"))

    return GreenhouseApplicationSchema(
        source="html_remix_context",
        detection=merged_detection,
        company_name=_clean_profile_value(job_post.get("company_name")) or None,
        title=_clean_profile_value(job_post.get("title")) or None,
        public_url=_clean_profile_value(job_post.get("public_url")) or url,
        job_location=_clean_profile_value(job_post.get("job_post_location")) or None,
        job_content=_clean_profile_value(job_post.get("content")) or None,
        questions=questions,
    )


class _GreenhouseRenderedFieldParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self._form_depth = 0
        self._current_label_for: str | None = None
        self._current_label_id: str | None = None
        self._current_label_parts: list[str] = []
        self._labels_by_for: dict[str, str] = {}
        self._labels_by_id: dict[str, str] = {}
        self._current_select: dict[str, Any] | None = None
        self._current_option_value: str | None = None
        self._current_option_parts: list[str] = []
        self._fields: list[dict[str, Any]] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        attrs_map = {key: (value or "") for key, value in attrs}
        if tag == "form":
            form_id = attrs_map.get("id", "")
            form_class = attrs_map.get("class", "")
            if (
                form_id == "application-form"
                or "application--form" in form_class
                or "form-template" in form_class
            ):
                self._form_depth += 1
            return

        if self._form_depth <= 0:
            return

        if tag == "label":
            self._current_label_for = attrs_map.get("for") or None
            self._current_label_id = attrs_map.get("id") or None
            self._current_label_parts = []
            return

        if tag == "select":
            self._current_select = {
                "name": attrs_map.get("name") or "",
                "id": attrs_map.get("id") or "",
                "required": bool(
                    attrs_map.get("required")
                    or _profile_bool(attrs_map.get("aria-required"))
                ),
                "options": [],
            }
            return

        if tag == "option" and self._current_select is not None:
            self._current_option_value = attrs_map.get("value", "").strip()
            self._current_option_parts = []
            label = attrs_map.get("label", "").strip()
            if label:
                self._current_option_parts.append(label)
            return

        if tag not in {"input", "textarea"}:
            return

        field = self._field_from_attrs(tag, attrs_map)
        if field is not None:
            self._fields.append(field)

    def handle_endtag(self, tag: str) -> None:
        if tag == "form" and self._form_depth > 0:
            self._form_depth -= 1
            return

        if self._form_depth <= 0:
            return

        if tag == "label":
            label = _clean_dom_label_text(
                " ".join(part.strip() for part in self._current_label_parts if part.strip()).strip()
            )
            if label:
                if self._current_label_for:
                    self._labels_by_for[self._current_label_for] = label
                if self._current_label_id:
                    self._labels_by_id[self._current_label_id] = label
            self._current_label_for = None
            self._current_label_id = None
            self._current_label_parts = []
            return

        if tag == "option" and self._current_select is not None:
            label = " ".join(part.strip() for part in self._current_option_parts if part.strip()).strip()
            value = (self._current_option_value or "").strip()
            if label or value:
                self._current_select["options"].append(
                    GreenhouseOption(label=label or value, value=value or label)
                )
            self._current_option_value = None
            self._current_option_parts = []
            return

        if tag == "select" and self._current_select is not None:
            select_field = self._field_from_select(self._current_select)
            if select_field is not None:
                self._fields.append(select_field)
            self._current_select = None

    def handle_data(self, data: str) -> None:
        if self._current_label_parts is not None and (
            self._current_label_for is not None or self._current_label_id is not None
        ):
            self._current_label_parts.append(data)
        if self._current_option_value is not None:
            self._current_option_parts.append(data)

    def questions(self) -> list[GreenhouseQuestion]:
        questions: list[GreenhouseQuestion] = []
        seen: set[tuple[str, str]] = set()
        for field in self._fields:
            label = str(field.get("label") or "").strip()
            api_name = str(field.get("api_name") or "").strip()
            if not label or not api_name:
                continue
            key = (_normalized_text(label), api_name)
            if key in seen:
                continue
            seen.add(key)
            question_group = "demographic_questions" if _normalized_text(label) in DEMOGRAPHIC_LABELS else "questions"
            questions.append(
                GreenhouseQuestion(
                    label=label,
                    group=question_group,
                    required=bool(field.get("required")),
                    inputs=[
                        GreenhouseInput(
                            api_name=api_name,
                            api_type=str(field.get("api_type") or "input_text"),
                            ui_type=str(field.get("ui_type") or "text_input"),
                            selector=str(field.get("selector") or "") or None,
                            options=list(field.get("options") or []),
                        )
                    ],
                )
            )
        return questions

    def _field_from_select(self, payload: dict[str, Any]) -> dict[str, Any] | None:
        api_name = str(payload.get("name") or payload.get("id") or "").strip()
        if not api_name:
            return None
        label = self._resolve_label(payload.get("id") or "", payload.get("name") or "", "")
        return {
            "label": label,
            "api_name": api_name,
            "required": bool(payload.get("required")),
            "api_type": "multi_value_single_select",
            "ui_type": "combobox",
            "selector": _selector_from_name(api_name),
            "options": list(payload.get("options") or []),
        }

    def _field_from_attrs(self, tag: str, attrs_map: dict[str, str]) -> dict[str, Any] | None:
        input_type = attrs_map.get("type", "").strip().lower()
        if input_type in {"submit", "button", "image"}:
            return None
        role = attrs_map.get("role", "").strip().lower()
        api_name = (attrs_map.get("name") or attrs_map.get("id") or "").strip()
        field_id = (attrs_map.get("id") or "").strip()
        if not api_name:
            return None
        label = self._resolve_label(field_id, api_name, attrs_map.get("aria-labelledby", ""))
        if not label:
            return None
        if role == "combobox":
            api_type = "multi_value_single_select"
            ui_type = "combobox"
        elif input_type == "file":
            api_type = "input_file"
            ui_type = "file_upload"
        elif input_type == "checkbox":
            api_type = "multi_value_multi_select"
            ui_type = "checkbox"
        elif input_type == "radio":
            api_type = "multi_value_single_select"
            ui_type = "radio"
        elif input_type == "hidden":
            api_type = "input_hidden"
            ui_type = "hidden"
        elif tag == "textarea":
            api_type = "textarea"
            ui_type = "textarea"
        else:
            api_type = "input_text"
            ui_type = "text_input"
        return {
            "label": label,
            "api_name": api_name,
            "required": bool(attrs_map.get("required") or _profile_bool(attrs_map.get("aria-required"))),
            "api_type": api_type,
            "ui_type": ui_type,
            "selector": _selector_from_name(api_name) or (f"#{field_id}" if field_id else None),
            "options": [],
        }

    def _resolve_label(self, field_id: str, api_name: str, aria_labelledby: str) -> str | None:
        candidates = [field_id, api_name]
        label_ids = [part.strip() for part in aria_labelledby.split() if part.strip()]
        for label_id in label_ids:
            label = self._labels_by_id.get(label_id)
            if label:
                return label
        for candidate in candidates:
            if not candidate:
                continue
            label = self._labels_by_for.get(candidate)
            if label:
                return label
            label = self._labels_by_id.get(candidate)
            if label:
                return label
        return None


def _extract_rendered_dom_questions(html: str) -> list[GreenhouseQuestion]:
    parser = _GreenhouseRenderedFieldParser()
    parser.feed(html)
    parser.close()
    return parser.questions()


def _merge_supplemental_questions(
    schema: GreenhouseApplicationSchema,
    supplemental_questions: list[GreenhouseQuestion],
) -> GreenhouseApplicationSchema:
    existing_keys: set[tuple[str, tuple[str, ...]]] = set()
    existing_names: set[str] = set()
    for question in schema.questions:
        api_names = tuple(sorted(input_field.api_name for input_field in question.inputs if input_field.api_name))
        existing_keys.add((_normalized_text(question.label), api_names))
        existing_names.update(api_name for api_name in api_names if api_name)

    added = False
    for question in supplemental_questions:
        api_names = tuple(sorted(input_field.api_name for input_field in question.inputs if input_field.api_name))
        if (_normalized_text(question.label), api_names) in existing_keys:
            continue
        if api_names and all(api_name in existing_names for api_name in api_names):
            continue
        schema.questions.append(question)
        existing_keys.add((_normalized_text(question.label), api_names))
        existing_names.update(api_name for api_name in api_names if api_name)
        added = True

    if added and schema.source == "job_board_api":
        schema.source = "job_board_api+html_dom"
    elif added and schema.source == "html_remix_context":
        schema.source = "html_remix_context+html_dom"
    return schema


def _fetch_greenhouse_api_payload(
    session: requests.Session,
    board_token: str,
    job_id: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    api_url = f"https://boards-api.greenhouse.io/v1/boards/{board_token}/jobs/{job_id}?questions=true"
    response = perform_request(
        session=session,
        method="GET",
        url=api_url,
        timeout_seconds=timeout_seconds,
    )
    payload = response.json()
    if not isinstance(payload, dict):
        raise CrawlFetchError("Greenhouse API returned a non-object payload.")
    return payload


def _fetch_greenhouse_content_payload(
    session: requests.Session,
    board_token: str,
    job_id: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    api_url = f"https://boards-api.greenhouse.io/v1/boards/{board_token}/jobs/{job_id}?content=true"
    response = perform_request(
        session=session,
        method="GET",
        url=api_url,
        timeout_seconds=timeout_seconds,
    )
    payload = response.json()
    if not isinstance(payload, dict):
        raise CrawlFetchError("Greenhouse content API returned a non-object payload.")
    return payload


def _fetch_html(session: requests.Session, url: str, timeout_seconds: int) -> str:
    response = perform_request(
        session=session,
        method="GET",
        url=url,
        timeout_seconds=timeout_seconds,
        headers={"Accept": "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8"},
    )
    return response.text


def _dismiss_playwright_cookie_consent(page) -> None:
    for locator in (
        page.get_by_role("button", name=re.compile(r"^i accept$", re.IGNORECASE)).first,
        page.get_by_role("button", name=re.compile(r"^accept$", re.IGNORECASE)).first,
        page.get_by_role("button", name=re.compile(r"^close$", re.IGNORECASE)).first,
    ):
        try:
            if locator.count() == 0 or not locator.is_visible(timeout=500):
                continue
            locator.click(timeout=1500)
            page.wait_for_timeout(250)
            return
        except Exception:
            continue


def _playwright_has_visible_application_form(page) -> bool:
    try:
        return bool(
            page.evaluate(
                """() => {
                    const visible = (element) => Boolean(
                        element &&
                        (element.offsetWidth || element.offsetHeight || element.getClientRects().length)
                    );
                    return Array.from(document.querySelectorAll("form")).some((form) => {
                        if (!visible(form)) {
                            return false;
                        }
                        const text = (form.innerText || "").toLowerCase();
                        return text.includes("submit application") || Boolean(form.querySelector("input[type='file']"));
                    });
                }"""
            )
        )
    except Exception:
        return False


def _playwright_expand_application_form(page, timeout_ms: int) -> None:
    if _playwright_has_visible_application_form(page):
        return
    _dismiss_playwright_cookie_consent(page)
    for locator in (
        page.get_by_role("button", name=re.compile(r"^apply$", re.IGNORECASE)).first,
        page.locator("button[aria-label='Apply']").first,
        page.locator("button:has-text('Apply')").first,
        page.get_by_role("link", name=re.compile(r"^apply$", re.IGNORECASE)).first,
    ):
        try:
            if locator.count() == 0 or not locator.is_visible(timeout=500):
                continue
            locator.click(timeout=min(timeout_ms, 3000))
            for _ in range(5):
                page.wait_for_timeout(350)
                if _playwright_has_visible_application_form(page):
                    return
            return
        except Exception:
            continue


def _extract_playwright_schema_payload(page) -> dict[str, Any]:
    payload = page.evaluate(
        """() => {
            const visible = (element) => Boolean(
                element &&
                (element.offsetWidth || element.offsetHeight || element.getClientRects().length)
            );
            const text = (element) => ((element?.innerText || element?.textContent || "").trim());
            const meta = (selector) => {
                const element = document.querySelector(selector);
                return element ? ((element.getAttribute("content") || "").trim()) : "";
            };
            const applicationForm = Array.from(document.querySelectorAll("form")).find((form) => {
                if (!visible(form)) {
                    return false;
                }
                const formText = text(form).toLowerCase();
                return formText.includes("submit application") || Boolean(form.querySelector("input[type='file']"));
            }) || null;
            const jobDescription = document.querySelector(".job-description");
            const jobMeta = document.querySelector(".job-component-details");
            return {
                "document_title": document.title || "",
                "og_title": meta("meta[property='og:title']"),
                "company_name": meta("meta[property='og:site_name']"),
                "meta_description": meta("meta[name='description']") || meta("meta[property='og:description']"),
                "page_title": text(document.querySelector(".job-title")) || text(document.querySelector("h1")),
                "job_meta": text(jobMeta),
                "public_url": location.href,
                "job_description_html": jobDescription ? jobDescription.innerHTML : "",
                "application_form_html": applicationForm ? applicationForm.outerHTML : "",
            };
        }"""
    )
    if not isinstance(payload, dict):
        return {}
    return payload


def _normalize_playwright_payload(
    payload: dict[str, Any],
    detection: GreenhouseDetection,
) -> GreenhouseApplicationSchema | None:
    form_html = _clean_profile_value(payload.get("application_form_html"))
    if not form_html:
        return None
    questions = _extract_rendered_dom_questions(form_html)
    if not questions:
        return None

    company_name = _clean_profile_value(payload.get("company_name"))
    if company_name.lower().endswith(" jobs"):
        company_name = company_name[:-5].strip()
    if not company_name:
        document_title = _clean_profile_value(payload.get("document_title"))
        match = re.match(r"(.+?)\s+Jobs\s+-\s+", document_title)
        if match:
            company_name = match.group(1).strip()

    title = _clean_profile_value(payload.get("page_title"))
    og_title = _clean_profile_value(payload.get("og_title"))
    if not title and og_title:
        title = og_title.split(" - ", 1)[0].strip() or og_title

    job_location: str | None = None
    job_meta = _clean_profile_value(payload.get("job_meta"))
    if job_meta:
        parts = [part.strip() for part in job_meta.split("|") if part.strip()]
        for part in parts[1:]:
            if not part.lower().startswith("id:"):
                job_location = part
                break
    if not job_location and title and og_title.startswith(f"{title} - "):
        job_location = og_title[len(title) + 3 :].strip() or None

    browser_url = _clean_profile_value(payload.get("public_url")) or detection.url
    browser_detection = detect_greenhouse_application(browser_url)
    merged_detection = GreenhouseDetection(
        url=browser_url,
        host=browser_detection.host,
        is_greenhouse=True,
        is_application=True,
        board_token=browser_detection.board_token or detection.board_token,
        job_id=browser_detection.job_id or detection.job_id,
        signals=sorted(set(detection.signals + browser_detection.signals + ["playwright_dom"])),
    )

    return GreenhouseApplicationSchema(
        source="playwright_dom",
        detection=merged_detection,
        company_name=company_name or None,
        title=title or None,
        public_url=browser_url,
        job_location=job_location,
        job_content=_clean_profile_value(payload.get("job_description_html"))
        or _clean_profile_value(payload.get("meta_description"))
        or None,
        questions=questions,
    )


def _load_greenhouse_application_schema_with_playwright(
    url: str,
    *,
    detection: GreenhouseDetection,
    timeout_seconds: int,
) -> GreenhouseApplicationSchema | None:
    try:
        from playwright.sync_api import sync_playwright
    except Exception as exc:  # noqa: BLE001
        raise CrawlFetchError(
            "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium"
        ) from exc

    timeout_ms = max(1000, timeout_seconds * 1000)
    with sync_playwright() as pw_manager:
        browser = None
        context = None
        try:
            browser = pw_manager.chromium.launch(headless=True)
            context = browser.new_context()
            page = context.new_page()
            page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 8000))
            except Exception:
                pass
            _dismiss_playwright_cookie_consent(page)
            _playwright_expand_application_form(page, timeout_ms)
            return _normalize_playwright_payload(_extract_playwright_schema_payload(page), detection)
        finally:
            if context is not None:
                context.close()
            if browser is not None:
                browser.close()


def load_greenhouse_application_schema(
    url: str,
    *,
    session: requests.Session | None = None,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
) -> GreenhouseApplicationSchema:
    session = session or requests_session()
    detection = detect_greenhouse_application(url)
    if not detection.is_greenhouse:
        raise CrawlFetchError(f"URL is not a Greenhouse page: {url}")

    fetch_errors: list[str] = []

    if detection.board_token and detection.job_id:
        try:
            payload = _fetch_greenhouse_api_payload(
                session=session,
                board_token=detection.board_token,
                job_id=detection.job_id,
                timeout_seconds=timeout_seconds,
            )
            schema = _normalize_api_payload(payload, detection)
            if not schema.job_content:
                try:
                    content_payload = _fetch_greenhouse_content_payload(
                        session=session,
                        board_token=detection.board_token,
                        job_id=detection.job_id,
                        timeout_seconds=timeout_seconds,
                    )
                except Exception:
                    pass
                else:
                    schema.job_content = _clean_profile_value(content_payload.get("content")) or None
            try:
                html = _fetch_html(session, url, timeout_seconds)
            except Exception:
                return schema
            return _merge_supplemental_questions(schema, _extract_rendered_dom_questions(html))
        except Exception as exc:  # noqa: BLE001
            fetch_errors.append(f"job_board_api: {exc}")

    try:
        html = _fetch_html(session, url, timeout_seconds)
    except Exception as exc:  # noqa: BLE001
        fetch_errors.append(f"html_fetch: {exc}")
    else:
        html_detection = detect_greenhouse_application(url, html=html)
        schema = _normalize_html_payload(html, url, html_detection)
        if schema is not None:
            return _merge_supplemental_questions(schema, _extract_rendered_dom_questions(html))
        fetch_errors.append("html_remix_context: no structured application payload found")

    try:
        schema = _load_greenhouse_application_schema_with_playwright(
            url,
            detection=detection,
            timeout_seconds=timeout_seconds,
        )
    except Exception as exc:  # noqa: BLE001
        fetch_errors.append(f"playwright_dom: {exc}")
    else:
        if schema is not None:
            return schema
        fetch_errors.append("playwright_dom: no browser-rendered application form found")

    detail = "; ".join(fetch_errors) if fetch_errors else "no usable Greenhouse application metadata"
    raise CrawlFetchError(f"Failed to load Greenhouse application schema: {detail}")


def _required_missing(question: GreenhouseQuestion, profile_key: str | None, profile: dict[str, str]) -> bool:
    if not question.required or not profile_key:
        return False
    return not _clean_profile_value(profile.get(profile_key))


def _profile_bool(value: Any) -> bool | None:
    if isinstance(value, bool):
        return value
    if value is None:
        return None
    normalized = _clean_profile_value(value).lower()
    if normalized in {"1", "true", "yes", "y"}:
        return True
    if normalized in {"0", "false", "no", "n"}:
        return False
    return None


def _yes_no_answer(value: Any) -> str | None:
    boolean = _profile_bool(value)
    if boolean is None:
        return None
    return "Yes" if boolean else "No"


def _env_flag(name: str, default: bool) -> bool:
    raw = _clean_profile_value(os.getenv(name))
    if not raw:
        return default
    normalized = raw.lower()
    if normalized in {"1", "true", "yes", "y", "on"}:
        return True
    if normalized in {"0", "false", "no", "n", "off"}:
        return False
    return default


def _normalize_location_tokens(value: Any) -> list[str]:
    raw = _clean_profile_value(value)
    if not raw:
        return []
    return [token for token in re.split(r"[\n,;/|]+", raw) if token.strip()]


def _profile_usa_answer(profile: dict[str, Any]) -> str | None:
    country_candidates: list[str] = []
    country_candidates.extend(_normalize_location_tokens(profile.get("current_country")))
    country_candidates.extend(_normalize_location_tokens(profile.get("location")))
    raw_anticipated = profile.get("anticipated_work_countries")
    if isinstance(raw_anticipated, (list, tuple, set)):
        country_candidates.extend(_clean_profile_value(item) for item in raw_anticipated)
    else:
        country_candidates.extend(_normalize_location_tokens(raw_anticipated))
    for candidate in country_candidates:
        normalized = _normalized_text(candidate)
        if normalized in USA_COUNTRY_ALIASES:
            return "Yes"
    return None


def _profile_is_us_citizen(profile: dict[str, Any]) -> bool | None:
    for key in ("is_us_citizen", "us_citizen", "us_citizenship"):
        boolean = _profile_bool(profile.get(key))
        if boolean is not None:
            return boolean
    visa_status = _clean_profile_value(profile.get("current_visa_status"))
    if not visa_status:
        return None
    if PERMANENT_STATUS_PATTERN.search(visa_status):
        return False
    if NONIMMIGRANT_WORK_STATUS_PATTERN.search(visa_status):
        return False
    normalized = _normalized_text(visa_status)
    if "citizen" in normalized:
        return True
    return None


def _profile_is_us_person(profile: dict[str, Any]) -> bool | None:
    for key in ("is_us_person", "us_person"):
        boolean = _profile_bool(profile.get(key))
        if boolean is not None:
            return boolean
    citizen = _profile_is_us_citizen(profile)
    if citizen is True:
        return True
    for key in ("is_permanent_resident", "permanent_resident"):
        boolean = _profile_bool(profile.get(key))
        if boolean is not None:
            return boolean
    visa_status = _clean_profile_value(profile.get("current_visa_status"))
    if not visa_status:
        return citizen
    if PERMANENT_STATUS_PATTERN.search(visa_status):
        return True
    if NONIMMIGRANT_WORK_STATUS_PATTERN.search(visa_status):
        return False
    return citizen


def _profile_requires_employer_sponsorship(profile: dict[str, Any]) -> bool | None:
    for key in (
        "requires_sponsorship_if_opt_included",
        "requires_visa_sponsorship_future",
        "requires_visa_sponsorship_now",
    ):
        boolean = _profile_bool(profile.get(key))
        if boolean is True:
            return True
    visa_status = _clean_profile_value(profile.get("current_visa_status"))
    if re.search(r"\b(?:f[\s-]?1|opt|stem opt|stem-opt)\b", visa_status, re.I):
        return True
    explicit_default = _profile_bool(profile.get("default_requires_sponsorship_answer"))
    if explicit_default is not None:
        return explicit_default
    return None


def _policy_corpus_text(schema: GreenhouseApplicationSchema) -> str:
    parts = [
        _plain_text(schema.title),
        _plain_text(schema.job_location),
        _plain_text(schema.job_content),
    ]
    for question in schema.questions:
        parts.append(_plain_text(question.label))
        parts.append(_plain_text(question.description))
    return " ".join(part for part in parts if part)


def _evaluate_job_policy_blockers(
    schema: GreenhouseApplicationSchema,
    profile: dict[str, Any],
) -> list[str]:
    corpus_text = _policy_corpus_text(schema)
    if not corpus_text:
        return []
    blockers: list[str] = []
    visa_status = _clean_profile_value(profile.get("current_visa_status"))

    if any(pattern.search(corpus_text) for pattern in CLEARANCE_REQUIRED_PATTERNS + US_CITIZENSHIP_REQUIRED_PATTERNS):
        citizen = _profile_is_us_citizen(profile)
        if citizen is False:
            detail = f" current visa status '{visa_status}'" if visa_status else " the current profile"
            blockers.append(
                "Job description: role requires U.S. citizenship and/or security clearance, "
                f"which conflicts with{detail}."
            )
        elif citizen is None:
            blockers.append(
                "Job description: role requires U.S. citizenship and/or security clearance. Confirm eligibility before applying."
            )

    if any(pattern.search(corpus_text) for pattern in US_PERSON_REQUIRED_PATTERNS):
        us_person = _profile_is_us_person(profile)
        if us_person is False:
            detail = f" current visa status '{visa_status}'" if visa_status else " the current profile"
            blockers.append(
                f"Job description: role appears restricted to U.S. persons, which conflicts with{detail}."
            )
        elif us_person is None:
            blockers.append(
                "Job description: role appears restricted to U.S. persons. Confirm export-control eligibility before applying."
            )

    if any(pattern.search(corpus_text) for pattern in NO_SPONSORSHIP_PATTERNS):
        sponsorship = _profile_requires_employer_sponsorship(profile)
        if sponsorship is True:
            detail = f" current visa status '{visa_status}'" if visa_status else " the current profile"
            blockers.append(
                f"Job description: role states sponsorship is not available, which conflicts with{detail}."
            )
        elif sponsorship is None:
            blockers.append(
                "Job description: role states sponsorship is not available. Confirm your work authorization before applying."
            )

    unique: list[str] = []
    for blocker in blockers:
        if blocker not in unique:
            unique.append(blocker)
    return unique


def _match_option_label(question: GreenhouseQuestion, raw_value: Any) -> str | None:
    cleaned = _clean_profile_value(raw_value)
    if not cleaned:
        return None
    normalized = _normalized_text(cleaned)
    for input_field in question.inputs:
        for option in input_field.options:
            if _normalized_text(option.label) == normalized:
                return option.label
            if _normalized_text(option.value) == normalized:
                return option.label or option.value
    return None


def _decline_option_answer(question: GreenhouseQuestion) -> str | None:
    decline_patterns = (
        "decline to self identify",
        "wish to answer",
        "want to answer",
        "decline",
    )
    for input_field in question.inputs:
        for option in input_field.options:
            label = option.label or option.value
            normalized = _normalized_text(label)
            if any(pattern in normalized for pattern in decline_patterns):
                return label
    return None


def _company_specific_answer(
    question: GreenhouseQuestion,
    profile: dict[str, Any],
    company_name: str | None,
) -> str | None:
    raw_answers = profile.get("company_specific_answers")
    if not isinstance(raw_answers, dict):
        return None
    normalized_company = _normalized_text(company_name)
    candidate_maps: list[dict[str, Any]] = []
    for key, value in raw_answers.items():
        if not isinstance(value, dict):
            continue
        if key == "*" or _normalized_text(key) == normalized_company:
            candidate_maps.append(value)
    if not candidate_maps:
        return None
    candidate_keys = [
        question.label,
        _normalized_text(question.label),
    ]
    candidate_keys.extend(input_field.api_name for input_field in question.inputs if input_field.api_name)
    for mapping in candidate_maps:
        for key in candidate_keys:
            if key not in mapping:
                continue
            answer = mapping.get(key)
            if answer is None:
                continue
            matched = _match_option_label(question, answer)
            if matched:
                return matched
            cleaned = _normalize_answer_value(answer)
            if cleaned:
                return cleaned
    return None


def _human_identity_answer(question: GreenhouseQuestion) -> str | None:
    human_option: str | None = None
    for input_field in question.inputs:
        for option in input_field.options:
            normalized = _normalized_text(option.label or option.value)
            if "human being" in normalized or normalized == "human":
                human_option = option.label or option.value
            if "ai" in normalized or "automated program" in normalized:
                continue
    return human_option


def _extract_json_object(text: str) -> str:
    stripped = text.strip()
    if stripped.startswith("{") and stripped.endswith("}"):
        return stripped
    start = stripped.find("{")
    end = stripped.rfind("}")
    if start >= 0 and end > start:
        return stripped[start : end + 1].strip()
    return ""


def _greenhouse_slm_enabled() -> bool:
    if "GREENHOUSE_SLM_ENABLED" in os.environ:
        return _env_flag("GREENHOUSE_SLM_ENABLED", False)
    provider = _clean_profile_value(os.getenv("GREENHOUSE_SLM_PROVIDER")).lower()
    if provider:
        return provider == "ollama"
    for provider_name in ("ORCHESTRATOR_SLM_PROVIDER", "SLM_SCORING_PROVIDER"):
        provider = _clean_profile_value(os.getenv(provider_name)).lower()
        if provider:
            return provider == "ollama"
    return False


def _greenhouse_slm_model() -> str:
    return (
        _clean_profile_value(os.getenv("GREENHOUSE_SLM_MODEL"))
        or _clean_profile_value(os.getenv("SLM_SCORING_MODEL"))
        or "qwen3:4b"
    )


def _greenhouse_slm_endpoint() -> str:
    return _clean_profile_value(os.getenv("GREENHOUSE_SLM_ENDPOINT")) or "http://127.0.0.1:11434/api/chat"


def _greenhouse_slm_timeout_seconds() -> int:
    raw = _clean_profile_value(os.getenv("GREENHOUSE_SLM_TIMEOUT_SECONDS"))
    try:
        return max(3, int(raw))
    except (TypeError, ValueError):
        model = _greenhouse_slm_model().strip().lower()
        if model.startswith("qwen3"):
            return 15
        return 8


def _greenhouse_slm_question_context(
    schema: GreenhouseApplicationSchema,
    question: GreenhouseQuestion,
) -> dict[str, Any]:
    index = -1
    for position, candidate in enumerate(schema.questions):
        if candidate is question:
            index = position
            break
    previous_questions = []
    next_questions = []
    if index >= 0:
        previous_questions = [item.label for item in schema.questions[max(0, index - 2) : index]]
        next_questions = [item.label for item in schema.questions[index + 1 : index + 3]]
    primary_input = _question_primary_input(question)
    return {
        "label": question.label,
        "normalized_label": _normalized_text(question.label),
        "description": question.description,
        "required": question.required,
        "group": question.group,
        "primary_input": {
            "api_name": primary_input.api_name if primary_input else None,
            "api_type": primary_input.api_type if primary_input else None,
            "ui_type": primary_input.ui_type if primary_input else None,
            "selector": primary_input.selector if primary_input else None,
            "option_labels": [
                option.label or option.value
                for option in (primary_input.options if primary_input else [])
                if _clean_profile_value(option.label or option.value)
            ],
        },
        "all_inputs": [
            {
                "api_name": input_field.api_name,
                "api_type": input_field.api_type,
                "ui_type": input_field.ui_type,
                "selector": input_field.selector,
                "allowed_filetypes": list(input_field.allowed_filetypes),
                "option_labels": [
                    option.label or option.value
                    for option in input_field.options
                    if _clean_profile_value(option.label or option.value)
                ],
            }
            for input_field in question.inputs
        ],
        "neighboring_questions": {
            "previous": previous_questions,
            "next": next_questions,
        },
    }


def resume_variant_options(profile: dict[str, Any] | None) -> list[dict[str, str]]:
    if not isinstance(profile, dict):
        return []
    raw = profile.get("resume_variants")
    if not isinstance(raw, dict):
        return []
    options: list[dict[str, str]] = []
    for key, value in raw.items():
        variant = _clean_profile_value(key)
        path = ""
        if isinstance(value, dict):
            path = _clean_profile_value(value.get("path"))
        else:
            path = _clean_profile_value(value)
        if not variant or not path:
            continue
        options.append(
            {
                "variant": variant,
                "label": variant.replace("_", " ").strip().title(),
                "path": path,
                "file": Path(path).name,
            }
        )
    return options


def _resume_variant_entries(profile: dict[str, Any]) -> list[tuple[str, str]]:
    entries: list[tuple[str, str]] = []
    for option in resume_variant_options(profile):
        entries.append((option["variant"], option["path"]))
    return entries


def _resume_selection_text(schema: GreenhouseApplicationSchema) -> str:
    return " ".join(
        part
        for part in [
            _clean_profile_value(schema.title),
            _clean_profile_value(schema.job_location),
            _plain_text(schema.job_content),
        ]
        if part
    ).lower()


def _default_resume_variant(profile: dict[str, Any], entries: list[tuple[str, str]]) -> tuple[str | None, str | None]:
    default_variant = _normalized_text(_clean_profile_value(profile.get("resume_variant_default")))
    for key, path in entries:
        if _normalized_text(key) == default_variant:
            return path, key
    if entries:
        return entries[0][1], entries[0][0]
    return None, None


def _heuristic_resume_variant_selection(
    profile: dict[str, Any],
    schema: GreenhouseApplicationSchema,
) -> tuple[str | None, str | None]:
    entries = _resume_variant_entries(profile)
    if not entries:
        return None, None
    if len(entries) == 1:
        return entries[0][1], entries[0][0]

    title_text = _clean_profile_value(schema.title).lower()
    body_text = _resume_selection_text(schema)
    if not any(keyword in title_text for keyword in TECHNICAL_ROLE_KEYWORDS):
        return _default_resume_variant(profile, entries)

    best_key = ""
    best_path = ""
    best_score = -1
    for key, path in entries:
        keywords = RESUME_VARIANT_KEYWORDS.get(key, ())
        score = 0
        for keyword in keywords:
            if keyword in title_text:
                score += 4
            elif keyword in body_text:
                score += 1
        if score > best_score:
            best_key = key
            best_path = path
            best_score = score
    if best_path and best_score > 0:
        return best_path, best_key
    return _default_resume_variant(profile, entries)


def _explicit_resume_recommendation(
    profile: dict[str, Any],
) -> GreenhouseResumeRecommendation | None:
    options = resume_variant_options(profile)
    normalized_variant = _normalized_text(_clean_profile_value(profile.get("selected_resume_variant")))
    explicit_path = _clean_profile_value(profile.get("selected_resume_path"))
    selection_source = _clean_profile_value(profile.get("resume_selection_source")) or "manual_override"

    if explicit_path:
        candidate_path = str(Path(explicit_path).expanduser())
        basename = Path(candidate_path).name.strip().lower()
        matched_variant: str | None = None
        matched_path = explicit_path
        for option in options:
            option_path = str(Path(option["path"]).expanduser())
            if option_path == candidate_path:
                matched_variant = option["variant"]
                matched_path = option["path"]
                break
            if Path(option_path).name.strip().lower() == basename and matched_variant is None:
                matched_variant = option["variant"]
                matched_path = option["path"]
        return GreenhouseResumeRecommendation(
            variant=matched_variant,
            path=matched_path,
            source=selection_source,
            available_variants=options,
            reason="Explicit resume path selected for this run.",
            confidence=1.0,
        )

    if normalized_variant:
        for option in options:
            if _normalized_text(option["variant"]) == normalized_variant:
                return GreenhouseResumeRecommendation(
                    variant=option["variant"],
                    path=option["path"],
                    source=selection_source,
                    available_variants=options,
                    reason="Explicit resume variant selected for this run.",
                    confidence=1.0,
                )

    return None


def _call_greenhouse_resume_slm(
    *,
    schema: GreenhouseApplicationSchema,
    profile: dict[str, Any],
    options: list[dict[str, str]],
) -> GreenhouseResumeRecommendation | None:
    if not _greenhouse_slm_enabled() or len(options) < 2:
        return None
    payload = {
        "company_name": schema.company_name,
        "job_title": schema.title,
        "job_location": schema.job_location,
        "job_content_excerpt": _plain_text(schema.job_content)[:5000],
        "available_resume_variants": [
            {
                "variant": option["variant"],
                "label": option["label"],
                "file": option["file"],
            }
            for option in options
        ],
        "candidate_profile": _greenhouse_slm_profile_context(profile),
    }
    body: dict[str, Any] = {
        "model": _greenhouse_slm_model(),
        "stream": False,
        "messages": [
            {"role": "system", "content": GREENHOUSE_RESUME_SLM_SYSTEM_PROMPT},
            {
                "role": "user",
                "content": f"Select the best resume variant for this job payload: {json.dumps(payload, separators=(',', ':'))}",
            },
        ],
    }
    if body["model"].strip().lower().startswith("qwen3"):
        body["think"] = False

    request = urllib.request.Request(
        _greenhouse_slm_endpoint(),
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=_greenhouse_slm_timeout_seconds()) as response:
            raw = response.read()
    except urllib.error.URLError:
        return None
    except Exception:
        return None
    try:
        parsed = json.loads(raw.decode("utf-8"))
    except json.JSONDecodeError:
        return None
    content = str(((parsed.get("message") or {}).get("content") or "")).strip()
    if not content:
        return None
    candidate = _extract_json_object(content)
    if not candidate:
        return None
    try:
        selection = json.loads(candidate)
    except json.JSONDecodeError:
        return None

    selected_variant = _normalized_text(_clean_profile_value(selection.get("variant")))
    if not selected_variant:
        return None

    matched_option = next(
        (option for option in options if _normalized_text(option["variant"]) == selected_variant),
        None,
    )
    if matched_option is None:
        return None

    confidence = selection.get("confidence")
    try:
        confidence_value = float(confidence)
    except (TypeError, ValueError):
        confidence_value = None

    return GreenhouseResumeRecommendation(
        variant=matched_option["variant"],
        path=matched_option["path"],
        source="slm_recommended",
        available_variants=options,
        reason=_clean_profile_value(selection.get("reason")) or None,
        confidence=confidence_value,
    )


def recommend_resume_selection(
    profile: dict[str, Any] | None,
    schema: GreenhouseApplicationSchema,
) -> GreenhouseResumeRecommendation:
    prepared_profile = dict(profile or {})
    options = resume_variant_options(prepared_profile)

    explicit = _explicit_resume_recommendation(prepared_profile)
    if explicit is not None:
        return explicit

    existing_resume_path = _clean_profile_value(prepared_profile.get("resume_path"))
    if not options:
        return GreenhouseResumeRecommendation(
            variant=None,
            path=existing_resume_path or None,
            source="profile_default" if existing_resume_path else "unavailable",
            available_variants=[],
            reason="Single resume path from profile." if existing_resume_path else "No configured resume variants.",
            confidence=1.0 if existing_resume_path else None,
        )

    if len(options) == 1:
        return GreenhouseResumeRecommendation(
            variant=options[0]["variant"],
            path=options[0]["path"],
            source="single_variant",
            available_variants=options,
            reason="Only one resume variant is configured.",
            confidence=1.0,
        )

    slm_recommendation = _call_greenhouse_resume_slm(
        schema=schema,
        profile=prepared_profile,
        options=options,
    )
    if slm_recommendation is not None:
        return slm_recommendation

    heuristic_path, heuristic_variant = _heuristic_resume_variant_selection(prepared_profile, schema)
    if heuristic_path:
        return GreenhouseResumeRecommendation(
            variant=heuristic_variant,
            path=heuristic_path,
            source="heuristic_recommended",
            available_variants=options,
            reason="Keyword-based resume matching fallback.",
            confidence=None,
        )

    default_path, default_variant = _default_resume_variant(prepared_profile, _resume_variant_entries(prepared_profile))
    return GreenhouseResumeRecommendation(
        variant=default_variant,
        path=default_path,
        source="profile_default",
        available_variants=options,
        reason="Default profile resume variant.",
        confidence=1.0 if default_path else None,
    )


def _prepare_profile_for_schema(
    schema: GreenhouseApplicationSchema,
    profile: dict[str, Any] | None = None,
) -> dict[str, Any]:
    prepared: dict[str, Any] = dict(profile or {})
    normalized_labels = {_normalized_text(question.label) for question in schema.questions}
    has_preferred_name_field = bool(
        {"preferred first name", "preferred name"} & normalized_labels
    )
    has_legal_name_field = any("legal name" in label for label in normalized_labels)

    legal_first_name = _clean_profile_value(prepared.get("legal_first_name")) or _clean_profile_value(
        prepared.get("first_name")
    )
    legal_last_name = _clean_profile_value(prepared.get("legal_last_name")) or _clean_profile_value(
        prepared.get("last_name")
    )
    preferred_first_name = (
        _clean_profile_value(prepared.get("preferred_first_name"))
        or _clean_profile_value(prepared.get("preferred_name"))
        or _clean_profile_value(prepared.get("first_name"))
        or legal_first_name
    )
    if has_preferred_name_field:
        form_first_name = legal_first_name or preferred_first_name
    elif has_legal_name_field:
        form_first_name = preferred_first_name or legal_first_name
    else:
        form_first_name = legal_first_name or preferred_first_name

    if form_first_name:
        prepared["form_first_name"] = form_first_name
    if preferred_first_name:
        prepared["form_preferred_first_name"] = preferred_first_name
    if legal_last_name:
        prepared["form_last_name"] = legal_last_name
    if not _clean_profile_value(prepared.get("legal_name")):
        legal_name = " ".join(
            part
            for part in [
                legal_first_name,
                legal_last_name,
            ]
            if part
        ).strip()
        if legal_name:
            prepared["legal_name"] = legal_name
    if not _clean_profile_value(prepared.get("form_full_name")):
        if has_legal_name_field:
            full_name = " ".join(part for part in [preferred_first_name, legal_last_name] if part).strip()
        else:
            full_name = prepared.get("legal_name") or " ".join(
                part for part in [form_first_name, legal_last_name] if part
            ).strip()
        if full_name:
            prepared["form_full_name"] = full_name
    if not _clean_profile_value(prepared.get("professional_profile_url")):
        professional_profile_url = _clean_profile_value(prepared.get("linkedin")) or _clean_profile_value(
            prepared.get("website")
        )
        if professional_profile_url:
            prepared["professional_profile_url"] = professional_profile_url
    resume_recommendation = recommend_resume_selection(prepared, schema)
    if resume_recommendation.path:
        prepared["resume_path"] = resume_recommendation.path
        prepared["selected_resume_path"] = resume_recommendation.path
    if resume_recommendation.variant:
        prepared["selected_resume_variant"] = resume_recommendation.variant
    if resume_recommendation.source:
        prepared["resume_selection_source"] = resume_recommendation.source
    if resume_recommendation.reason and not _clean_profile_value(prepared.get("resume_selection_reason")):
        prepared["resume_selection_reason"] = resume_recommendation.reason
    if (
        resume_recommendation.confidence is not None
        and not _clean_profile_value(prepared.get("resume_selection_confidence"))
    ):
        prepared["resume_selection_confidence"] = resume_recommendation.confidence
    return prepared


def _resolve_profile_key(question: GreenhouseQuestion, profile: dict[str, str]) -> str | None:
    normalized_label = _normalized_text(question.label)
    candidates = AUTOFILL_LABEL_TO_PROFILE_KEYS.get(normalized_label)
    if not candidates:
        return None
    for key in candidates:
        if _clean_profile_value(profile.get(key)):
            return key
    return candidates[0]


def _label_or_description_matches(question: GreenhouseQuestion, patterns: tuple[re.Pattern[str], ...]) -> bool:
    blob = _question_blob(question.label, question.description)
    return any(pattern.search(blob) for pattern in patterns)


def _is_demographic(question: GreenhouseQuestion) -> bool:
    if question.group in {"compliance", "demographic_questions"}:
        return True
    return _normalized_text(question.label) in DEMOGRAPHIC_LABELS


def _is_open_ended(question: GreenhouseQuestion) -> bool:
    if _label_or_description_matches(question, QUEUE_PATTERNS):
        return True
    return any(input_field.api_type == "textarea" for input_field in question.inputs)


def _has_only_hidden_inputs(question: GreenhouseQuestion) -> bool:
    if not question.inputs:
        return False
    return all(input_field.ui_type == "hidden" for input_field in question.inputs)


def _is_target_company_employment_question(
    question: GreenhouseQuestion,
    company_name: str | None,
) -> bool:
    normalized_company = _normalized_text(company_name)
    if not normalized_company:
        return False
    blob = _question_blob(question.label, question.description)
    normalized_blob = _normalized_text(blob)
    if normalized_company not in normalized_blob:
        return False
    return any(pattern.search(blob) for pattern in TARGET_COMPANY_EMPLOYMENT_PATTERNS)


def _policy_answer_for_question(
    question: GreenhouseQuestion,
    profile: dict[str, Any],
    company_name: str | None = None,
) -> str | None:
    blob = _question_blob(question.label, question.description)
    normalized_blob = _normalized_text(blob)

    if _is_target_company_employment_question(question, company_name):
        return None

    company_specific_answer = _company_specific_answer(question, profile, company_name)
    if company_specific_answer:
        return company_specific_answer

    for pattern in VISA_STATUS_PATTERNS:
        if pattern.search(blob):
            return _clean_profile_value(profile.get("current_visa_status")) or None

    if _label_or_description_matches(question, CURRENT_COUNTRY_PATTERNS):
        return _clean_profile_value(profile.get("current_country")) or None

    if _label_or_description_matches(question, CURRENT_STATE_PATTERNS):
        return _clean_profile_value(profile.get("current_state_or_region")) or None

    if "earliest you would want to start" in normalized_blob or "available to start" in normalized_blob:
        return _clean_profile_value(profile.get("default_earliest_start_answer")) or _clean_profile_value(
            profile.get("earliest_start_answer")
        ) or None

    if "deadline" in normalized_blob or "timeline considerations" in normalized_blob:
        return _clean_profile_value(profile.get("default_timeline_considerations_answer")) or _clean_profile_value(
            profile.get("timeline_considerations_answer")
        ) or None

    if "current or previous employer" in normalized_blob:
        return _clean_profile_value(profile.get("current_employer")) or None

    if "current or previous job title" in normalized_blob:
        return _clean_profile_value(profile.get("current_or_previous_job_title")) or None

    if "most recent degree" in normalized_blob:
        return _clean_profile_value(profile.get("most_recent_degree")) or None

    if "most recent school" in normalized_blob or "school you attended" in normalized_blob:
        return _clean_profile_value(profile.get("most_recent_school")) or None

    if "located in and plan to work from the usa" in normalized_blob:
        return _profile_usa_answer(profile)

    if "eligible to work in your country of residence" in normalized_blob:
        answer = _clean_profile_value(profile.get("default_authorized_to_work_answer")) or None
        return answer or _yes_no_answer(profile.get("authorized_to_work_in_us"))

    if "authorized to work" in normalized_blob:
        answer = _clean_profile_value(profile.get("default_authorized_to_work_answer")) or None
        return answer or _yes_no_answer(profile.get("authorized_to_work_in_us"))

    if "open to working in person" in normalized_blob or "open to working in person" in normalized_blob.replace("-", " "):
        answer = _clean_profile_value(profile.get("default_hybrid_or_onsite_answer")) or None
        return answer or _yes_no_answer(profile.get("open_to_hybrid_or_onsite"))

    if "open to hybrid" in normalized_blob or "open to onsite" in normalized_blob:
        answer = _clean_profile_value(profile.get("default_hybrid_or_onsite_answer")) or None
        return answer or _yes_no_answer(profile.get("open_to_hybrid_or_onsite"))

    if "work remotely" in normalized_blob or "plan to work remotely" in normalized_blob:
        answer = _clean_profile_value(profile.get("default_remote_work_answer")) or None
        return answer or _yes_no_answer(profile.get("open_to_remote"))

    if "address from which you plan on working" in normalized_blob or "current address" in normalized_blob:
        return _clean_profile_value(profile.get("current_work_address")) or _clean_profile_value(
            profile.get("mailing_address")
        ) or _clean_profile_value(profile.get("full_address")) or None

    if "relocat" in normalized_blob:
        return _yes_no_answer(profile.get("open_to_relocation"))

    if "opt in" in normalized_blob or "whatsapp" in normalized_blob:
        return _yes_no_answer(profile.get("whatsapp_opt_in_default"))

    if "privacy policy" in normalized_blob or "candidate privacy" in normalized_blob:
        explicit_text = _match_option_label(question, profile.get("privacy_policy_ack_text"))
        if explicit_text:
            return explicit_text
        answer = _yes_no_answer(profile.get("privacy_policy_ack_default"))
        if answer:
            return answer
        return _clean_profile_value(profile.get("privacy_policy_ack_text")) or None

    if "what is your legal first name" in normalized_blob:
        return _clean_profile_value(profile.get("legal_first_name")) or None

    if "what is your legal last name" in normalized_blob:
        return _clean_profile_value(profile.get("legal_last_name")) or None

    if "legal name" in normalized_blob:
        legal_name = " ".join(
            part
            for part in [
                _clean_profile_value(profile.get("legal_first_name")),
                _clean_profile_value(profile.get("legal_last_name")),
            ]
            if part
        ).strip()
        return legal_name or None

    if "have you ever been employed" in normalized_blob:
        return _yes_no_answer(profile.get("previously_employed_by_target_company"))

    if "which of the following best describes you" in normalized_blob:
        answer = _human_identity_answer(question)
        if answer:
            return answer

    if "ai policy for application" in normalized_blob or "confirm your understanding by selecting yes" in normalized_blob:
        matched_yes = _match_option_label(question, "Yes")
        return matched_yes or "Yes"

    if "have you ever interviewed at" in normalized_blob or "have you interviewed at" in normalized_blob:
        answer = _yes_no_answer(profile.get("previously_interviewed_at_target_company"))
        if answer:
            return answer
        answer = _yes_no_answer(profile.get("interviewed_at_target_company_before"))
        if answer:
            return answer

    if "hispanic" in normalized_blob or "latino" in normalized_blob:
        for key in (
            "hispanic_latino_answer",
            "default_hispanic_latino_answer",
            "is_hispanic_latino",
            "hispanic_latino",
        ):
            raw_answer = profile.get(key)
            matched = _match_option_label(question, raw_answer)
            if matched:
                return matched
            answer = _yes_no_answer(raw_answer)
            if answer:
                return answer

    if "veteran" in normalized_blob:
        for key in (
            "veteran_status",
            "default_veteran_status",
            "protected_veteran_status",
        ):
            raw_answer = profile.get(key)
            matched = _match_option_label(question, raw_answer)
            if matched:
                return matched
            answer = _yes_no_answer(raw_answer)
            if answer:
                return answer
            cleaned = _clean_profile_value(raw_answer)
            if cleaned:
                return cleaned

    if normalized_blob == "gender" or "gender identity" in normalized_blob:
        for key in (
            "gender_answer",
            "default_gender_answer",
            "gender_identity",
        ):
            raw_answer = profile.get(key)
            matched = _match_option_label(question, raw_answer)
            if matched:
                return matched
            answer = _yes_no_answer(raw_answer)
            if answer:
                return answer
            cleaned = _clean_profile_value(raw_answer)
            if cleaned:
                return cleaned

    if normalized_blob == "race" or "please identify your race" in normalized_blob or "race ethnicity" in normalized_blob:
        for key in (
            "race_answer",
            "default_race_answer",
            "race_ethnicity_answer",
            "race_ethnicity",
        ):
            raw_answer = profile.get(key)
            matched = _match_option_label(question, raw_answer)
            if matched:
                return matched
            cleaned = _clean_profile_value(raw_answer)
            if cleaned:
                return cleaned

    if question.group in {"compliance", "demographic_questions"}:
        if _profile_bool(profile.get("default_demographic_decline")):
            decline_answer = _decline_option_answer(question)
            if decline_answer:
                return decline_answer

    if "how did you hear about" in normalized_blob:
        for key in ("how_did_you_hear_about", "job_discovery_source", "default_heard_about_source"):
            raw_answer = profile.get(key)
            answer = _match_option_label(question, raw_answer)
            if answer:
                return answer
            if any(input_field.ui_type == "text_input" for input_field in question.inputs):
                cleaned = _clean_profile_value(raw_answer)
                if cleaned:
                    return cleaned

    if "visa" in normalized_blob or "sponsor" in normalized_blob or "work permit" in normalized_blob:
        if SPONSORSHIP_OPT_INCLUDED_PATTERN.search(blob):
            answer = _yes_no_answer(profile.get("requires_sponsorship_if_opt_included"))
            if answer:
                return answer
        if "future" in normalized_blob or "now or in the future" in normalized_blob:
            answer = _yes_no_answer(profile.get("requires_visa_sponsorship_future"))
            if answer:
                return answer
        if "now" in normalized_blob or "current" in normalized_blob:
            answer = _yes_no_answer(profile.get("requires_visa_sponsorship_now"))
            if answer:
                return answer
        answer = _clean_profile_value(profile.get("default_requires_sponsorship_answer")) or None
        if answer:
            return answer

    return None


def _build_policy_answers(
    schema: GreenhouseApplicationSchema,
    profile: dict[str, Any] | None = None,
) -> dict[str, str]:
    prepared = _prepare_profile_for_schema(schema, profile)
    answers: dict[str, str] = {}
    for question in schema.questions:
        answer = _policy_answer_for_question(
            question,
            prepared,
            company_name=schema.company_name,
        )
        if not answer:
            continue
        answers[question.label] = answer
        normalized_label = _normalized_text(question.label)
        if normalized_label:
            answers[normalized_label] = answer
        for input_field in question.inputs:
            if input_field.api_name:
                answers[input_field.api_name] = answer
    return answers


def _question_is_slm_candidate(question: GreenhouseQuestion) -> bool:
    decision = question.decision
    if decision is None or decision.action != "REVIEW":
        return False
    if question.group in {"data_compliance", "compliance", "demographic_questions"}:
        return False
    normalized_label = _normalized_text(question.label)
    if normalized_label in {"cover letter", "cover letter text"}:
        return False
    if _is_open_ended(question):
        return False
    normalized_label = _normalized_text(question.label)
    if "how did you hear about" in normalized_label or "best describes you" in normalized_label:
        return False
    primary_input = _question_primary_input(question)
    if primary_input is None:
        return False
    if primary_input.ui_type in {"file_upload", "checkbox_group", "hidden", "unknown"}:
        return False
    if primary_input.ui_type == "combobox" and not primary_input.options:
        return False
    if primary_input.ui_type in {"checkbox", "radio"} and not primary_input.options and not question.required:
        return False
    if primary_input.ui_type in {"text_input", "textarea"}:
        blob = _question_blob(question.label, question.description)
        return any(pattern.search(blob) for pattern in SLM_EXPERIENCE_PATTERNS)
    if primary_input.ui_type not in {"combobox", "checkbox", "radio"}:
        return False
    allowed_choice_values = {
        "yes",
        "no",
        "true",
        "false",
        "i am a human being",
        "i am an ai or automated program",
    }
    option_labels = {
        _normalized_text(option.label or option.value)
        for option in primary_input.options
        if _clean_profile_value(option.label or option.value)
    }
    return bool(option_labels) and option_labels.issubset(allowed_choice_values)


def _greenhouse_slm_profile_context(profile: dict[str, Any]) -> dict[str, Any]:
    fields = (
        "current_employer",
        "current_or_previous_job_title",
        "most_recent_degree",
        "most_recent_school",
        "location",
        "current_country",
        "current_state_or_region",
        "current_visa_status",
        "authorized_to_work_in_us",
        "open_to_relocation",
        "open_to_hybrid_or_onsite",
        "anticipated_work_countries",
        "selected_resume_variant",
        "resume_variant_default",
        "candidate_summary",
        "slm_candidate_summary",
        "experience_summary",
        "skills_summary",
    )
    context: dict[str, Any] = {}
    for field_name in fields:
        value = profile.get(field_name)
        if value is None or value == "":
            continue
        context[field_name] = value
    return context


def _greenhouse_profile_retrieval(
    *,
    schema: GreenhouseApplicationSchema,
    question: GreenhouseQuestion,
    profile: dict[str, Any],
) -> dict[str, Any]:
    primary_input = _question_primary_input(question)
    retrieval = retrieve_profile_knowledge(
        question_label=question.label,
        question_description=question.description,
        option_labels=[
            option.label or option.value
            for option in (primary_input.options if primary_input else [])
            if _clean_profile_value(option.label or option.value)
        ],
        job_title=schema.title,
        job_excerpt=_plain_text(schema.job_content)[:6000],
        selected_resume_variant=_clean_profile_value(profile.get("selected_resume_variant")) or None,
    )
    return {
        "retrieved_profile_evidence": [
            {
                "chunk_id": item.item_id,
                "source_file": item.source_file,
                "section_title": item.section_title,
                "text": item.text,
                "word_count": item.word_count,
                "topic_tags": list(item.topic_tags),
                "score": item.score,
                "reasons": list(item.reasons),
            }
            for item in retrieval.evidence_chunks
        ],
        "retrieved_style_snippets": [
            {
                "snippet_id": item.item_id,
                "source_file": item.source_file,
                "section_title": item.section_title,
                "text": item.text,
                "word_count": item.word_count,
                "topic_tags": list(item.topic_tags),
                "score": item.score,
                "reasons": list(item.reasons),
            }
            for item in retrieval.style_snippets
        ],
        "profile_retrieval_summary": list(retrieval.retrieval_summary),
        "retrieved_chunk_ids": [item.item_id for item in retrieval.evidence_chunks],
        "style_snippet_ids": [item.item_id for item in retrieval.style_snippets],
        "has_strong_evidence": retrieval.has_strong_evidence,
    }


def _retrieval_compact_payload(items: list[dict[str, Any]], *, text_limit: int) -> list[dict[str, Any]]:
    payload: list[dict[str, Any]] = []
    for item in items[:3]:
        payload.append(
            {
                "chunk_id": item.get("chunk_id") or item.get("snippet_id"),
                "source_file": item.get("source_file"),
                "section_title": item.get("section_title"),
                "text": _clean_profile_value(item.get("text"))[:text_limit],
                "topic_tags": list(item.get("topic_tags") or []),
                "score": item.get("score"),
                "reasons": list(item.get("reasons") or []),
            }
        )
    return payload


def _question_has_binary_yes_no_options(question: GreenhouseQuestion) -> bool:
    primary_input = _question_primary_input(question)
    if primary_input is None:
        return False
    labels = {
        _normalized_text(option.label or option.value)
        for option in primary_input.options
        if _clean_profile_value(option.label or option.value)
    }
    return labels.issubset({"yes", "no", "true", "false"}) and bool(labels)


def _leading_sentences(text: str, *, limit: int = 2) -> str:
    sentences = [
        sentence.strip()
        for sentence in re.split(r"(?<=[.!?])\s+", " ".join(str(text or "").split()))
        if sentence.strip()
    ]
    if not sentences:
        return " ".join(str(text or "").split())
    return " ".join(sentences[:limit]).strip()


def _profile_knowledge_direct_suggestion(
    *,
    question: GreenhouseQuestion,
    retrieval: dict[str, Any],
) -> GreenhouseSuggestedAnswer | None:
    primary_input = _question_primary_input(question)
    if primary_input is None:
        return None
    if not retrieval["has_strong_evidence"] or not retrieval["retrieved_profile_evidence"]:
        return None
    top_chunk = retrieval["retrieved_profile_evidence"][0]
    top_chunk_id = str(top_chunk.get("chunk_id") or "").strip()
    top_text = _clean_profile_value(top_chunk.get("text"))
    if not top_text:
        return None
    normalized_blob = _question_blob(question.label, question.description)
    if not any(pattern.search(normalized_blob) for pattern in SLM_EXPERIENCE_PATTERNS):
        return None

    if primary_input.ui_type in {"combobox", "checkbox", "radio"} and _question_has_binary_yes_no_options(question):
        matched = _match_option_label(question, "Yes")
        if matched:
            return GreenhouseSuggestedAnswer(
                question_label=question.label,
                api_name=primary_input.api_name or None,
                value=matched,
                source="profile_knowledge",
                reason=f"Strong retrieved evidence supports a Yes answer [{top_chunk_id}].",
                confidence=0.76,
                draft_source="profile_knowledge",
                retrieved_chunk_ids=list(retrieval["retrieved_chunk_ids"]),
                style_snippet_ids=list(retrieval["style_snippet_ids"]),
                retrieval_summary=list(retrieval["profile_retrieval_summary"]),
            )

    if primary_input.ui_type in {"text_input", "textarea"}:
        excerpt = _leading_sentences(top_text, limit=2)
        if not excerpt:
            return None
        answer = f"Yes — {excerpt}"
        return GreenhouseSuggestedAnswer(
            question_label=question.label,
            api_name=primary_input.api_name or None,
            value=answer[:400],
            source="profile_knowledge",
            reason=f"Drafted directly from retrieved evidence [{top_chunk_id}].",
            confidence=0.71,
            draft_source="profile_knowledge",
            retrieved_chunk_ids=list(retrieval["retrieved_chunk_ids"]),
            style_snippet_ids=list(retrieval["style_snippet_ids"]),
            retrieval_summary=list(retrieval["profile_retrieval_summary"]),
        )
    return None


def _call_greenhouse_slm(
    *,
    schema: GreenhouseApplicationSchema,
    question: GreenhouseQuestion,
    profile: dict[str, Any],
) -> GreenhouseSuggestedAnswer | None:
    if not _greenhouse_slm_enabled():
        return None
    question_context = _greenhouse_slm_question_context(schema, question)
    primary_input = _question_primary_input(question)
    if primary_input is None:
        return None
    retrieval = _greenhouse_profile_retrieval(
        schema=schema,
        question=question,
        profile=profile,
    )
    direct_suggestion = _profile_knowledge_direct_suggestion(
        question=question,
        retrieval=retrieval,
    )
    if direct_suggestion is not None:
        return direct_suggestion
    if not retrieval["has_strong_evidence"] or not retrieval["retrieved_profile_evidence"]:
        return None
    payload = {
        "company_name": schema.company_name,
        "job_title": schema.title,
        "job_location": schema.job_location,
        "job_content_excerpt": _plain_text(schema.job_content)[:2200],
        "question": question_context,
        "candidate_profile": _greenhouse_slm_profile_context(profile),
        "retrieved_profile_evidence": _retrieval_compact_payload(
            retrieval["retrieved_profile_evidence"],
            text_limit=420,
        ),
        "retrieved_style_snippets": _retrieval_compact_payload(
            retrieval["retrieved_style_snippets"],
            text_limit=180,
        ),
        "profile_retrieval_summary": retrieval["profile_retrieval_summary"],
    }
    body: dict[str, Any] = {
        "model": _greenhouse_slm_model(),
        "stream": False,
        "messages": [
            {"role": "system", "content": GREENHOUSE_SLM_SYSTEM_PROMPT},
            {
                "role": "user",
                "content": f"Suggest a supervised answer for this application payload: {json.dumps(payload, separators=(',', ':'))}",
            },
        ],
    }
    if body["model"].strip().lower().startswith("qwen3"):
        body["think"] = False

    request = urllib.request.Request(
        _greenhouse_slm_endpoint(),
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=_greenhouse_slm_timeout_seconds()) as response:
            raw = response.read()
    except urllib.error.URLError:
        return None
    except Exception:
        return None
    try:
        parsed = json.loads(raw.decode("utf-8"))
    except json.JSONDecodeError:
        return None
    content = str(((parsed.get("message") or {}).get("content") or "")).strip()
    if not content:
        return None
    candidate = _extract_json_object(content)
    if not candidate:
        return None
    try:
        suggestion = json.loads(candidate)
    except json.JSONDecodeError:
        return None
    if not suggestion.get("should_fill"):
        return None
    answer = _clean_profile_value(suggestion.get("answer"))
    if not answer:
        return None
    if primary_input.options:
        matched = _match_option_label(question, answer)
        if not matched:
            return None
        answer = matched
    confidence = suggestion.get("confidence")
    try:
        confidence_value = float(confidence)
    except (TypeError, ValueError):
        confidence_value = None
    return GreenhouseSuggestedAnswer(
        question_label=question.label,
        api_name=primary_input.api_name or None,
        value=answer,
        source="slm",
        reason=_clean_profile_value(suggestion.get("reason")) or None,
        confidence=confidence_value,
        draft_source="profile_knowledge",
        retrieved_chunk_ids=list(retrieval["retrieved_chunk_ids"]),
        style_snippet_ids=list(retrieval["style_snippet_ids"]),
        retrieval_summary=list(retrieval["profile_retrieval_summary"]),
    )


def build_slm_suggested_answers(
    analysis: GreenhouseApplicationAnalysis,
    *,
    profile: dict[str, Any] | None = None,
) -> list[GreenhouseSuggestedAnswer]:
    prepared_profile = _prepare_profile_for_schema(analysis.schema, profile)
    policy_answers = _build_policy_answers(analysis.schema, prepared_profile)
    suggestions: list[GreenhouseSuggestedAnswer] = []
    for question in analysis.schema.questions:
        if not _question_is_slm_candidate(question):
            continue
        if _lookup_approved_answer(question, policy_answers):
            continue
        suggestion = _call_greenhouse_slm(
            schema=analysis.schema,
            question=question,
            profile=prepared_profile,
        )
        if suggestion is not None:
            suggestions.append(suggestion)
    return suggestions


def _question_is_interactive_slm_candidate(question: GreenhouseQuestion) -> bool:
    decision = question.decision
    if decision is None or decision.action not in {"REVIEW", "QUEUE"}:
        return False
    if question.group in {"data_compliance", "compliance", "demographic_questions"}:
        return False
    normalized_label = _normalized_text(question.label)
    if normalized_label in {"cover letter", "cover letter text"}:
        return False
    primary_input = _question_primary_input(question)
    if primary_input is None:
        return False
    if primary_input.ui_type in {"file_upload", "hidden", "unknown"}:
        return False
    if primary_input.ui_type in {"checkbox_group"}:
        return False
    if primary_input.ui_type == "combobox" and not primary_input.options:
        return False
    return primary_input.ui_type in {"text_input", "textarea", "combobox", "checkbox", "radio"}


def build_interactive_slm_suggested_answers(
    analysis: GreenhouseApplicationAnalysis,
    *,
    profile: dict[str, Any] | None = None,
    approved_answers: dict[str, Any] | None = None,
) -> list[GreenhouseSuggestedAnswer]:
    prepared_profile = _prepare_profile_for_schema(analysis.schema, profile)
    merged_answers = _build_policy_answers(analysis.schema, prepared_profile)
    if isinstance(approved_answers, dict):
        merged_answers.update(approved_answers)
    suggestions: list[GreenhouseSuggestedAnswer] = []
    seen_keys: set[tuple[str, str | None]] = set()
    for existing in analysis.suggested_answers:
        key = (existing.question_label, existing.api_name)
        seen_keys.add(key)
        suggestions.append(existing)
    for question in analysis.schema.questions:
        if not _question_is_interactive_slm_candidate(question):
            continue
        if _lookup_approved_answer(question, merged_answers):
            continue
        key = (question.label, (_question_primary_input(question) or GreenhouseInput("", "", "")).api_name or None)
        if key in seen_keys:
            continue
        suggestion = _call_greenhouse_slm(
            schema=analysis.schema,
            question=question,
            profile=prepared_profile,
        )
        if suggestion is not None:
            suggestions.append(suggestion)
            seen_keys.add((suggestion.question_label, suggestion.api_name))
    return suggestions


def classify_question(
    question: GreenhouseQuestion,
    profile: dict[str, Any] | None = None,
    company_name: str | None = None,
) -> GreenhouseDecision:
    profile = {key: _clean_profile_value(value) for key, value in (profile or {}).items()}
    normalized_label = _normalized_text(question.label)
    primary_profile_key = _resolve_profile_key(question, profile)

    if question.group == "data_compliance":
        return GreenhouseDecision(
            action="REVIEW" if question.required else "SKIP_OPTIONAL",
            reason="Data-consent items require explicit policy handling.",
        )

    if normalized_label in {"cover letter", "cover letter text"}:
        return GreenhouseDecision(
            action="REVIEW",
            reason="Cover letters are not treated as safe autonomous autofill content.",
            profile_key="cover_letter_path",
        )

    if _has_only_hidden_inputs(question):
        return GreenhouseDecision(
            action="SKIP_OPTIONAL",
            reason="Hidden derived fields are managed by the form rather than direct user input.",
        )

    if _is_demographic(question):
        return GreenhouseDecision(
            action="REVIEW" if question.required else "SKIP_OPTIONAL",
            reason="Demographic and EEOC questions stay out of autonomous submit decisions.",
        )

    if _is_target_company_employment_question(question, company_name):
        return GreenhouseDecision(
            action="REVIEW",
            reason="Target-company employment history questions require explicit review.",
        )

    if _label_or_description_matches(question, REVIEW_PATTERNS):
        return GreenhouseDecision(
            action="REVIEW",
            reason="Question is policy-sensitive and needs explicit review.",
            profile_key=primary_profile_key,
        )

    if normalized_label == "legal name if different than above" and primary_profile_key:
        if profile.get(primary_profile_key):
            return GreenhouseDecision(
                action="AUTOFILL",
                reason="Question maps to a low-risk structured profile field.",
                profile_key=primary_profile_key,
            )
        return GreenhouseDecision(
            action="SKIP_OPTIONAL",
            reason="Optional mapped profile value is missing.",
            profile_key=primary_profile_key,
        )

    if _label_or_description_matches(question, REVIEW_IF_REQUIRED_PATTERNS):
        return GreenhouseDecision(
            action="REVIEW" if question.required else "SKIP_OPTIONAL",
            reason="Question is common but still needs explicit review when required.",
            profile_key=primary_profile_key,
        )

    if primary_profile_key:
        if _required_missing(question, primary_profile_key, profile):
            return GreenhouseDecision(
                action="BLOCKED",
                reason="Required mapped profile value is missing.",
                profile_key=primary_profile_key,
            )
        if profile.get(primary_profile_key):
            return GreenhouseDecision(
                action="AUTOFILL",
                reason="Question maps to a low-risk structured profile field.",
                profile_key=primary_profile_key,
            )
        return GreenhouseDecision(
            action="SKIP_OPTIONAL",
            reason="Optional mapped profile value is missing.",
            profile_key=primary_profile_key,
        )

    if _is_open_ended(question):
        return GreenhouseDecision(
            action="QUEUE",
            reason="Open-ended questions require queued or approved content.",
        )

    if any(input_field.api_type == "input_file" for input_field in question.inputs):
        return GreenhouseDecision(
            action="REVIEW" if question.required else "SKIP_OPTIONAL",
            reason="Unknown file uploads need explicit handling.",
        )

    if question.required:
        return GreenhouseDecision(
            action="REVIEW",
            reason="Required unmapped question needs review.",
        )

    return GreenhouseDecision(
        action="SKIP_OPTIONAL",
        reason="Optional unmapped question is skipped by default.",
    )


def analyze_greenhouse_application(
    schema: GreenhouseApplicationSchema,
    profile: dict[str, Any] | None = None,
    approved_answers: dict[str, Any] | None = None,
) -> GreenhouseApplicationAnalysis:
    prepared_profile = _prepare_profile_for_schema(schema, profile)
    merged_answers = _build_policy_answers(schema, prepared_profile)
    if isinstance(approved_answers, dict):
        merged_answers.update(approved_answers)
    counts: dict[str, int] = {}
    base_attention_required: list[str] = []

    for question in schema.questions:
        question.decision = classify_question(
            question,
            profile=prepared_profile,
            company_name=schema.company_name,
        )
        action = question.decision.action
        counts[action] = counts.get(action, 0) + 1
        if action in {"QUEUE", "REVIEW", "STOP", "BLOCKED"}:
            base_attention_required.append(f"{question.label}: {question.decision.reason}")

    analysis = GreenhouseApplicationAnalysis(
        schema=schema,
        auto_submit_eligible=False,
        decision_counts=counts,
        attention_required=base_attention_required,
    )
    analysis.suggested_answers = build_slm_suggested_answers(analysis, profile=prepared_profile)
    analysis.review_queue = build_review_queue(
        analysis,
        approved_answers=merged_answers,
        suggested_answers=analysis.suggested_answers,
    )
    analysis.submit_safety = evaluate_submit_safety(analysis, profile=prepared_profile)
    analysis.auto_submit_eligible = analysis.submit_safety.eligible
    analysis.attention_required = list(analysis.submit_safety.blockers)
    return analysis


def inspect_greenhouse_application(
    url: str,
    *,
    profile: dict[str, Any] | None = None,
    approved_answers: dict[str, Any] | None = None,
    session: requests.Session | None = None,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
) -> GreenhouseApplicationAnalysis:
    schema = load_greenhouse_application_schema(
        url,
        session=session,
        timeout_seconds=timeout_seconds,
    )
    return analyze_greenhouse_application(
        schema,
        profile=profile,
        approved_answers=approved_answers,
    )


def build_autofill_targets(
    analysis: GreenhouseApplicationAnalysis,
    profile: dict[str, Any] | None = None,
) -> list[GreenhouseFillTarget]:
    prepared_profile = _prepare_profile_for_schema(analysis.schema, profile)
    profile_map = {key: _clean_profile_value(value) for key, value in prepared_profile.items()}
    targets: list[GreenhouseFillTarget] = []
    for question in analysis.schema.questions:
        decision = question.decision
        if decision is None or decision.action != "AUTOFILL" or not decision.profile_key:
            continue
        value = profile_map.get(decision.profile_key, "")
        if not value:
            continue
        for input_field in question.inputs:
            if input_field.ui_type == "file_upload":
                if decision.profile_key not in {"resume_path", "cover_letter_path"}:
                    continue
            elif input_field.ui_type not in {"text_input", "textarea", "combobox"}:
                continue
            targets.append(
                GreenhouseFillTarget(
                    question_label=question.label,
                    api_name=input_field.api_name,
                    ui_type=input_field.ui_type,
                    selector=input_field.selector,
                    profile_key=decision.profile_key,
                    value=value,
                    value_source="profile",
                    question_group=question.group,
                    options=list(input_field.options),
                )
            )
            break
    return targets


def _lookup_approved_answer(
    question: GreenhouseQuestion,
    approved_answers: dict[str, Any] | None,
) -> str | None:
    if not isinstance(approved_answers, dict):
        return None
    candidate_keys = [question.label, _normalized_text(question.label)]
    candidate_keys.extend(input_field.api_name for input_field in question.inputs if input_field.api_name)
    for key in candidate_keys:
        if key not in approved_answers:
            continue
        value = approved_answers.get(key)
        if value is None:
            continue
        cleaned = _normalize_answer_value(value)
        if cleaned:
            return cleaned
    return None


def _lookup_suggested_answer(
    question: GreenhouseQuestion,
    suggested_answers: list[GreenhouseSuggestedAnswer] | None,
) -> GreenhouseSuggestedAnswer | None:
    if not suggested_answers:
        return None
    candidate_keys = {question.label, _normalized_text(question.label)}
    candidate_keys.update(input_field.api_name for input_field in question.inputs if input_field.api_name)
    for item in suggested_answers:
        if item.question_label in candidate_keys:
            return item
        if item.api_name and item.api_name in candidate_keys:
            return item
    return None


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, (list, tuple, set)):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def build_review_queue(
    analysis: GreenhouseApplicationAnalysis,
    approved_answers: dict[str, Any] | None = None,
    suggested_answers: list[GreenhouseSuggestedAnswer] | None = None,
) -> list[GreenhouseReviewQueueItem]:
    queue: list[GreenhouseReviewQueueItem] = []
    supported_answer_types = {
        "text_input",
        "textarea",
        "combobox",
        "checkbox",
        "checkbox_group",
        "radio",
        "file_upload",
    }
    for question in analysis.schema.questions:
        decision = question.decision
        if decision is None or decision.action not in {"QUEUE", "REVIEW", "BLOCKED", "STOP"}:
            continue
        primary_input = _question_primary_input(question)
        approved_answer = _lookup_approved_answer(question, approved_answers)
        suggested_answer = _lookup_suggested_answer(question, suggested_answers)
        if decision.action in {"BLOCKED", "STOP"}:
            status = "blocked"
        elif approved_answer and primary_input and primary_input.ui_type in supported_answer_types:
            status = "answered"
        else:
            status = "pending"
        queue.append(
            GreenhouseReviewQueueItem(
                question_label=question.label,
                api_name=primary_input.api_name if primary_input else None,
                field_type=primary_input.ui_type if primary_input else "unknown",
                required=question.required,
                group=question.group,
                decision=decision.action,
                reason=decision.reason,
                suggested_answer=suggested_answer.value if suggested_answer else None,
                suggested_answer_source=suggested_answer.source if suggested_answer else None,
                suggested_answer_reason=suggested_answer.reason if suggested_answer else None,
                suggested_answer_confidence=suggested_answer.confidence if suggested_answer else None,
                draft_source=(getattr(suggested_answer, "draft_source", None) if suggested_answer else None),
                retrieved_chunk_ids=_coerce_string_list(getattr(suggested_answer, "retrieved_chunk_ids", []))
                if suggested_answer
                else [],
                style_snippet_ids=_coerce_string_list(getattr(suggested_answer, "style_snippet_ids", []))
                if suggested_answer
                else [],
                retrieval_summary=_coerce_string_list(getattr(suggested_answer, "retrieval_summary", []))
                if suggested_answer
                else [],
                approved_answer=approved_answer,
                status=status,
            )
        )
    return queue


def build_approved_answer_targets(
    analysis: GreenhouseApplicationAnalysis,
    approved_answers: dict[str, Any] | None = None,
    profile: dict[str, Any] | None = None,
) -> list[GreenhouseFillTarget]:
    policy_answers = _build_policy_answers(analysis.schema, profile)
    explicit_answers = approved_answers if isinstance(approved_answers, dict) else {}
    targets: list[GreenhouseFillTarget] = []
    for question in analysis.schema.questions:
        decision = question.decision
        if decision is None or decision.action not in {"QUEUE", "REVIEW", "SKIP_OPTIONAL"}:
            continue
        value_source = "policy"
        approved_answer = _lookup_approved_answer(question, explicit_answers)
        if approved_answer:
            value_source = "approved_answer"
        else:
            approved_answer = _lookup_approved_answer(question, policy_answers)
        if not approved_answer:
            continue
        for input_field in question.inputs:
            if input_field.ui_type not in {
                "text_input",
                "textarea",
                "combobox",
                "checkbox",
                "checkbox_group",
                "radio",
                "file_upload",
            }:
                continue
            targets.append(
                GreenhouseFillTarget(
                    question_label=question.label,
                    api_name=input_field.api_name,
                    ui_type=input_field.ui_type,
                    selector=input_field.selector,
                    profile_key=input_field.api_name or question.label,
                    value=approved_answer,
                    value_source=value_source,
                    question_group=question.group,
                    options=list(input_field.options),
                )
            )
            break
    return targets


def build_suggested_answer_targets(
    analysis: GreenhouseApplicationAnalysis,
) -> list[GreenhouseFillTarget]:
    targets: list[GreenhouseFillTarget] = []
    for question in analysis.schema.questions:
        decision = question.decision
        if decision is None or decision.action != "REVIEW":
            continue
        suggested_answer = _lookup_suggested_answer(question, analysis.suggested_answers)
        if suggested_answer is None:
            continue
        for input_field in question.inputs:
            if input_field.ui_type not in {
                "text_input",
                "textarea",
                "combobox",
                "checkbox",
                "radio",
            }:
                continue
            targets.append(
                GreenhouseFillTarget(
                    question_label=question.label,
                    api_name=input_field.api_name,
                    ui_type=input_field.ui_type,
                    selector=input_field.selector,
                    profile_key=input_field.api_name or question.label,
                    value=suggested_answer.value,
                    value_source="slm_suggestion",
                    question_group=question.group,
                    options=list(input_field.options),
                )
            )
            break
    return targets


def build_interactive_suggested_answer_targets(
    analysis: GreenhouseApplicationAnalysis,
    *,
    profile: dict[str, Any] | None = None,
    approved_answers: dict[str, Any] | None = None,
) -> list[GreenhouseFillTarget]:
    suggestions = build_interactive_slm_suggested_answers(
        analysis,
        profile=profile,
        approved_answers=approved_answers,
    )
    targets: list[GreenhouseFillTarget] = []
    for question in analysis.schema.questions:
        decision = question.decision
        if decision is None or decision.action not in {"REVIEW", "QUEUE"}:
            continue
        suggested_answer = _lookup_suggested_answer(question, suggestions)
        if suggested_answer is None:
            continue
        for input_field in question.inputs:
            if input_field.ui_type not in {
                "text_input",
                "textarea",
                "combobox",
                "checkbox",
                "radio",
            }:
                continue
            targets.append(
                GreenhouseFillTarget(
                    question_label=question.label,
                    api_name=input_field.api_name,
                    ui_type=input_field.ui_type,
                    selector=input_field.selector,
                    profile_key=input_field.api_name or question.label,
                    value=suggested_answer.value,
                    value_source="slm_interactive_suggestion",
                    question_group=question.group,
                    options=list(input_field.options),
                )
            )
            break
    return targets


def evaluate_submit_safety(
    analysis: GreenhouseApplicationAnalysis,
    *,
    profile: dict[str, Any] | None = None,
) -> GreenhouseSubmitSafety:
    blockers: list[str] = []
    for item in analysis.review_queue:
        if item.status in {"blocked", "pending"}:
            blockers.append(f"{item.question_label}: {item.reason}")
    prepared_profile = {key: value for key, value in (profile or {}).items()}
    job_blockers = _evaluate_job_policy_blockers(analysis.schema, prepared_profile)
    blockers.extend(job_blockers)
    return GreenhouseSubmitSafety(
        eligible=not blockers,
        blockers=blockers,
        requires_browser_validation=True,
        job_blockers=job_blockers,
    )


def _load_profile_file(path: str | None) -> dict[str, Any]:
    if not path:
        return {}
    profile_path = Path(path)
    payload = json.loads(profile_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise CrawlFetchError("Profile file must contain a JSON object.")
    return payload


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Inspect a Greenhouse application form and classify fields for autofill safety."
    )
    parser.add_argument("url", help="Greenhouse hosted job URL or embed URL.")
    parser.add_argument(
        "--profile-file",
        help="Optional JSON file with candidate profile values.",
    )
    parser.add_argument(
        "--answers-file",
        help="Optional JSON file with approved answers keyed by question label or api name.",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"HTTP timeout in seconds. Default: {DEFAULT_TIMEOUT_SECONDS}.",
    )
    parser.add_argument(
        "--indent",
        type=int,
        default=2,
        help="JSON indentation level for output.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        profile = _load_profile_file(args.profile_file)
        approved_answers = _load_profile_file(args.answers_file)
        analysis = inspect_greenhouse_application(
            args.url,
            profile=profile,
            approved_answers=approved_answers,
            timeout_seconds=args.timeout_seconds,
        )
    except Exception as exc:  # noqa: BLE001
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(analysis.to_dict(), indent=args.indent, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
