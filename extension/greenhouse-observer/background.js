const DEFAULT_BACKEND_BASE_URL = 'http://127.0.0.1:8776';
const CONFIG_STORAGE_KEY = 'greenhouseObserverConfig';
const SESSION_STORAGE_PREFIX = 'greenhouseObserverSession:';

const DEFAULT_CONFIG = {
  backendBaseUrl: DEFAULT_BACKEND_BASE_URL,
  resumeDecisionSource: 'manual',
  externalResumeRecommendation: '',
  autoObserve: true,
};

function normalizeBaseUrl(value) {
  const raw = String(value || '').trim();
  return (raw || DEFAULT_BACKEND_BASE_URL).replace(/\/+$/, '');
}

function normalizeConfig(value) {
  const raw = value && typeof value === 'object' ? value : {};
  return {
    backendBaseUrl: normalizeBaseUrl(raw.backendBaseUrl),
    resumeDecisionSource: String(raw.resumeDecisionSource || DEFAULT_CONFIG.resumeDecisionSource).trim(),
    externalResumeRecommendation: String(
      raw.externalResumeRecommendation || DEFAULT_CONFIG.externalResumeRecommendation
    ).trim(),
    autoObserve: raw.autoObserve !== false,
  };
}

function isSupportedGreenhouseUrl(url) {
  return /^https:\/\/(?:boards|job-boards)\.greenhouse\.io\//i.test(String(url || ''));
}

function isLikelyConfirmationUrl(url) {
  return /(?:\/confirmation|\/thanks|\/thank-you|\/submitted)\b/i.test(String(url || ''));
}

function sessionStorageKey(tabId) {
  return `${SESSION_STORAGE_PREFIX}${tabId}`;
}

function normalizeSession(value) {
  if (!value || typeof value !== 'object') {
    return null;
  }
  return {
    active: !!value.active,
    startedAt: value.startedAt || null,
    startedReason: value.startedReason || null,
    requestedUrl: String(value.requestedUrl || '').trim() || null,
    finalPageUrl: String(value.finalPageUrl || '').trim() || null,
    pageTitle: String(value.pageTitle || '').trim() || null,
    confirmationDetected: !!value.confirmationDetected,
    endedReason: String(value.endedReason || '').trim() || null,
    config: normalizeConfig(value.config),
    events: Array.isArray(value.events) ? value.events : [],
    lastUpload: value.lastUpload || null,
    recommendation:
      value.recommendation && typeof value.recommendation === 'object'
        ? {
            status: String(value.recommendation.status || 'idle'),
            payload: value.recommendation.payload || null,
            fetchedAt: value.recommendation.fetchedAt || null,
            error: value.recommendation.error || null,
          }
        : {
            status: 'idle',
            payload: null,
            fetchedAt: null,
            error: null,
          },
  };
}

function summarizeSession(session) {
  if (!session) {
    return {
      active: false,
      eventCount: 0,
      startedAt: null,
      startedReason: null,
      requestedUrl: null,
      finalPageUrl: null,
      confirmationDetected: false,
      endedReason: null,
      lastUpload: null,
      recommendationStatus: 'idle',
    };
  }
  return {
    active: !!session.active,
    eventCount: Array.isArray(session.events) ? session.events.length : 0,
    startedAt: session.startedAt,
    startedReason: session.startedReason || null,
    requestedUrl: session.requestedUrl || null,
    finalPageUrl: session.finalPageUrl || null,
    confirmationDetected: !!session.confirmationDetected,
    endedReason: session.endedReason || null,
    lastUpload: session.lastUpload || null,
    recommendationStatus:
      session.recommendation && session.recommendation.status ? session.recommendation.status : 'idle',
  };
}

async function getConfig() {
  const stored = await chrome.storage.local.get(CONFIG_STORAGE_KEY);
  return normalizeConfig(stored[CONFIG_STORAGE_KEY]);
}

async function saveConfig(config) {
  const normalized = normalizeConfig(config);
  await chrome.storage.local.set({
    [CONFIG_STORAGE_KEY]: normalized,
  });
  return normalized;
}

async function getSession(tabId) {
  if (!Number.isInteger(tabId)) {
    return null;
  }
  const key = sessionStorageKey(tabId);
  const stored = await chrome.storage.local.get(key);
  return normalizeSession(stored[key]);
}

async function saveSession(tabId, session) {
  if (!Number.isInteger(tabId) || !session) {
    return null;
  }
  const normalized = normalizeSession(session);
  const key = sessionStorageKey(tabId);
  await chrome.storage.local.set({
    [key]: normalized,
  });
  return normalized;
}

async function clearSession(tabId) {
  if (!Number.isInteger(tabId)) {
    return;
  }
  await chrome.storage.local.remove(sessionStorageKey(tabId));
}

async function fetchJSON(url, init = {}) {
  const response = await fetch(url, init);
  let payload = {};
  try {
    payload = await response.json();
  } catch (_error) {
    payload = {};
  }
  if (!response.ok) {
    throw new Error(payload.message || `Request failed with status ${response.status}`);
  }
  return payload;
}

async function sendMessageToTab(tabId, message) {
  try {
    return await chrome.tabs.sendMessage(tabId, message);
  } catch (_error) {
    return null;
  }
}

function createSession(requestedUrl, config, options = {}) {
  return {
    active: true,
    startedAt: new Date().toISOString(),
    startedReason: String(options.startedReason || '').trim() || 'manual_start',
    requestedUrl,
    finalPageUrl: requestedUrl,
    pageTitle: String(options.pageTitle || '').trim() || null,
    confirmationDetected: false,
    endedReason: null,
    config: normalizeConfig(config),
    events: [],
    lastUpload: null,
    recommendation: {
      status: 'idle',
      payload: null,
      fetchedAt: null,
      error: null,
    },
  };
}

async function updateRecommendation(tabId, status, extra = {}) {
  const session = await getSession(tabId);
  if (!session) {
    return null;
  }
  session.recommendation = {
    status,
    payload: extra.payload || null,
    fetchedAt: extra.fetchedAt || null,
    error: extra.error || null,
  };
  await saveSession(tabId, session);
  return session;
}

async function fetchAndStoreRecommendation(tabId, requestedUrl, config) {
  if (!Number.isInteger(tabId) || !requestedUrl) {
    return null;
  }
  const backendBaseUrl = normalizeBaseUrl(config.backendBaseUrl);
  await updateRecommendation(tabId, 'loading');
  try {
    const endpoint =
      `${backendBaseUrl}/api/greenhouse-observer/recommendation?url=` + encodeURIComponent(requestedUrl);
    const payload = await fetchJSON(endpoint);
    const session = await getSession(tabId);
    if (!session || session.requestedUrl !== requestedUrl) {
      return null;
    }
    session.recommendation = {
      status: 'ok',
      payload,
      fetchedAt: new Date().toISOString(),
      error: null,
    };
    await saveSession(tabId, session);
    return session;
  } catch (error) {
    const session = await getSession(tabId);
    if (!session || session.requestedUrl !== requestedUrl) {
      return null;
    }
    session.recommendation = {
      status: 'error',
      payload: null,
      fetchedAt: new Date().toISOString(),
      error: String(error && error.message ? error.message : error),
    };
    await saveSession(tabId, session);
    return session;
  }
}

async function startSession(tabId, requestedUrl, config, options = {}) {
  const session = createSession(requestedUrl, config, options);
  await saveSession(tabId, session);
  await sendMessageToTab(tabId, {
    type: 'gh_content_activate',
    config: session.config,
  });
  void fetchAndStoreRecommendation(tabId, requestedUrl, session.config);
  return session;
}

async function ingestSession(tabId, overrides = {}) {
  const session = await getSession(tabId);
  if (!session) {
    return {
      ok: false,
      message: 'No active session for this tab.',
    };
  }
  if (!session.active && session.lastUpload) {
    return {
      ok: true,
      message: 'Session already uploaded.',
      summary: summarizeSession(session),
      upload: session.lastUpload,
      recommendation: session.recommendation,
    };
  }

  const backendBaseUrl = normalizeBaseUrl(session.config.backendBaseUrl);
  const payload = {
    url: session.requestedUrl,
    final_page_url: overrides.finalPageUrl || session.finalPageUrl || session.requestedUrl,
    confirmation_detected:
      overrides.confirmationDetected !== undefined
        ? !!overrides.confirmationDetected
        : !!session.confirmationDetected,
    ended_reason: overrides.endedReason || 'manual_stop',
    resume_decision_source: session.config.resumeDecisionSource || null,
    external_resume_recommendation: session.config.externalResumeRecommendation || null,
    events: session.events,
  };
  const upload = await fetchJSON(`${backendBaseUrl}/api/greenhouse-observer/ingest`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  });
  session.active = false;
  session.endedReason = payload.ended_reason;
  session.lastUpload = {
    uploadedAt: new Date().toISOString(),
    payload: upload,
  };
  session.confirmationDetected = !!payload.confirmation_detected;
  session.finalPageUrl = payload.final_page_url;
  await saveSession(tabId, session);
  await sendMessageToTab(tabId, {
    type: 'gh_content_deactivate',
  });
  return {
    ok: true,
    summary: summarizeSession(session),
    upload: session.lastUpload,
    recommendation: session.recommendation,
  };
}

async function markUploadFailure(tabId, endedReason, error) {
  const session = await getSession(tabId);
  if (!session) {
    return;
  }
  session.active = false;
  session.endedReason = endedReason;
  session.lastUpload = {
    uploadedAt: new Date().toISOString(),
    error: String(error && error.message ? error.message : error),
  };
  await saveSession(tabId, session);
}

async function ensureAutoSession(tabId, requestedUrl, options = {}) {
  if (!Number.isInteger(tabId) || !isSupportedGreenhouseUrl(requestedUrl)) {
    return {
      session: await getSession(tabId),
      config: await getConfig(),
      started: false,
    };
  }
  const config = normalizeConfig(options.config || (await getConfig()));
  if (!config.autoObserve) {
    return {
      session: await getSession(tabId),
      config,
      started: false,
    };
  }

  let session = await getSession(tabId);
  if (session && session.active) {
    if (session.requestedUrl === requestedUrl || isLikelyConfirmationUrl(requestedUrl)) {
      session.finalPageUrl = requestedUrl;
      if (options.pageTitle) {
        session.pageTitle = String(options.pageTitle).trim() || null;
      }
      await saveSession(tabId, session);
      return {
        session,
        config,
        started: false,
      };
    }
    await ingestSession(tabId, {
      finalPageUrl: requestedUrl,
      endedReason: 'navigated_to_new_greenhouse_page',
    });
    session = null;
  }

  if (session && !session.active && session.requestedUrl === requestedUrl) {
    return {
      session,
      config,
      started: false,
    };
  }

  session = await startSession(tabId, requestedUrl, config, {
    startedReason: String(options.startedReason || '').trim() || 'auto_observe',
    pageTitle: options.pageTitle || null,
  });
  return {
    session,
    config,
    started: true,
  };
}

async function handleContentReady(tabId, message) {
  const pageUrl = String(message.pageUrl || '').trim();
  const pageTitle = String(message.pageTitle || '').trim();
  const config = await getConfig();
  let session = await getSession(tabId);
  if (Number.isInteger(tabId) && isSupportedGreenhouseUrl(pageUrl) && config.autoObserve) {
    const ensured = await ensureAutoSession(tabId, pageUrl, {
      config,
      startedReason: 'auto_observe',
      pageTitle,
    });
    session = ensured.session;
  }
  return {
    ok: true,
    autoObserve: config.autoObserve,
    active: !!(session && session.active),
    config: session && session.active ? session.config : config,
    summary: summarizeSession(session),
    recommendation: session ? session.recommendation : null,
  };
}

async function handleSessionStatus(tabId, message) {
  const config = await getConfig();
  const pageUrl = String(message.url || '').trim();
  let session = await getSession(tabId);

  if (Number.isInteger(tabId) && isSupportedGreenhouseUrl(pageUrl) && config.autoObserve) {
    const ensured = await ensureAutoSession(tabId, pageUrl, {
      config,
      startedReason: 'popup_status_refresh',
    });
    session = ensured.session;
  }

  return {
    ok: true,
    autoObserve: config.autoObserve,
    config,
    summary: summarizeSession(session),
    recommendation: session ? session.recommendation : null,
  };
}

async function handleConfigUpdate(tabId, message) {
  const config = await saveConfig(message.config || {});
  const pageUrl = String(message.url || '').trim();
  let session = await getSession(tabId);

  if (Number.isInteger(tabId) && pageUrl && isSupportedGreenhouseUrl(pageUrl)) {
    if (config.autoObserve) {
      const ensured = await ensureAutoSession(tabId, pageUrl, {
        config,
        startedReason: 'config_update_auto_observe',
      });
      session = ensured.session;
    } else if (session && session.active) {
      await ingestSession(tabId, {
        finalPageUrl: pageUrl,
        endedReason: 'auto_observe_disabled',
      });
      session = await getSession(tabId);
    } else {
      await sendMessageToTab(tabId, {
        type: 'gh_content_deactivate',
      });
    }
  }

  return {
    ok: true,
    autoObserve: config.autoObserve,
    config,
    summary: summarizeSession(session),
    recommendation: session ? session.recommendation : null,
  };
}

async function handleObserverEvent(tabId, message) {
  if (!Number.isInteger(tabId)) {
    return { ok: false, message: 'Missing tab id.' };
  }
  const pageUrl = String(message.pageUrl || '').trim();
  let session = await getSession(tabId);

  if ((!session || !session.active) && isSupportedGreenhouseUrl(pageUrl)) {
    const ensured = await ensureAutoSession(tabId, pageUrl, {
      startedReason: 'event_triggered_auto_observe',
    });
    session = ensured.session;
  }
  if (!session || !session.active) {
    return { ok: false, message: 'No active session.' };
  }
  if (message.event && typeof message.event === 'object') {
    session.events.push(message.event);
  }
  if (pageUrl) {
    session.finalPageUrl = pageUrl;
  }
  await saveSession(tabId, session);
  return {
    ok: true,
    summary: summarizeSession(session),
    recommendation: session.recommendation,
  };
}

async function handleConfirmation(tabId, message) {
  if (!Number.isInteger(tabId)) {
    return { ok: false, message: 'Missing tab id.' };
  }
  const session = await getSession(tabId);
  if (!session) {
    return { ok: false, message: 'No active session.' };
  }
  session.confirmationDetected = true;
  if (message.finalPageUrl) {
    session.finalPageUrl = String(message.finalPageUrl);
  }
  await saveSession(tabId, session);
  return ingestSession(tabId, {
    confirmationDetected: true,
    finalPageUrl: session.finalPageUrl,
    endedReason: 'confirmation_detected',
  });
}

async function handleFetchRecommendation(message) {
  const backendBaseUrl = normalizeBaseUrl(message.backendBaseUrl);
  const requestedUrl = String(message.url || '').trim();
  if (!requestedUrl) {
    return { ok: false, message: 'Missing url.' };
  }
  const endpoint =
    `${backendBaseUrl}/api/greenhouse-observer/recommendation?url=` + encodeURIComponent(requestedUrl);
  return fetchJSON(endpoint);
}

async function handleFetchHistory(message) {
  const backendBaseUrl = normalizeBaseUrl(message.backendBaseUrl);
  const requestedUrl = String(message.url || '').trim();
  if (!requestedUrl) {
    return { ok: false, message: 'Missing url.' };
  }
  const limit = Number.isInteger(message.limit) ? message.limit : 5;
  const endpoint =
    `${backendBaseUrl}/api/greenhouse-observer/history?url=` +
    encodeURIComponent(requestedUrl) +
    `&limit=${encodeURIComponent(String(limit))}`;
  return fetchJSON(endpoint);
}

async function handleTabUrlUpdate(tabId, newUrl) {
  const session = await getSession(tabId);
  if (!session) {
    return;
  }
  if (!session.active) {
    if (session.requestedUrl !== newUrl) {
      await clearSession(tabId);
    }
    return;
  }

  if (isLikelyConfirmationUrl(newUrl) && isSupportedGreenhouseUrl(newUrl)) {
    session.finalPageUrl = newUrl;
    await saveSession(tabId, session);
    return;
  }

  if (!isSupportedGreenhouseUrl(newUrl)) {
    try {
      await ingestSession(tabId, {
        finalPageUrl: newUrl,
        endedReason: 'navigated_away',
      });
    } catch (error) {
      await markUploadFailure(tabId, 'navigated_away', error);
    }
    return;
  }

  if (session.requestedUrl !== newUrl) {
    try {
      await ingestSession(tabId, {
        finalPageUrl: newUrl,
        endedReason: 'navigated_to_new_greenhouse_page',
      });
    } catch (error) {
      await markUploadFailure(tabId, 'navigated_to_new_greenhouse_page', error);
    }
  }
}

async function handleTabRemoved(tabId) {
  const session = await getSession(tabId);
  if (!session) {
    return;
  }
  if (session.active) {
    try {
      await ingestSession(tabId, {
        finalPageUrl: session.finalPageUrl || session.requestedUrl,
        endedReason: 'tab_closed',
      });
    } catch (error) {
      await markUploadFailure(tabId, 'tab_closed', error);
    }
  }
  await clearSession(tabId);
}

chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
  if (typeof changeInfo.url !== 'string' || !changeInfo.url.trim()) {
    return;
  }
  void handleTabUrlUpdate(tabId, changeInfo.url);
});

chrome.tabs.onRemoved.addListener((tabId) => {
  void handleTabRemoved(tabId);
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  const tabId =
    message && Number.isInteger(message.tabId)
      ? message.tabId
      : sender && sender.tab && Number.isInteger(sender.tab.id)
        ? sender.tab.id
        : null;

  if (!message || !message.type) {
    sendResponse({ ok: false, message: 'Missing message type.' });
    return false;
  }

  const respond = (promise) => {
    Promise.resolve(promise)
      .then(sendResponse)
      .catch((error) => {
        sendResponse({ ok: false, message: String(error && error.message ? error.message : error) });
      });
    return true;
  };

  if (message.type === 'gh_content_ready') {
    return respond(handleContentReady(tabId, message));
  }

  if (message.type === 'gh_session_status') {
    return respond(handleSessionStatus(tabId, message));
  }

  if (message.type === 'gh_session_start') {
    if (tabId === null) {
      sendResponse({ ok: false, message: 'Missing tab id.' });
      return false;
    }
    const requestedUrl = String(message.url || '').trim();
    if (!requestedUrl) {
      sendResponse({ ok: false, message: 'Missing tab URL.' });
      return false;
    }
    return respond(
      (async () => {
        const existing = await getSession(tabId);
        if (existing && existing.active && existing.requestedUrl === requestedUrl) {
          return {
            ok: true,
            summary: summarizeSession(existing),
            recommendation: existing.recommendation,
          };
        }
        if (existing && existing.active) {
          await ingestSession(tabId, {
            finalPageUrl: requestedUrl,
            endedReason: 'manual_restart',
          });
        }
        const session = await startSession(tabId, requestedUrl, message.config || {}, {
          startedReason: 'manual_start',
        });
        return {
          ok: true,
          summary: summarizeSession(session),
          recommendation: session.recommendation,
        };
      })()
    );
  }

  if (message.type === 'gh_session_stop') {
    if (tabId === null) {
      sendResponse({ ok: false, message: 'Missing tab id.' });
      return false;
    }
    return respond(
      ingestSession(tabId, {
        finalPageUrl: String(message.finalPageUrl || '').trim() || null,
        endedReason: String(message.endedReason || '').trim() || 'manual_stop',
        confirmationDetected: !!message.confirmationDetected,
      })
    );
  }

  if (message.type === 'gh_config_update') {
    return respond(handleConfigUpdate(tabId, message));
  }

  if (message.type === 'gh_observer_event') {
    return respond(handleObserverEvent(tabId, message));
  }

  if (message.type === 'gh_observer_confirmation') {
    return respond(handleConfirmation(tabId, message));
  }

  if (message.type === 'gh_fetch_recommendation') {
    return respond(handleFetchRecommendation(message));
  }

  if (message.type === 'gh_fetch_history') {
    return respond(handleFetchHistory(message));
  }

  sendResponse({ ok: false, message: `Unsupported message type: ${message.type}` });
  return false;
});
