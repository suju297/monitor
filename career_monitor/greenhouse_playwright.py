from __future__ import annotations

import argparse
import json
import os
import re
import sys
import threading
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

from .constants import DEFAULT_TIMEOUT_SECONDS
from .exceptions import CrawlFetchError
from .greenhouse_assistant import (
    GreenhouseApplicationAnalysis,
    GreenhouseFillTarget,
    build_approved_answer_targets,
    build_autofill_targets,
    build_interactive_suggested_answer_targets,
    build_suggested_answer_targets,
    inspect_greenhouse_application,
)
from .greenhouse_submission_state import (
    DEFAULT_GREENHOUSE_SUBMISSION_STATE_FILE,
    build_greenhouse_application_key,
    get_submission_record,
    load_greenhouse_submission_state,
    record_submission,
    save_greenhouse_submission_state,
)
from .greenhouse_page_observer import (
    drain_greenhouse_page_observer,
    install_greenhouse_page_observer,
)
from .jobs_db_sync import DEFAULT_JOBS_DB_FILE, sync_greenhouse_job_record
from .local_paths import prefer_legacy_or_local
from .greenhouse_trace_store import (
    DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
    DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    DEFAULT_GREENHOUSE_TRACE_FILE,
    DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
    append_greenhouse_manual_observation,
    append_greenhouse_trace,
)

DEFAULT_GREENHOUSE_STORAGE_STATE_FILE = ".local/greenhouse_browser_storage_state.json"
DEFAULT_CHROME_EXECUTABLE_CANDIDATES = (
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "~/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
)
_SHARED_BROWSER_LOCK = threading.Lock()
_SHARED_BROWSER_SESSIONS: dict[tuple[bool, str], "_GreenhouseSharedBrowserSession"] = {}


def _default_chrome_executable_path() -> str | None:
    for raw_candidate in DEFAULT_CHROME_EXECUTABLE_CANDIDATES:
        candidate = Path(raw_candidate).expanduser()
        if candidate.exists():
            return str(candidate)
    return None


def _browser_launch_kwargs(*, headless: bool) -> dict[str, Any]:
    kwargs: dict[str, Any] = {"headless": headless}
    preferred_browser = str(os.getenv("GREENHOUSE_BROWSER") or "").strip().lower()
    explicit_executable = str(os.getenv("GREENHOUSE_BROWSER_EXECUTABLE") or "").strip()

    if preferred_browser == "chromium":
        return kwargs

    executable_path: str | None = None
    if explicit_executable:
        candidate = Path(explicit_executable).expanduser()
        if candidate.exists():
            executable_path = str(candidate)
    elif not headless or preferred_browser == "chrome":
        executable_path = _default_chrome_executable_path()

    if executable_path:
        kwargs["executable_path"] = executable_path
    elif preferred_browser == "chrome" or not headless:
        kwargs["channel"] = "chrome"

    return kwargs


class _GreenhouseSharedBrowserSession:
    def __init__(self, *, headless: bool, storage_state_path: Path) -> None:
        from playwright.sync_api import sync_playwright

        self._lock = threading.Lock()
        self._storage_state_path = storage_state_path
        self._playwright_cm = sync_playwright()
        self._playwright = self._playwright_cm.start()
        self._browser = self._playwright.chromium.launch(**_browser_launch_kwargs(headless=headless))
        self._context = self._browser.new_context(**_context_storage_state_kwargs(storage_state_path))

    def new_page(self):
        with self._lock:
            opener_page = None
            for candidate in self._context.pages:
                try:
                    if not candidate.is_closed():
                        opener_page = candidate
                        break
                except Exception:
                    continue
            if opener_page is None:
                return self._context.new_page()
            existing_page_count = len(self._context.pages)
            try:
                opener_page.evaluate("() => window.open('about:blank', '_blank')")
                for _ in range(20):
                    pages = [page for page in self._context.pages if not page.is_closed()]
                    if len(pages) > existing_page_count:
                        return pages[-1]
                    try:
                        opener_page.wait_for_timeout(50)
                    except Exception:
                        break
            except Exception:
                pass
            return self._context.new_page()

    def persist_storage_state(self) -> None:
        with self._lock:
            self._storage_state_path.parent.mkdir(parents=True, exist_ok=True)
            self._context.storage_state(path=str(self._storage_state_path))


def _shared_browser_session(headless: bool, storage_state_path: Path) -> _GreenhouseSharedBrowserSession:
    key = (bool(headless), str(storage_state_path))
    with _SHARED_BROWSER_LOCK:
        session = _SHARED_BROWSER_SESSIONS.get(key)
        if session is None:
            session = _GreenhouseSharedBrowserSession(
                headless=headless,
                storage_state_path=storage_state_path,
            )
            _SHARED_BROWSER_SESSIONS[key] = session
        return session


def _reset_shared_browser_session(headless: bool, storage_state_path: Path) -> None:
    key = (bool(headless), str(storage_state_path))
    with _SHARED_BROWSER_LOCK:
        _SHARED_BROWSER_SESSIONS.pop(key, None)


def _new_shared_page(
    *,
    headless: bool,
    storage_state_path: Path,
):
    session = _shared_browser_session(headless=headless, storage_state_path=storage_state_path)
    try:
        return session, session.new_page()
    except Exception:
        _reset_shared_browser_session(headless=headless, storage_state_path=storage_state_path)
        session = _shared_browser_session(headless=headless, storage_state_path=storage_state_path)
        return session, session.new_page()


@dataclass
class FillExecution:
    question_label: str
    api_name: str
    ui_type: str
    profile_key: str
    value_source: str
    selector: str | None
    success: bool
    value_preview: str | None = None
    bound_via: str | None = None
    verification: str | None = None
    error: str | None = None


@dataclass
class PlaywrightAutofillResult:
    analysis: dict[str, Any]
    page_url: str
    form_visible: bool
    filled: list[FillExecution] = field(default_factory=list)
    skipped: list[FillExecution] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    browser_validation_errors: list[str] = field(default_factory=list)
    submit_safety: dict[str, Any] | None = None
    submission: dict[str, Any] | None = None
    resume_selection: dict[str, Any] | None = None
    trace: dict[str, Any] | None = None
    manual_observation: dict[str, Any] | None = None
    jobs_db_sync: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "analysis": self.analysis,
            "page_url": self.page_url,
            "form_visible": self.form_visible,
            "filled": [asdict(item) for item in self.filled],
            "skipped": [asdict(item) for item in self.skipped],
            "errors": list(self.errors),
            "browser_validation_errors": list(self.browser_validation_errors),
            "submit_safety": self.submit_safety,
            "submission": self.submission,
            "resume_selection": self.resume_selection,
            "trace": self.trace,
            "manual_observation": self.manual_observation,
            "jobs_db_sync": self.jobs_db_sync,
        }


@dataclass
class SubmissionAttempt:
    requested: bool
    application_key: str | None = None
    state_file: str | None = None
    attempted: bool = False
    submitted: bool = False
    persisted: bool = False
    duplicate_detected: bool = False
    submit_button_found: bool | None = None
    submit_button_enabled: bool | None = None
    submit_button_label: str | None = None
    confirmation_detected: bool = False
    confirmation_url: str | None = None
    confirmation_note: str | None = None
    manual_review_waited: bool = False
    manual_review_ended_reason: str | None = None
    blockers: list[str] = field(default_factory=list)
    existing_record: dict[str, Any] | None = None
    saved_record: dict[str, Any] | None = None


def _load_json_object_file(path: str | None, *, required_label: str) -> dict[str, Any]:
    if not path:
        raise CrawlFetchError(f"A {required_label} file is required.")
    profile_path = Path(path)
    payload = json.loads(profile_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise CrawlFetchError(f"{required_label.capitalize()} file must contain a JSON object.")
    return payload


def _preview_value(profile_key: str, value: str) -> str:
    if profile_key in {"email"}:
        return value[:3] + "***" if len(value) > 3 else "***"
    if profile_key in {"resume_path", "cover_letter_path"}:
        return Path(value).name
    return value[:60]


def _dedupe_fill_targets(targets: list[GreenhouseFillTarget]) -> list[GreenhouseFillTarget]:
    unique: list[GreenhouseFillTarget] = []
    seen: set[tuple[str, str, str]] = set()
    for target in targets:
        key = (target.question_label, target.api_name, target.ui_type)
        if key in seen:
            continue
        seen.add(key)
        unique.append(target)
    return unique


def _match_resume_variant(profile: dict[str, Any], path: str | None) -> str | None:
    if not path:
        return None
    raw = profile.get("resume_variants")
    if not isinstance(raw, dict):
        return None
    candidate = str(path).strip()
    candidate_path = str(Path(candidate).expanduser())
    basename = Path(candidate).name.strip().lower()
    basename_matches: list[str] = []
    for key, value in raw.items():
        stored_path = ""
        if isinstance(value, dict):
            stored_path = str(value.get("path") or "").strip()
        else:
            stored_path = str(value or "").strip()
        if not stored_path:
            continue
        variant_key = str(key).strip() or None
        if str(Path(stored_path).expanduser()) == candidate_path:
            return variant_key
        if Path(stored_path).name.strip().lower() == basename and variant_key:
            basename_matches.append(variant_key)
    if len(basename_matches) == 1:
        return basename_matches[0]
    return None


def _resume_selection_payload(
    profile: dict[str, Any],
    targets: list[GreenhouseFillTarget],
) -> dict[str, Any] | None:
    resume_target = next((target for target in targets if target.profile_key == "resume_path"), None)
    if resume_target is None:
        return None
    selected_path = str(profile.get("selected_resume_path") or resume_target.value).strip() or resume_target.value
    selected_variant = (
        str(profile.get("selected_resume_variant") or "").strip()
        or _match_resume_variant(profile, selected_path)
    )
    source = str(profile.get("resume_selection_source") or "").strip() or resume_target.value_source
    payload = {
        "selected_resume_variant": selected_variant or None,
        "selected_resume_path": selected_path or None,
        "selected_resume_file": Path(selected_path).name if selected_path else None,
        "resume_selection_source": source or None,
        "resume_selection_reason": str(profile.get("resume_selection_reason") or "").strip() or None,
        "resume_selection_confidence": profile.get("resume_selection_confidence"),
    }
    return payload


def _label_pattern(label: str) -> re.Pattern[str]:
    escaped = re.escape(" ".join(label.split()))
    return re.compile(rf"^\s*{escaped}\s*\*?\s*$", re.IGNORECASE)


def _label_contains_pattern(label: str) -> re.Pattern[str]:
    escaped = re.escape(" ".join(label.split()))
    return re.compile(escaped, re.IGNORECASE)


def _normalized_choice(value: str) -> str:
    return re.sub(r"\s+", " ", re.sub(r"[^a-z0-9]+", " ", value.strip().lower())).strip()


def _dismiss_cookie_consent(page) -> None:
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


def _application_form_locators(page) -> list[Any]:
    return [
        page.locator("form#application-form, .application--form").first,
        page.locator(
            "form.form-template",
            has=page.get_by_role("button", name=re.compile(r"submit application", re.IGNORECASE)),
        ).first,
        page.locator("form.form-template", has=page.locator("input[type='file']")).first,
    ]


def _ensure_form_visible(page, timeout_ms: int) -> bool:
    for form_locator in _application_form_locators(page):
        try:
            if form_locator.count() == 0:
                continue
            form_locator.wait_for(state="visible", timeout=timeout_ms)
            return True
        except Exception:
            continue

    _dismiss_cookie_consent(page)
    apply_candidates = (
        page.get_by_role("button", name=re.compile(r"^apply$", re.IGNORECASE)).first,
        page.locator("button[aria-label='Apply']").first,
        page.locator("button:has-text('Apply')").first,
        page.get_by_role("link", name=re.compile(r"^apply$", re.IGNORECASE)).first,
    )
    for locator in apply_candidates:
        try:
            if locator.count() == 0:
                continue
            locator.click(timeout=min(timeout_ms, 3000))
            for form_locator in _application_form_locators(page):
                try:
                    if form_locator.count() == 0:
                        continue
                    form_locator.wait_for(state="visible", timeout=timeout_ms)
                    return True
                except Exception:
                    continue
        except Exception:
            continue
    for form_locator in _application_form_locators(page):
        try:
            if form_locator.count() > 0 and form_locator.is_visible(timeout=500):
                return True
        except Exception:
            continue
    return False


def _resolve_locator(page, target: GreenhouseFillTarget):
    candidates: list[tuple[str, Any]] = []
    if target.selector:
        candidates.append((f"selector:{target.selector}", page.locator(target.selector)))
    candidates.append(
        (
            "label_exact",
            page.get_by_label(target.question_label, exact=True),
        )
    )
    candidates.append(
        (
            "label_regex",
            page.get_by_label(_label_pattern(target.question_label)),
        )
    )
    candidates.append(
        (
            "label_contains",
            page.get_by_label(_label_contains_pattern(target.question_label)),
        )
    )
    candidates.append(
        (
            "role_textbox",
            page.get_by_role("textbox", name=_label_contains_pattern(target.question_label)),
        )
    )
    candidates.append(
        (
            "role_combobox",
            page.get_by_role("combobox", name=_label_contains_pattern(target.question_label)),
        )
    )
    candidates.append(
        (
            "role_checkbox",
            page.get_by_role("checkbox", name=_label_contains_pattern(target.question_label)),
        )
    )
    candidates.append(
        (
            "role_radio",
            page.get_by_role("radio", name=_label_contains_pattern(target.question_label)),
        )
    )

    for source, locator in candidates:
        try:
            if locator.count() == 0:
                continue
            return source, locator
        except Exception:
            continue
    return None, None


def _resolve_option_label(target: GreenhouseFillTarget) -> str:
    if not target.options:
        return target.value
    normalized_answer = _normalized_choice(target.value)
    for option in target.options:
        if _normalized_choice(option.label) == normalized_answer:
            return option.label
        if _normalized_choice(option.value) == normalized_answer:
            return option.label or target.value
    return target.value


def _fill_combobox(page, locator, target: GreenhouseFillTarget, timeout_ms: int) -> str:
    option_label = _resolve_option_label(target)
    locator.click(timeout=timeout_ms)
    try:
        locator.fill(option_label, timeout=timeout_ms)
    except Exception:
        locator.press("Meta+A", timeout=timeout_ms)
        locator.press("Backspace", timeout=timeout_ms)
        locator.type(option_label, delay=20, timeout=timeout_ms)

    option_locator = page.get_by_role("option", name=_label_pattern(option_label)).first
    try:
        if option_locator.count() > 0:
            option_locator.click(timeout=timeout_ms)
            return option_label
    except Exception:
        pass

    locator.press("ArrowDown", timeout=timeout_ms)
    locator.press("Enter", timeout=timeout_ms)
    return option_label


def _truthy_choice_answer(value: str) -> bool | None:
    normalized = _normalized_choice(value)
    if normalized in {
        "yes",
        "true",
        "1",
        "checked",
        "check",
        "agree",
        "agreed",
        "acknowledge",
        "acknowledge confirm",
        "confirm",
        "consent",
        "opt in",
    }:
        return True
    if normalized in {
        "no",
        "false",
        "0",
        "unchecked",
        "uncheck",
        "decline",
        "declined",
        "opt out",
    }:
        return False
    return None


def _split_answer_tokens(value: str) -> list[str]:
    cleaned = value.strip()
    if not cleaned:
        return []
    if any(separator in cleaned for separator in ("\n", ";", ",")):
        tokens = re.split(r"[\n;,]+", cleaned)
        return [token.strip() for token in tokens if token.strip()]
    return [cleaned]


def _resolve_selected_options(target: GreenhouseFillTarget) -> list[tuple[str, str]]:
    if not target.options:
        cleaned = target.value.strip()
        return [(cleaned, cleaned)] if cleaned else []

    tokens = _split_answer_tokens(target.value)
    if not tokens:
        tokens = [target.value]

    selected: list[tuple[str, str]] = []
    for token in tokens:
        normalized_token = _normalized_choice(token)
        for option in target.options:
            if _normalized_choice(option.label) == normalized_token or _normalized_choice(option.value) == normalized_token:
                selected.append((option.label or option.value, option.value))
                break
    if selected:
        deduped: list[tuple[str, str]] = []
        seen: set[tuple[str, str]] = set()
        for item in selected:
            if item in seen:
                continue
            seen.add(item)
            deduped.append(item)
        return deduped

    if len(target.options) == 1 and _truthy_choice_answer(target.value) is True:
        option = target.options[0]
        return [(option.label or option.value, option.value)]
    return []


def _locator_count(locator) -> int:
    try:
        return locator.count()
    except Exception:
        return 0


def _associated_label_text(page, locator) -> str:
    try:
        element_id = locator.get_attribute("id", timeout=500) or ""
    except Exception:
        element_id = ""
    if not element_id:
        return ""
    label = page.locator(f"label[for='{element_id}']").first
    try:
        if label.count() == 0:
            return ""
        return " ".join((label.inner_text(timeout=500) or "").split())
    except Exception:
        return ""


def _resolve_choice_locator(page, locator, target: GreenhouseFillTarget, option_value: str, option_label: str):
    total = _locator_count(locator)
    normalized_value = _normalized_choice(option_value)
    normalized_label = _normalized_choice(option_label)
    if total == 1:
        return locator.first
    for idx in range(total):
        choice = locator.nth(idx)
        try:
            raw_value = choice.get_attribute("value", timeout=500) or ""
        except Exception:
            raw_value = ""
        if raw_value and _normalized_choice(raw_value) == normalized_value:
            return choice
        label_text = _associated_label_text(page, choice)
        if label_text and _normalized_choice(label_text) == normalized_label:
            return choice

    label_candidates = (
        page.get_by_label(option_label, exact=True),
        page.get_by_label(_label_pattern(option_label)),
        page.get_by_label(_label_contains_pattern(option_label)),
    )
    for candidate in label_candidates:
        try:
            if candidate.count() == 0:
                continue
            return candidate.first
        except Exception:
            continue

    role_name = _label_contains_pattern(option_label)
    for role in ("checkbox", "radio"):
        candidate = page.get_by_role(role, name=role_name)
        try:
            if candidate.count() == 0:
                continue
            return candidate.first
        except Exception:
            continue
    return None


def _set_choice_state(locator, *, checked: bool, timeout_ms: int) -> None:
    try:
        if checked:
            locator.check(timeout=timeout_ms, force=True)
        else:
            locator.uncheck(timeout=timeout_ms, force=True)
        return
    except Exception:
        pass

    locator.click(timeout=timeout_ms, force=True)
    if not checked:
        try:
            if locator.is_checked(timeout=timeout_ms):
                locator.click(timeout=timeout_ms, force=True)
        except Exception:
            pass


def _fill_choice_target(page, locator, target: GreenhouseFillTarget, timeout_ms: int) -> str:
    if target.ui_type == "checkbox_group":
        selected_options = _resolve_selected_options(target)
        if not selected_options:
            raise RuntimeError("No matching checkbox options were resolved from the approved answer.")
        applied_labels: list[str] = []
        for option_label, option_value in selected_options:
            choice_locator = _resolve_choice_locator(page, locator, target, option_value, option_label)
            if choice_locator is None:
                raise RuntimeError(f"No matching checkbox option found for '{option_label}'.")
            _set_choice_state(choice_locator, checked=True, timeout_ms=timeout_ms)
            applied_labels.append(option_label)
        return ", ".join(applied_labels)

    selected_options = _resolve_selected_options(target)
    choice_locator = None
    verification = "checked"
    if selected_options:
        option_label, option_value = selected_options[0]
        choice_locator = _resolve_choice_locator(page, locator, target, option_value, option_label)
        verification = option_label
    elif _locator_count(locator) > 0:
        choice_locator = locator.first
        truthy = _truthy_choice_answer(target.value)
        if truthy is False and target.ui_type == "checkbox":
            _set_choice_state(choice_locator, checked=False, timeout_ms=timeout_ms)
            return "unchecked"
    if choice_locator is None:
        raise RuntimeError("No matching choice control found.")
    _set_choice_state(choice_locator, checked=True, timeout_ms=timeout_ms)
    return verification


def _collect_browser_validation_errors(page) -> list[str]:
    messages: list[str] = []
    seen: set[str] = set()
    candidates = (
        page.locator("[role='alert']"),
        page.locator("[id$='-error']"),
        page.locator(".error"),
    )
    for locator in candidates:
        try:
            total = min(locator.count(), 40)
        except Exception:
            continue
        for idx in range(total):
            item = locator.nth(idx)
            try:
                if not item.is_visible(timeout=200):
                    continue
                text = " ".join((item.inner_text(timeout=500) or "").split())
            except Exception:
                continue
            if not text or text in seen:
                continue
            seen.add(text)
            messages.append(text[:240])
    return messages


def _dedupe_preserve_order(values: list[str]) -> list[str]:
    seen: set[str] = set()
    unique: list[str] = []
    for value in values:
        if not value or value in seen:
            continue
        seen.add(value)
        unique.append(value)
    return unique


def _context_storage_state_kwargs(storage_state_path: Path | None) -> dict[str, Any]:
    if storage_state_path is None or not storage_state_path.exists():
        return {}
    return {"storage_state": str(storage_state_path)}


def _should_wait_for_manual_review(*, headless: bool, keep_open_for_review: bool, submitted: bool) -> bool:
    return bool(keep_open_for_review and not headless and not submitted)


def _find_submit_control(page):
    candidates: list[tuple[str, Any]] = [
        (
            "role_submit_application",
            page.get_by_role("button", name=re.compile(r"submit application", re.IGNORECASE)).first,
        ),
        (
            "role_submit",
            page.get_by_role("button", name=re.compile(r"^submit$", re.IGNORECASE)).first,
        ),
        (
            "css_submit_button",
            page.locator(
                "form#application-form button[type='submit'], .application--form button[type='submit']"
            ).first,
        ),
        (
            "css_submit_input",
            page.locator(
                "form#application-form input[type='submit'], .application--form input[type='submit']"
            ).first,
        ),
    ]
    for source, locator in candidates:
        try:
            if locator.count() == 0:
                continue
            if locator.is_visible(timeout=500):
                return source, locator
        except Exception:
            continue
    return None, None


def _locator_label(locator) -> str | None:
    try:
        for attribute in ("aria-label", "value", "title"):
            value = (locator.get_attribute(attribute, timeout=500) or "").strip()
            if value:
                return value[:120]
    except Exception:
        pass
    try:
        text = " ".join((locator.inner_text(timeout=500) or "").split())
        return text[:120] or None
    except Exception:
        return None


def _locator_is_enabled(locator) -> bool:
    try:
        if locator.is_disabled(timeout=500):
            return False
    except Exception:
        return False
    try:
        aria_disabled = (locator.get_attribute("aria-disabled", timeout=500) or "").strip().lower()
        if aria_disabled == "true":
            return False
    except Exception:
        pass
    return True


def _collect_invalid_field_names(page) -> list[str]:
    try:
        names = page.evaluate(
            """() => {
                const visible = (element) => Boolean(
                    element &&
                    (element.offsetWidth || element.offsetHeight || element.getClientRects().length)
                );
                const forms = Array.from(document.querySelectorAll("form"));
                const root =
                    forms.find((form) => {
                        if (!visible(form)) {
                            return false;
                        }
                        const text = (form.innerText || "").toLowerCase();
                        return text.includes("submit application") || Boolean(form.querySelector("input[type='file']"));
                    }) ||
                    document.querySelector("form#application-form, .application--form") ||
                    document;
                const fields = Array.from(root.querySelectorAll("input, textarea, select"));
                const describe = (field) => {
                    if (field.labels && field.labels.length > 0) {
                        return field.labels[0].innerText.trim();
                    }
                    const label = field.closest("label");
                    if (label && label.innerText) {
                        return label.innerText.trim();
                    }
                    return field.getAttribute("aria-label") || field.name || field.id || field.type || "unknown field";
                };
                return fields
                    .filter((field) => typeof field.checkValidity === "function" && !field.checkValidity())
                    .map((field) => describe(field))
                    .filter(Boolean)
                    .slice(0, 20);
            }"""
        )
    except Exception:
        return []
    if not isinstance(names, list):
        return []
    messages = [f"Invalid or incomplete field: {' '.join(str(name).split())[:160]}" for name in names]
    return _dedupe_preserve_order(messages)


def _collect_challenge_blockers(page) -> list[str]:
    blockers: list[str] = []
    challenge_selectors = (
        ("iframe[title*='reCAPTCHA']", "Visible CAPTCHA challenge detected."),
        (".g-recaptcha", "Visible CAPTCHA challenge detected."),
        ("[data-testid*='captcha']", "Visible CAPTCHA challenge detected."),
    )
    for selector, message in challenge_selectors:
        locator = page.locator(selector).first
        try:
            if locator.count() > 0 and locator.is_visible(timeout=300):
                blockers.append(message)
        except Exception:
            continue

    verification_copy = (
        page.get_by_text(re.compile(r"verify you are human", re.IGNORECASE)).first,
        page.get_by_text(re.compile(r"verification code", re.IGNORECASE)).first,
        page.get_by_text(re.compile(r"check your email.*code", re.IGNORECASE)).first,
    )
    for locator in verification_copy:
        try:
            if locator.count() > 0 and locator.is_visible(timeout=300):
                blockers.append("Visible verification challenge detected.")
                break
        except Exception:
            continue
    return _dedupe_preserve_order(blockers)


def _safe_fill_target_groups(targets: list[GreenhouseFillTarget]) -> list[list[GreenhouseFillTarget]]:
    group_order = {
        "questions": 0,
        "location_questions": 1,
        "compliance": 2,
        "demographic_questions": 3,
        "data_compliance": 4,
    }
    ordered = sorted(
        targets,
        key=lambda target: (
            group_order.get(str(target.question_group or "").strip(), 99),
            target.ui_type == "file_upload",
            str(target.question_label or "").strip().lower(),
            str(target.api_name or "").strip().lower(),
        ),
    )
    groups: list[list[GreenhouseFillTarget]] = []
    current_group: str | None = None
    for target in ordered:
        question_group = str(target.question_group or "questions").strip() or "questions"
        if current_group != question_group:
            groups.append([])
            current_group = question_group
        groups[-1].append(target)
    return groups


def _apply_fill_targets(
    page,
    targets: list[GreenhouseFillTarget],
    *,
    timeout_ms: int,
    safe_interaction_mode: bool,
    stop_on_challenge: bool,
) -> tuple[list[FillExecution], list[FillExecution], list[str]]:
    filled: list[FillExecution] = []
    skipped: list[FillExecution] = []
    challenge_blockers: list[str] = []
    target_groups = _safe_fill_target_groups(targets) if safe_interaction_mode else [targets]

    for target_group in target_groups:
        for target in target_group:
            execution = _fill_target(page, target, timeout_ms)
            if execution.success:
                filled.append(execution)
            else:
                skipped.append(execution)
            if safe_interaction_mode:
                try:
                    page.wait_for_timeout(120)
                except Exception:
                    pass
            challenge_blockers = _collect_challenge_blockers(page)
            if challenge_blockers and stop_on_challenge:
                return filled, skipped, challenge_blockers
        if safe_interaction_mode:
            try:
                page.wait_for_timeout(180)
            except Exception:
                pass
        challenge_blockers = _collect_challenge_blockers(page)
        if challenge_blockers and stop_on_challenge:
            return filled, skipped, challenge_blockers

    return filled, skipped, challenge_blockers


def _evaluate_confirmation_signals(
    *,
    current_url: str,
    previous_url: str,
    title: str,
    page_text: str,
    form_visible: bool,
) -> tuple[bool, str | None]:
    combined = " ".join(part for part in [title, page_text] if part).strip()
    if re.search(r"/(?:confirmation|thanks|thank-you|submitted)\b", current_url, re.IGNORECASE):
        return True, "Confirmation URL detected after submit."
    if re.search(
        r"\b(?:application submitted|thank you for applying|we(?:'|’)ve received your application|your application has been submitted)\b",
        combined,
        re.IGNORECASE,
    ):
        return True, "Confirmation text detected after submit."
    if current_url != previous_url and not form_visible and re.search(
        r"\b(?:thank you|submitted|received)\b",
        combined,
        re.IGNORECASE,
    ):
        return True, "Navigation away from form with confirmation copy detected."
    return False, None


def _detect_confirmation(page, *, previous_url: str, timeout_ms: int) -> tuple[bool, str | None]:
    try:
        page.wait_for_load_state("domcontentloaded", timeout=min(timeout_ms, 5000))
    except Exception:
        pass
    try:
        page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 8000))
    except Exception:
        pass
    try:
        page.wait_for_timeout(1500)
    except Exception:
        pass
    try:
        title = page.title()
    except Exception:
        title = ""
    try:
        page_text = " ".join((page.locator("body").inner_text(timeout=1000) or "").split())[:4000]
    except Exception:
        page_text = ""
    try:
        form_visible = page.locator("form#application-form, .application--form").first.is_visible(timeout=500)
    except Exception:
        form_visible = False
    return _evaluate_confirmation_signals(
        current_url=page.url,
        previous_url=previous_url,
        title=title,
        page_text=page_text,
        form_visible=form_visible,
    )


def _live_confirmation_state(page, *, previous_url: str, last_page_url: str) -> tuple[bool, str | None, str]:
    current_url = last_page_url
    try:
        current_url = page.url
    except Exception:
        pass
    try:
        title = page.title()
    except Exception:
        title = ""
    try:
        page_text = " ".join((page.locator("body").inner_text(timeout=500) or "").split())[:3000]
    except Exception:
        page_text = ""
    try:
        form_visible = page.locator("form#application-form, .application--form").first.is_visible(timeout=300)
    except Exception:
        form_visible = False
    confirmed, note = _evaluate_confirmation_signals(
        current_url=current_url,
        previous_url=previous_url,
        title=title,
        page_text=page_text,
        form_visible=form_visible,
    )
    return confirmed, note, current_url


def _wait_for_manual_review_completion(
    page,
    *,
    previous_url: str,
    poll_interval_seconds: float = 1.0,
) -> dict[str, Any]:
    last_page_url = previous_url
    observed_events: list[dict[str, Any]] = []
    while True:
        observed_events.extend(drain_greenhouse_page_observer(page))
        try:
            if page.is_closed():
                return {
                    "ended_reason": "page_closed",
                    "confirmation_detected": False,
                    "confirmation_note": None,
                    "final_page_url": last_page_url,
                    "observed_events": observed_events,
                }
        except Exception:
            return {
                "ended_reason": "page_closed",
                "confirmation_detected": False,
                "confirmation_note": None,
                "final_page_url": last_page_url,
                "observed_events": observed_events,
            }

        confirmed, note, last_page_url = _live_confirmation_state(
            page,
            previous_url=previous_url,
            last_page_url=last_page_url,
        )
        if confirmed:
            observed_events.extend(drain_greenhouse_page_observer(page))
            return {
                "ended_reason": note or "confirmation_detected",
                "confirmation_detected": True,
                "confirmation_note": note,
                "final_page_url": last_page_url,
                "observed_events": observed_events,
            }
        try:
            page.wait_for_timeout(int(max(0.25, poll_interval_seconds) * 1000))
        except Exception:
            return {
                "ended_reason": "page_closed",
                "confirmation_detected": False,
                "confirmation_note": None,
                "final_page_url": last_page_url,
                "observed_events": observed_events,
            }


def _build_submit_blockers(
    submit_safety: dict[str, Any] | None,
    *,
    duplicate_record: dict[str, Any] | None,
    challenge_blockers: list[str],
    submit_button_found: bool,
    submit_button_enabled: bool,
) -> list[str]:
    blockers: list[str] = []
    if duplicate_record:
        submitted_at = str(duplicate_record.get("submitted_at") or "").strip()
        if submitted_at:
            blockers.append(f"Application was already submitted on {submitted_at}.")
        else:
            blockers.append("Application was already submitted.")

    eligible_after_browser_validation = bool(
        submit_safety and submit_safety.get("eligible_after_browser_validation")
    )
    if not eligible_after_browser_validation:
        if submit_safety:
            blockers.extend(str(item) for item in (submit_safety.get("blockers") or []) if item)
            blockers.extend(str(item) for item in (submit_safety.get("browser_blockers") or []) if item)
        if not blockers:
            blockers.append("Application is not eligible for autonomous submission.")

    if not submit_button_found:
        blockers.append("Submit button not found.")
    elif not submit_button_enabled:
        blockers.append("Submit button is disabled.")

    blockers.extend(challenge_blockers)
    return _dedupe_preserve_order(blockers)


def _fill_target(page, target: GreenhouseFillTarget, timeout_ms: int) -> FillExecution:
    source, locator = _resolve_locator(page, target)
    execution = FillExecution(
        question_label=target.question_label,
        api_name=target.api_name,
        ui_type=target.ui_type,
        profile_key=target.profile_key,
        value_source=target.value_source,
        selector=target.selector,
        success=False,
        value_preview=_preview_value(target.profile_key, target.value),
        bound_via=source,
    )
    if locator is None:
        execution.error = "No matching DOM element found."
        return execution

    try:
        primary_locator = locator.first
        if target.ui_type in {"text_input", "textarea"}:
            primary_locator.fill(target.value, timeout=timeout_ms)
            execution.verification = primary_locator.input_value(timeout=timeout_ms)[:100]
        elif target.ui_type == "combobox":
            execution.verification = _fill_combobox(page, primary_locator, target, timeout_ms)
        elif target.ui_type == "file_upload":
            file_path = Path(target.value)
            if not file_path.exists():
                execution.error = f"File does not exist: {file_path}"
                return execution
            primary_locator.set_input_files(str(file_path), timeout=timeout_ms)
            execution.verification = file_path.name
        elif target.ui_type in {"checkbox", "checkbox_group", "radio"}:
            execution.verification = _fill_choice_target(page, locator, target, timeout_ms)
        else:
            execution.error = f"Unsupported ui_type for autofill: {target.ui_type}"
            return execution
        execution.success = True
        return execution
    except Exception as exc:  # noqa: BLE001
        execution.error = str(exc)
        return execution


def execute_greenhouse_autofill(
    url: str,
    *,
    profile: dict[str, Any],
    approved_answers: dict[str, Any] | None = None,
    headless: bool = False,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
    screenshot_path: str | None = None,
    allow_submit: bool = False,
    keep_open_for_review: bool = True,
    storage_state_file: str = DEFAULT_GREENHOUSE_STORAGE_STATE_FILE,
    submission_state_file: str = DEFAULT_GREENHOUSE_SUBMISSION_STATE_FILE,
    store_traces: bool = True,
    trace_file: str = DEFAULT_GREENHOUSE_TRACE_FILE,
    training_examples_file: str = DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
    manual_session_file: str = DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    manual_field_events_file: str = DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
    sync_jobs_db: bool = True,
    jobs_db_file: str = DEFAULT_JOBS_DB_FILE,
    reuse_browser_session: bool = False,
    safe_interaction_mode: bool = True,
) -> PlaywrightAutofillResult:
    analysis: GreenhouseApplicationAnalysis = inspect_greenhouse_application(
        url,
        profile=profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    autofill_targets = build_autofill_targets(analysis, profile=profile)
    approved_answer_targets = build_approved_answer_targets(
        analysis,
        approved_answers=approved_answers,
        profile=profile,
    )
    suggested_answer_targets = build_suggested_answer_targets(analysis)
    interactive_suggested_targets: list[GreenhouseFillTarget] = []
    if not headless and keep_open_for_review:
        interactive_suggested_targets = build_interactive_suggested_answer_targets(
            analysis,
            profile=profile,
            approved_answers=approved_answers,
        )
    targets = _dedupe_fill_targets(
        list(autofill_targets)
        + list(approved_answer_targets)
        + list(suggested_answer_targets)
        + list(interactive_suggested_targets)
    )
    resume_selection = _resume_selection_payload(profile, targets)
    timeout_ms = max(1000, timeout_seconds * 1000)
    application_key = build_greenhouse_application_key(
        board_token=analysis.schema.detection.board_token,
        job_id=analysis.schema.detection.job_id,
        public_url=analysis.schema.public_url or url,
    )
    submission_state = None
    submission_state_path = Path(submission_state_file)
    storage_state_path = prefer_legacy_or_local(
        "greenhouse_browser_storage_state.json",
        ".state/greenhouse_browser_storage_state.json",
    )
    configured_storage_state = Path(storage_state_file).expanduser()
    if configured_storage_state.is_absolute():
        storage_state_path = configured_storage_state
    elif str(configured_storage_state).strip() and configured_storage_state.name != "greenhouse_browser_storage_state.json":
        storage_state_path = (Path.cwd() / configured_storage_state).resolve(strict=False)
    existing_record = None
    manual_review_events: list[dict[str, Any]] = []
    submission = SubmissionAttempt(
        requested=allow_submit,
        application_key=application_key,
        state_file=str(submission_state_path),
    )
    if allow_submit:
        submission_state = load_greenhouse_submission_state(submission_state_path)
        existing_record = get_submission_record(submission_state, application_key=application_key)
        if existing_record:
            submission.duplicate_detected = True
            submission.existing_record = existing_record

    job_blockers = list((analysis.submit_safety.job_blockers if analysis.submit_safety else []) or [])
    if job_blockers:
        submission.blockers.extend(job_blockers)
        blocked_submit_safety = asdict(analysis.submit_safety) if analysis.submit_safety else {
            "eligible": False,
            "blockers": list(job_blockers),
            "requires_browser_validation": True,
            "job_blockers": list(job_blockers),
        }
        blocked_submit_safety["browser_blockers"] = list(job_blockers)
        blocked_submit_safety["eligible_after_browser_validation"] = False
        blocked_submit_safety["challenge_blockers"] = []
        blocked_submit_safety["challenge_detected"] = False
        return PlaywrightAutofillResult(
            analysis=analysis.to_dict(),
            page_url=url,
            form_visible=False,
            errors=list(job_blockers),
            submit_safety=blocked_submit_safety,
            submission=asdict(submission),
            resume_selection=resume_selection,
        )

    try:
        from playwright.sync_api import sync_playwright
    except Exception as exc:  # noqa: BLE001
        raise CrawlFetchError(
            "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium"
        ) from exc

    shared_session = None
    context = None
    browser = None
    page = None
    pw_manager = None
    last_page_url = url
    waited_for_manual_review = False
    try:
        if reuse_browser_session:
            shared_session, page = _new_shared_page(
                headless=headless,
                storage_state_path=storage_state_path,
            )
        else:
            pw_manager = sync_playwright()
            pw = pw_manager.start()
            browser = pw.chromium.launch(**_browser_launch_kwargs(headless=headless))
            context = browser.new_context(**_context_storage_state_kwargs(storage_state_path))
            page = context.new_page()

        page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
        try:
            page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 8000))
        except Exception:
            pass

        form_visible = _ensure_form_visible(page, timeout_ms)
        last_page_url = page.url
        result = PlaywrightAutofillResult(
            analysis=analysis.to_dict(),
            page_url=page.url,
            form_visible=form_visible,
            submit_safety=asdict(analysis.submit_safety) if analysis.submit_safety else None,
            submission=asdict(submission),
            resume_selection=resume_selection,
        )
        if not form_visible:
            result.errors.append("Application form was not visible.")
            if result.submit_safety is None:
                result.submit_safety = {
                    "eligible": False,
                    "blockers": ["Application form was not visible."],
                    "requires_browser_validation": True,
                    "eligible_after_browser_validation": False,
                    "browser_blockers": ["Application form was not visible."],
                }
            else:
                blockers = list(result.submit_safety.get("blockers") or [])
                blockers.append("Application form was not visible.")
                result.submit_safety["blockers"] = blockers
                result.submit_safety["browser_blockers"] = ["Application form was not visible."]
                result.submit_safety["eligible_after_browser_validation"] = False
        else:
            filled, skipped, challenge_blockers = _apply_fill_targets(
                page,
                targets,
                timeout_ms=timeout_ms,
                safe_interaction_mode=safe_interaction_mode,
                stop_on_challenge=bool(headless or not keep_open_for_review),
            )
            result.filled.extend(filled)
            result.skipped.extend(skipped)
            result.browser_validation_errors = _collect_browser_validation_errors(page)
            blockers: list[str] = list(result.browser_validation_errors)
            blockers.extend(challenge_blockers)
            blockers.extend(
                f"{item.question_label}: {item.error or 'Failed to apply value.'}"
                for item in result.skipped
            )
            if challenge_blockers:
                result.errors.append("Automation paused because a verification challenge was detected.")
            if result.submit_safety is None:
                result.submit_safety = {
                    "eligible": False,
                    "blockers": blockers,
                    "requires_browser_validation": True,
                    "browser_blockers": blockers,
                    "eligible_after_browser_validation": False,
                    "challenge_blockers": challenge_blockers,
                    "challenge_detected": bool(challenge_blockers),
                }
            else:
                logical_blockers = list(result.submit_safety.get("blockers") or [])
                combined = logical_blockers + blockers
                result.submit_safety["browser_blockers"] = blockers
                result.submit_safety["eligible_after_browser_validation"] = (
                    bool(result.submit_safety.get("eligible")) and not blockers
                )
                result.submit_safety["blockers"] = combined
                result.submit_safety["challenge_blockers"] = challenge_blockers
                result.submit_safety["challenge_detected"] = bool(challenge_blockers)

        if allow_submit:
            submission_payload = result.submission or {}
            _submit_source, submit_locator = _find_submit_control(page)
            submit_button_found = submit_locator is not None
            submit_button_enabled = _locator_is_enabled(submit_locator) if submit_locator else False
            challenge_blockers = _collect_challenge_blockers(page)
            challenge_blockers.extend(_collect_invalid_field_names(page))
            submit_blockers = _build_submit_blockers(
                result.submit_safety,
                duplicate_record=existing_record,
                challenge_blockers=challenge_blockers,
                submit_button_found=submit_button_found,
                submit_button_enabled=submit_button_enabled,
            )
            submission_payload["submit_button_found"] = submit_button_found
            submission_payload["submit_button_enabled"] = (
                submit_button_enabled if submit_button_found else None
            )
            submission_payload["submit_button_label"] = (
                _locator_label(submit_locator) if submit_locator else None
            )
            submission_payload["blockers"] = submit_blockers

            if submit_locator and not submit_blockers:
                submission_payload["attempted"] = True
                submit_locator.click(timeout=timeout_ms)
                confirmed, confirmation_note = _detect_confirmation(
                    page,
                    previous_url=result.page_url,
                    timeout_ms=timeout_ms,
                )
                last_page_url = page.url
                submission_payload["confirmation_detected"] = confirmed
                submission_payload["confirmation_note"] = confirmation_note
                submission_payload["confirmation_url"] = page.url
                if confirmed:
                    submission_payload["submitted"] = True
                    if submission_state is not None:
                        saved_record = record_submission(
                            submission_state,
                            application_key=application_key,
                            board_token=analysis.schema.detection.board_token,
                            job_id=analysis.schema.detection.job_id,
                            company_name=analysis.schema.company_name,
                            title=analysis.schema.title,
                            public_url=analysis.schema.public_url or url,
                            confirmation_url=page.url,
                            confirmation_note=confirmation_note,
                            page_url=page.url,
                        )
                        save_greenhouse_submission_state(submission_state_path, submission_state)
                        submission_payload["persisted"] = True
                        submission_payload["saved_record"] = saved_record
                else:
                    post_submit_blockers = _collect_browser_validation_errors(page)
                    post_submit_blockers.extend(_collect_challenge_blockers(page))
                    post_submit_blockers.append("Submit click completed but no confirmation page was detected.")
                    submission_payload["blockers"] = _dedupe_preserve_order(
                        list(submission_payload.get("blockers") or []) + post_submit_blockers
                    )
                    result.browser_validation_errors = _dedupe_preserve_order(
                        list(result.browser_validation_errors) + post_submit_blockers
                    )
            result.submission = submission_payload

        if _should_wait_for_manual_review(
            headless=headless,
            keep_open_for_review=keep_open_for_review,
            submitted=bool((result.submission or {}).get("submitted")),
        ):
            waited_for_manual_review = True
            install_greenhouse_page_observer(page)
            manual_review = _wait_for_manual_review_completion(
                page,
                previous_url=analysis.schema.public_url or url,
            )
            manual_review_events = [
                event for event in (manual_review.get("observed_events") or []) if isinstance(event, dict)
            ]
            last_page_url = str(manual_review.get("final_page_url") or last_page_url).strip() or last_page_url
            submission_payload = result.submission or {}
            submission_payload["manual_review_waited"] = True
            submission_payload["manual_review_ended_reason"] = manual_review.get("ended_reason")
            submission_payload["manual_review_event_count"] = len(manual_review_events)
            if manual_review.get("confirmation_detected"):
                confirmation_note = str(manual_review.get("confirmation_note") or "").strip() or None
                submission_payload["attempted"] = True
                submission_payload["submitted"] = True
                submission_payload["confirmation_detected"] = True
                submission_payload["confirmation_note"] = confirmation_note
                submission_payload["confirmation_url"] = last_page_url
                if submission_state is None:
                    submission_state = load_greenhouse_submission_state(submission_state_path)
                saved_record = record_submission(
                    submission_state,
                    application_key=application_key,
                    board_token=analysis.schema.detection.board_token,
                    job_id=analysis.schema.detection.job_id,
                    company_name=analysis.schema.company_name,
                    title=analysis.schema.title,
                    public_url=analysis.schema.public_url or url,
                    confirmation_url=last_page_url,
                    confirmation_note=confirmation_note,
                    page_url=last_page_url,
                )
                save_greenhouse_submission_state(submission_state_path, submission_state)
                submission_payload["persisted"] = True
                submission_payload["saved_record"] = saved_record
            result.submission = submission_payload

        if screenshot_path and not page.is_closed():
            page.screenshot(path=screenshot_path, full_page=True)
        result.page_url = last_page_url
    finally:
        if reuse_browser_session:
            if shared_session is not None:
                try:
                    shared_session.persist_storage_state()
                except Exception:
                    pass
            if page is not None and not waited_for_manual_review:
                try:
                    if not page.is_closed():
                        page.close()
                except Exception:
                    pass
        else:
            try:
                storage_state_path.parent.mkdir(parents=True, exist_ok=True)
                if context is not None:
                    context.storage_state(path=str(storage_state_path))
            except Exception:
                pass
            try:
                if context is not None:
                    context.close()
            except Exception:
                pass
            try:
                if browser is not None:
                    browser.close()
            except Exception:
                pass
            try:
                if pw_manager is not None:
                    pw_manager.stop()
            except Exception:
                pass
    if store_traces and manual_review_events:
        try:
            result.manual_observation = append_greenhouse_manual_observation(
                analysis=analysis,
                observed_events=manual_review_events,
                profile=profile,
                fill_targets=targets,
                application_key=application_key,
                requested_url=url,
                final_page_url=last_page_url,
                confirmation_detected=bool((result.submission or {}).get("confirmation_detected")),
                ended_reason=str((result.submission or {}).get("manual_review_ended_reason") or "").strip() or None,
                resume_decision_source="assistant_review",
                external_resume_recommendation=None,
                session_file=manual_session_file,
                field_events_file=manual_field_events_file,
            )
        except Exception as exc:  # noqa: BLE001
            result.errors.append(f"Manual observation persistence failed: {exc}")
    if store_traces:
        try:
            result.trace = append_greenhouse_trace(
                analysis=analysis,
                result=result.to_dict(),
                profile=profile,
                approved_answers=approved_answers,
                fill_targets=targets,
                application_key=application_key,
                requested_url=url,
                trace_file=trace_file,
                training_examples_file=training_examples_file,
            )
        except Exception as exc:  # noqa: BLE001
            result.errors.append(f"Trace persistence failed: {exc}")
    try:
        pending_review_count = sum(
            1 for item in analysis.review_queue if item.status in {"pending", "blocked"}
        )
        submitted = bool((result.submission or {}).get("submitted"))
        eligible_after_browser_validation = bool(
            (result.submit_safety or {}).get("eligible_after_browser_validation")
        )
        if submitted:
            sync_outcome = "submitted"
        elif eligible_after_browser_validation:
            sync_outcome = "eligible"
        elif pending_review_count > 0:
            sync_outcome = "review_required"
        else:
            sync_outcome = "not_ready"
        result.jobs_db_sync = sync_greenhouse_job_record(
            requested_url=url,
            public_url=analysis.schema.public_url,
            source="playwright_run",
            outcome=sync_outcome,
            application_status="applied" if submitted else None,
            trace_id=str((result.trace or {}).get("trace_id") or "").strip() or None,
            manual_session_id=str((result.manual_observation or {}).get("manual_session_id") or "").strip() or None,
            auto_submit_eligible=bool(analysis.auto_submit_eligible),
            review_pending_count=pending_review_count,
            confirmation_detected=bool((result.submission or {}).get("confirmation_detected")),
            jobs_db_file=jobs_db_file,
            enabled=sync_jobs_db,
        )
    except Exception as exc:  # noqa: BLE001
        result.errors.append(f"jobs.db sync failed: {exc}")
        result.jobs_db_sync = {"stored": False, "reason": f"sync_failed: {exc}"}
    return result


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fill safe Greenhouse application fields via Playwright, with optional autonomous submit."
    )
    parser.add_argument("url", help="Greenhouse hosted job URL.")
    parser.add_argument(
        "--profile-file",
        required=True,
        help="Path to JSON profile file.",
    )
    parser.add_argument(
        "--answers-file",
        help="Optional JSON object with approved answers keyed by question label or api name.",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"Timeout for HTTP and page operations. Default: {DEFAULT_TIMEOUT_SECONDS}.",
    )
    parser.add_argument(
        "--headless",
        action=argparse.BooleanOptionalAction,
        default=False,
        help="Run Chromium headless. Default is headed so review and queue items stay visible.",
    )
    parser.add_argument(
        "--screenshot",
        help="Optional output path for a post-fill screenshot.",
    )
    parser.add_argument(
        "--allow-submit",
        action="store_true",
        help="Actually click submit when the application is fully safe and no duplicate record exists.",
    )
    parser.add_argument(
        "--keep-open-for-review",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Keep headed runs open for manual review until you submit or close the window.",
    )
    parser.add_argument(
        "--storage-state-file",
        default=DEFAULT_GREENHOUSE_STORAGE_STATE_FILE,
        help="Path to persisted Playwright browser storage used to reuse login state across runs.",
    )
    parser.add_argument(
        "--submission-state-file",
        default=DEFAULT_GREENHOUSE_SUBMISSION_STATE_FILE,
        help="Path to persistent submission-state JSON used for duplicate prevention.",
    )
    parser.add_argument(
        "--store-traces",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Persist append-only run traces and question-level training examples.",
    )
    parser.add_argument(
        "--trace-file",
        default=DEFAULT_GREENHOUSE_TRACE_FILE,
        help="Path to append-only JSONL run traces.",
    )
    parser.add_argument(
        "--training-examples-file",
        default=DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
        help="Path to append-only JSONL question-level training examples.",
    )
    parser.add_argument(
        "--sync-jobs-db",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Sync assistant outcomes back into jobs.db.",
    )
    parser.add_argument(
        "--jobs-db-file",
        default=DEFAULT_JOBS_DB_FILE,
        help="Path to jobs.db for syncing application activity.",
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
        profile = _load_json_object_file(args.profile_file, required_label="profile")
        approved_answers = (
            _load_json_object_file(args.answers_file, required_label="approved answers")
            if args.answers_file
            else None
        )
        result = execute_greenhouse_autofill(
            args.url,
            profile=profile,
            approved_answers=approved_answers,
            headless=bool(args.headless),
            timeout_seconds=args.timeout_seconds,
            screenshot_path=args.screenshot,
            allow_submit=bool(args.allow_submit),
            keep_open_for_review=bool(args.keep_open_for_review),
            storage_state_file=args.storage_state_file,
            submission_state_file=args.submission_state_file,
            store_traces=bool(args.store_traces),
            trace_file=args.trace_file,
            training_examples_file=args.training_examples_file,
            sync_jobs_db=bool(args.sync_jobs_db),
            jobs_db_file=args.jobs_db_file,
        )
    except Exception as exc:  # noqa: BLE001
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result.to_dict(), indent=args.indent, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
