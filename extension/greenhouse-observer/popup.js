const CONFIG_STORAGE_KEY = 'greenhouseObserverConfig';
const DEFAULTS = {
  backendBaseUrl: 'http://127.0.0.1:8776',
  resumeDecisionSource: 'manual',
  externalResumeRecommendation: '',
  autoObserve: true,
};

async function getActiveTab() {
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

function isGreenhouseUrl(url) {
  return /^https:\/\/(?:boards|job-boards)\.greenhouse\.io\//i.test(String(url || ''));
}

function setStatus(text) {
  document.getElementById('status').textContent = text;
}

function setRecommendation(text) {
  document.getElementById('recommendation').textContent = text;
}

function setHistory(text) {
  document.getElementById('history').textContent = text;
}

function currentConfig() {
  return {
    backendBaseUrl: document.getElementById('backendBaseUrl').value.trim(),
    resumeDecisionSource: document.getElementById('resumeDecisionSource').value,
    externalResumeRecommendation: document.getElementById('externalResumeRecommendation').value.trim(),
    autoObserve: document.getElementById('autoObserve').checked,
  };
}

async function saveConfig(tab) {
  const config = currentConfig();
  await chrome.storage.local.set({ [CONFIG_STORAGE_KEY]: config });
  await sendRuntimeMessage({
    type: 'gh_config_update',
    tabId: tab && tab.id ? tab.id : null,
    url: tab && tab.url ? tab.url : '',
    config,
  });
  return config;
}

async function loadConfig() {
  const stored = await chrome.storage.local.get(CONFIG_STORAGE_KEY);
  const config = {
    ...DEFAULTS,
    ...(stored[CONFIG_STORAGE_KEY] || {}),
  };
  document.getElementById('backendBaseUrl').value = config.backendBaseUrl;
  document.getElementById('resumeDecisionSource').value = config.resumeDecisionSource;
  document.getElementById('externalResumeRecommendation').value = config.externalResumeRecommendation;
  document.getElementById('autoObserve').checked = config.autoObserve !== false;
  return config;
}

function sendRuntimeMessage(message) {
  return chrome.runtime.sendMessage(message);
}

function renderRecommendation(recommendation) {
  if (!recommendation || recommendation.status === 'idle') {
    setRecommendation('No recommendation cached for this page yet.');
    return;
  }
  if (recommendation.status === 'loading') {
    setRecommendation('Loading recommendation...');
    return;
  }
  if (recommendation.status === 'error') {
    setRecommendation(`Recommendation error\n${recommendation.error || 'Unknown error'}`);
    return;
  }
  const payload = recommendation.payload || {};
  const reviewQueue = Array.isArray(payload.review_queue) ? payload.review_queue : [];
  const pendingReviewCount = reviewQueue.filter((item) => item && item.status !== 'answered').length;
  const lines = [
    payload.company_name || '-',
    payload.job_title || '-',
    `resume: ${payload.recommended_resume_variant || '-'} / ${payload.recommended_resume_file || '-'}`,
    `auto submit eligible: ${payload.auto_submit_eligible ? 'yes' : 'no'}`,
    `review pending: ${pendingReviewCount}`,
  ];
  setRecommendation(lines.join('\n'));
}

function renderHistory(payload) {
  if (!payload || payload.status === 'loading') {
    setHistory('Loading history...');
    return;
  }
  if (!payload || payload.status === 'error') {
    setHistory(payload && payload.error ? `History error\n${payload.error}` : 'History unavailable.');
    return;
  }
  const sessions = Array.isArray(payload.sessions) ? payload.sessions : [];
  if (sessions.length === 0) {
    setHistory('No recorded sessions for this job yet.');
    return;
  }
  const lines = sessions.map((session) => {
    const summary = session.observation_summary || {};
    const resume = session.manual_resume_decision || {};
    const assistant = session.assistant_recommendation || {};
    const capturedAt = String(session.captured_at || '-').replace('T', ' ').slice(0, 16);
    const outcome = session.submitted
      ? 'submitted'
      : session.confirmation_detected
        ? 'confirmed'
        : session.ended_reason || 'not submitted';
    const resumeLabel = resume.selected_variant || assistant.resume_variant || '-';
    const resumeNote =
      resume.matches_assistant_recommendation === false
        ? `${resumeLabel} (override)`
        : resumeLabel;
    return [
      `${capturedAt} | ${outcome}`,
      `events ${summary.event_count || 0} | overrides ${summary.override_count || 0} | resume ${resumeNote}`,
      `session ${String(session.manual_session_id || '').slice(-10) || '-'}`,
    ].join('\n');
  });
  setHistory(lines.join('\n\n'));
}

async function refreshStatus(tab) {
  if (!tab || !tab.id) {
    setStatus('No active tab.');
    return null;
  }
  const result = await sendRuntimeMessage({
    type: 'gh_session_status',
    tabId: tab.id,
    url: tab.url || '',
  });
  if (!result || !result.ok) {
    setStatus(result && result.message ? result.message : 'Status unavailable.');
    return null;
  }
  const summary = result.summary || {};
  const config = result.config || {};
  const lines = [
    `auto observe: ${result.autoObserve ? 'on' : 'off'}`,
    `active: ${summary.active ? 'yes' : 'no'}`,
    `started: ${summary.startedAt || '-'}`,
    `reason: ${summary.startedReason || summary.endedReason || '-'}`,
    `events: ${summary.eventCount || 0}`,
    `confirmation: ${summary.confirmationDetected ? 'yes' : 'no'}`,
    `recommendation: ${summary.recommendationStatus || 'idle'}`,
    `decision source: ${config.resumeDecisionSource || '-'}`,
    `last upload: ${summary.lastUpload ? 'ok' : 'none'}`,
  ];
  setStatus(lines.join('\n'));
  renderRecommendation(result.recommendation || null);
  return result;
}

async function fetchRecommendation(tab, config) {
  if (!tab || !tab.url) {
    setRecommendation('No active tab.');
    return;
  }
  if (!isGreenhouseUrl(tab.url)) {
    setRecommendation('This tab is not a supported Greenhouse hosted page.');
    return;
  }
  setRecommendation('Loading recommendation...');
  const result = await sendRuntimeMessage({
    type: 'gh_fetch_recommendation',
    backendBaseUrl: config.backendBaseUrl,
    url: tab.url,
  });
  if (!result || !result.ok) {
    setRecommendation(result && result.message ? result.message : 'Recommendation request failed.');
    return;
  }
  renderRecommendation({
    status: 'ok',
    payload: result,
  });
}

async function fetchHistory(tab, config) {
  if (!tab || !tab.url) {
    setHistory('No active tab.');
    return;
  }
  if (!isGreenhouseUrl(tab.url)) {
    setHistory('History is only available on supported Greenhouse hosted pages.');
    return;
  }
  renderHistory({ status: 'loading' });
  const result = await sendRuntimeMessage({
    type: 'gh_fetch_history',
    backendBaseUrl: config.backendBaseUrl,
    url: tab.url,
    limit: 5,
  });
  if (!result || !result.ok) {
    renderHistory({
      status: 'error',
      error: result && result.message ? result.message : 'History request failed.',
    });
    return;
  }
  renderHistory(result);
}

async function main() {
  const tab = await getActiveTab();
  const config = await loadConfig();
  const tabInfo = document.getElementById('tabInfo');
  if (!tab || !tab.url) {
    tabInfo.textContent = 'No active tab.';
  } else {
    tabInfo.textContent = isGreenhouseUrl(tab.url)
      ? `Tab: ${tab.url}`
      : 'Current tab is not a supported Greenhouse hosted page.';
  }

  document.getElementById('saveConfig').addEventListener('click', async () => {
    const latestConfig = await saveConfig(tab);
    setStatus(`Settings saved\nauto observe: ${latestConfig.autoObserve ? 'on' : 'off'}`);
    await refreshStatus(tab);
    await fetchHistory(tab, latestConfig);
  });

  document.getElementById('refreshRecommendation').addEventListener('click', async () => {
    const latestConfig = await loadConfig();
    await fetchRecommendation(tab, latestConfig);
    await fetchHistory(tab, latestConfig);
  });

  document.getElementById('refreshStatus').addEventListener('click', async () => {
    const latestConfig = await loadConfig();
    await refreshStatus(tab);
    await fetchHistory(tab, latestConfig);
  });

  document.getElementById('startNow').addEventListener('click', async () => {
    if (!tab || !tab.id || !tab.url) {
      setStatus('No active tab.');
      return;
    }
    if (!isGreenhouseUrl(tab.url)) {
      setStatus('Open a supported Greenhouse hosted page first.');
      return;
    }
    const latestConfig = await saveConfig(tab);
    const result = await sendRuntimeMessage({
      type: 'gh_session_start',
      tabId: tab.id,
      url: tab.url,
      config: latestConfig,
    });
    if (!result || !result.ok) {
      setStatus(result && result.message ? result.message : 'Failed to start capture.');
      return;
    }
    await refreshStatus(tab);
  });

  document.getElementById('uploadNow').addEventListener('click', async () => {
    if (!tab || !tab.id) {
      setStatus('No active tab.');
      return;
    }
    const result = await sendRuntimeMessage({
      type: 'gh_session_stop',
      tabId: tab.id,
      finalPageUrl: tab.url || '',
      endedReason: 'manual_stop',
    });
    if (!result || !result.ok) {
      setStatus(result && result.message ? result.message : 'Failed to upload current session.');
      return;
    }
    const upload = result.upload && result.upload.payload ? result.upload.payload : null;
    if (upload) {
      setStatus(
        `uploaded\ncompany: ${upload.company_name || '-'}\njob: ${upload.job_title || '-'}\nevents: ${upload.event_count || 0}`
      );
      renderRecommendation(result.recommendation || null);
      await fetchHistory(tab, await loadConfig());
      return;
    }
    const latestConfig = await loadConfig();
    await refreshStatus(tab);
    await fetchHistory(tab, latestConfig);
  });

  const initialStatus = await refreshStatus(tab);
  await fetchHistory(tab, config);
  if (!initialStatus || !initialStatus.recommendation || initialStatus.recommendation.status === 'idle') {
    await fetchRecommendation(tab, config);
  }
}

main().catch((error) => {
  setStatus(String(error && error.message ? error.message : error));
});
