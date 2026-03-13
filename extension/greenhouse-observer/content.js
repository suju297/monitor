const greenhouseObserverState = {
  active: false,
  config: null,
  handlers: null,
  confirmationIntervalId: null,
  confirmationSent: false,
};

function normalizeText(value) {
  return String(value || '').replace(/\s+/g, ' ').trim();
}

function elementRole(element) {
  return normalizeText(element && element.getAttribute ? element.getAttribute('role') : '').toLowerCase();
}

function isNativeField(element) {
  if (!element || !element.tagName) {
    return false;
  }
  const tagName = String(element.tagName || '').toLowerCase();
  return ['input', 'textarea', 'select'].includes(tagName);
}

function isChoiceLikeField(element) {
  if (!element) {
    return false;
  }
  if (isNativeField(element)) {
    const inputType = String(element.type || '').toLowerCase();
    return ['checkbox', 'radio', 'file'].includes(inputType) || String(element.tagName || '').toLowerCase() === 'select';
  }
  const role = elementRole(element);
  return ['combobox', 'option', 'radio', 'checkbox', 'switch'].includes(role);
}

function isObservableField(element) {
  if (!element) {
    return false;
  }
  if (isNativeField(element)) {
    return true;
  }
  const role = elementRole(element);
  if (['combobox', 'option', 'radio', 'checkbox', 'switch'].includes(role)) {
    return true;
  }
  return element.matches
    ? element.matches("[aria-haspopup='listbox'], [aria-expanded][aria-controls]")
    : false;
}

function questionLabelForElement(element) {
  if (!element) {
    return '';
  }
  if (element.labels && element.labels.length > 0) {
    return normalizeText(element.labels[0].innerText);
  }
  const closestLabel = element.closest ? element.closest('label') : null;
  if (closestLabel) {
    const text = normalizeText(closestLabel.innerText);
    if (text) {
      return text;
    }
  }
  const fieldRoot =
    element.closest && element.closest('.field, .application-field, .question, .questionnaire-item, li, div');
  if (fieldRoot) {
    const label = fieldRoot.querySelector('label, legend, h3, h4');
    if (label) {
      const text = normalizeText(label.innerText);
      if (text) {
        return text;
      }
    }
  }
  return normalizeText(
    (element.getAttribute && element.getAttribute('aria-label')) ||
      element.name ||
      element.id ||
      element.innerText ||
      ''
  );
}

function observableValue(element) {
  if (!element) {
    return '';
  }
  if (isNativeField(element)) {
    const inputType = String(element.type || '').toLowerCase();
    if (inputType === 'file') {
      return '';
    }
    return String(element.value || '');
  }
  return normalizeText(
    (element.getAttribute && element.getAttribute('aria-label')) ||
      (element.getAttribute && element.getAttribute('data-value')) ||
      element.innerText ||
      element.textContent ||
      ''
  );
}

function observableChecked(element) {
  if (!element) {
    return null;
  }
  if (typeof element.checked === 'boolean') {
    return !!element.checked;
  }
  const ariaChecked = element.getAttribute ? element.getAttribute('aria-checked') : null;
  if (ariaChecked === 'true') {
    return true;
  }
  if (ariaChecked === 'false') {
    return false;
  }
  return null;
}

function buildFieldEvent(eventType, element) {
  const inputType = String(element && element.type ? element.type : '').toLowerCase();
  return {
    sequence: Date.now(),
    offset_ms: performance.now(),
    event_type: eventType,
    page_url: window.location.href,
    page_title: document.title || '',
    question_label: questionLabelForElement(element),
    api_name: String(element && element.name ? element.name : ''),
    element_id: String(element && element.id ? element.id : ''),
    element_name: String(element && element.name ? element.name : ''),
    tag_name: String(element && element.tagName ? element.tagName : '').toLowerCase(),
    input_type: inputType || elementRole(element) || '',
    element_role: elementRole(element),
    required: !!(element && element.required),
    value: observableValue(element),
    checked: observableChecked(element),
    file_names:
      inputType === 'file'
        ? Array.from((element && element.files) || []).map((file) => file.name)
        : [],
  };
}

function buildPageEvent(eventType, extra = {}) {
  return {
    sequence: Date.now(),
    offset_ms: performance.now(),
    event_type: eventType,
    page_url: window.location.href,
    page_title: document.title || '',
    question_label: '',
    api_name: '',
    element_id: '',
    element_name: '',
    tag_name: 'document',
    input_type: '',
    element_role: '',
    required: false,
    value: '',
    checked: null,
    file_names: [],
    ...extra,
  };
}

function sendObserverEvent(event) {
  chrome.runtime.sendMessage(
    {
      type: 'gh_observer_event',
      event,
      pageUrl: window.location.href,
    },
    () => {
      void chrome.runtime.lastError;
    }
  );
}

function detectConfirmation() {
  const url = window.location.href;
  if (/(?:\/confirmation|\/thanks|\/thank-you|\/submitted)\b/i.test(url)) {
    return true;
  }
  const bodyText = normalizeText(document.body ? document.body.innerText : '').toLowerCase();
  return /application submitted|thank you for applying|we've received your application|your application has been submitted/.test(
    bodyText
  );
}

function stopConfirmationPolling() {
  if (greenhouseObserverState.confirmationIntervalId !== null) {
    window.clearInterval(greenhouseObserverState.confirmationIntervalId);
    greenhouseObserverState.confirmationIntervalId = null;
  }
}

function sendConfirmation() {
  if (greenhouseObserverState.confirmationSent) {
    return;
  }
  greenhouseObserverState.confirmationSent = true;
  chrome.runtime.sendMessage(
    {
      type: 'gh_observer_confirmation',
      finalPageUrl: window.location.href,
    },
    () => {
      void chrome.runtime.lastError;
    }
  );
}

function startConfirmationPolling() {
  stopConfirmationPolling();
  greenhouseObserverState.confirmationIntervalId = window.setInterval(() => {
    if (!greenhouseObserverState.active) {
      stopConfirmationPolling();
      return;
    }
    if (detectConfirmation()) {
      greenhouseObserverState.active = false;
      detachListeners();
      stopConfirmationPolling();
      sendConfirmation();
    }
  }, 1500);
}

function nearestObservableTarget(node) {
  return node && node.closest
    ? node.closest(
        "input, textarea, select, [role='combobox'], [role='option'], [role='radio'], [role='checkbox'], [role='switch'], [aria-haspopup='listbox']"
      )
    : null;
}

function attachListeners() {
  if (greenhouseObserverState.handlers) {
    return;
  }
  const onFocus = (event) => {
    if (!greenhouseObserverState.active || !isObservableField(event.target)) {
      return;
    }
    sendObserverEvent(buildFieldEvent('focus', event.target));
  };
  const onChange = (event) => {
    if (!greenhouseObserverState.active || !isObservableField(event.target)) {
      return;
    }
    sendObserverEvent(buildFieldEvent('change', event.target));
  };
  const onBlur = (event) => {
    if (!greenhouseObserverState.active || !isObservableField(event.target)) {
      return;
    }
    sendObserverEvent(buildFieldEvent('blur', event.target));
  };
  const onClick = (event) => {
    if (!greenhouseObserverState.active) {
      return;
    }
    const button = event.target && event.target.closest ? event.target.closest("button, input[type='submit']") : null;
    if (button) {
      const label = normalizeText(button.innerText || button.value || button.getAttribute('aria-label') || '');
      if (label) {
        sendObserverEvent({
          ...buildPageEvent('submit_click'),
          question_label: label,
          element_id: String(button.id || ''),
          element_name: String(button.name || ''),
          tag_name: String(button.tagName || '').toLowerCase(),
          input_type: String(button.type || '').toLowerCase(),
        });
      }
      return;
    }
    const control = nearestObservableTarget(event.target);
    if (!control || !isChoiceLikeField(control)) {
      return;
    }
    sendObserverEvent(buildFieldEvent('click', control));
  };
  const onVisibilityChange = () => {
    if (!greenhouseObserverState.active) {
      return;
    }
    sendObserverEvent(
      buildPageEvent('visibility_change', {
        value: document.visibilityState || '',
      })
    );
  };
  const onPageHide = () => {
    if (!greenhouseObserverState.active) {
      return;
    }
    sendObserverEvent(buildPageEvent('page_hide'));
  };

  document.addEventListener('focusin', onFocus, true);
  document.addEventListener('change', onChange, true);
  document.addEventListener('focusout', onBlur, true);
  document.addEventListener('click', onClick, true);
  document.addEventListener('visibilitychange', onVisibilityChange, true);
  window.addEventListener('pagehide', onPageHide, true);
  greenhouseObserverState.handlers = {
    onFocus,
    onChange,
    onBlur,
    onClick,
    onVisibilityChange,
    onPageHide,
  };
}

function detachListeners() {
  const handlers = greenhouseObserverState.handlers;
  if (!handlers) {
    return;
  }
  document.removeEventListener('focusin', handlers.onFocus, true);
  document.removeEventListener('change', handlers.onChange, true);
  document.removeEventListener('focusout', handlers.onBlur, true);
  document.removeEventListener('click', handlers.onClick, true);
  document.removeEventListener('visibilitychange', handlers.onVisibilityChange, true);
  window.removeEventListener('pagehide', handlers.onPageHide, true);
  greenhouseObserverState.handlers = null;
}

function activateObserver(config) {
  if (greenhouseObserverState.active) {
    greenhouseObserverState.config = config || greenhouseObserverState.config || null;
    return;
  }
  greenhouseObserverState.active = true;
  greenhouseObserverState.config = config || null;
  greenhouseObserverState.confirmationSent = false;
  attachListeners();
  sendObserverEvent(buildPageEvent('page_observe_start'));
  if (detectConfirmation()) {
    sendConfirmation();
    return;
  }
  startConfirmationPolling();
}

function deactivateObserver() {
  greenhouseObserverState.active = false;
  detachListeners();
  stopConfirmationPolling();
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (!message || !message.type) {
    sendResponse({ ok: false, message: 'Missing message type.' });
    return false;
  }
  if (message.type === 'gh_content_activate') {
    activateObserver(message.config || null);
    sendResponse({ ok: true, active: true, pageUrl: window.location.href });
    return false;
  }
  if (message.type === 'gh_content_deactivate') {
    deactivateObserver();
    sendResponse({ ok: true, active: false, pageUrl: window.location.href });
    return false;
  }
  if (message.type === 'gh_content_status') {
    sendResponse({
      ok: true,
      active: greenhouseObserverState.active,
      pageUrl: window.location.href,
      confirmationDetected: detectConfirmation(),
    });
    return false;
  }
  sendResponse({ ok: false, message: `Unsupported message type: ${message.type}` });
  return false;
});

chrome.runtime.sendMessage(
  {
    type: 'gh_content_ready',
    pageUrl: window.location.href,
    pageTitle: document.title || '',
  },
  (response) => {
    if (chrome.runtime.lastError) {
      return;
    }
    if (response && response.active) {
      activateObserver(response.config || null);
    }
  }
);
