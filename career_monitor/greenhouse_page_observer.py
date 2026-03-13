from __future__ import annotations

from typing import Any


GREENHOUSE_PAGE_OBSERVER_SCRIPT = """
(() => {
  if (window.__ghManualObserver && window.__ghManualObserver.flush) {
    return;
  }
  const sanitizeText = (value) => String(value || "").replace(/\\s+/g, " ").trim();
  const labelFor = (el) => {
    if (el.labels && el.labels.length > 0) {
      return sanitizeText(el.labels[0].innerText);
    }
    const closestLabel = el.closest("label");
    if (closestLabel) {
      const text = sanitizeText(closestLabel.innerText);
      if (text) {
        return text;
      }
    }
    const fieldRoot = el.closest(".field, .application-field, .question, li, div");
    if (fieldRoot) {
      const label = fieldRoot.querySelector("label, legend");
      if (label) {
        const text = sanitizeText(label.innerText);
        if (text) {
          return text;
        }
      }
    }
    return sanitizeText(el.getAttribute("aria-label") || el.name || el.id || "");
  };
  window.__ghManualObserver = {
    seq: 0,
    startedAt: Date.now(),
    events: [],
    flush() {
      const out = this.events.slice();
      this.events = [];
      return out;
    },
  };
  const pushEvent = (eventType, el) => {
    if (!el || !el.tagName) {
      return;
    }
    const tagName = String(el.tagName || "").toLowerCase();
    if (!["input", "textarea", "select"].includes(tagName)) {
      return;
    }
    const inputType = String(el.type || "").toLowerCase();
    const fileNames = inputType === "file" ? Array.from(el.files || []).map((file) => file.name) : [];
    const value = inputType === "file" ? "" : String(el.value || "");
    window.__ghManualObserver.events.push({
      sequence: ++window.__ghManualObserver.seq,
      offset_ms: Date.now() - window.__ghManualObserver.startedAt,
      event_type: eventType,
      page_url: window.location.href,
      question_label: labelFor(el),
      api_name: String(el.name || ""),
      element_id: String(el.id || ""),
      element_name: String(el.name || ""),
      tag_name: tagName,
      input_type: inputType,
      required: !!el.required,
      value: value,
      checked: typeof el.checked === "boolean" ? !!el.checked : null,
      file_names: fileNames,
    });
  };
  document.addEventListener("focusin", (event) => pushEvent("focus", event.target), true);
  document.addEventListener("change", (event) => pushEvent("change", event.target), true);
  document.addEventListener("focusout", (event) => pushEvent("blur", event.target), true);
  document.addEventListener("click", (event) => {
    const el = event.target && event.target.closest ? event.target.closest("button, input[type='submit']") : null;
    if (!el) {
      return;
    }
    const label = sanitizeText(el.innerText || el.value || el.getAttribute("aria-label") || "");
    if (!label) {
      return;
    }
    window.__ghManualObserver.events.push({
      sequence: ++window.__ghManualObserver.seq,
      offset_ms: Date.now() - window.__ghManualObserver.startedAt,
      event_type: "submit_click",
      page_url: window.location.href,
      question_label: label,
      api_name: "",
      element_id: String(el.id || ""),
      element_name: String(el.name || ""),
      tag_name: String(el.tagName || "").toLowerCase(),
      input_type: String(el.type || "").toLowerCase(),
      required: false,
      value: "",
      checked: null,
      file_names: [],
    });
  }, true);
})();
"""


def install_greenhouse_page_observer(page) -> None:
    page.add_init_script(script=GREENHOUSE_PAGE_OBSERVER_SCRIPT)
    try:
        page.evaluate(GREENHOUSE_PAGE_OBSERVER_SCRIPT)
    except Exception:
        pass


def drain_greenhouse_page_observer(page) -> list[dict[str, Any]]:
    try:
        events = page.evaluate(
            """() => {
                if (!window.__ghManualObserver || typeof window.__ghManualObserver.flush !== "function") {
                    return [];
                }
                return window.__ghManualObserver.flush();
            }"""
        )
    except Exception:
        return []
    if not isinstance(events, list):
        return []
    return [event for event in events if isinstance(event, dict)]
