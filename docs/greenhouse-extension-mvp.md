# Greenhouse Observer Extension MVP

## What it does

This extension records manual Greenhouse application behavior in your normal browser and sends the session to a local Python API.

It now auto-observes supported Greenhouse hosted pages by default. You do not need to click Start Capture for each page.

It captures:

- field focus / change / blur
- custom choice control clicks for combobox / checkbox / radio-style widgets when exposed in the DOM
- submit clicks
- page start / visibility / page-hide lifecycle events
- confirmation detection
- resume decision metadata

The local API reuses the existing Greenhouse assistant and trace store so the extension data lands in the same manual-observation JSONL files.

## Files

- Extension: `extension/greenhouse-observer/`
- Local API: `career_monitor/greenhouse_observer_api.py`

## Start the local API

```bash
uv run python -m career_monitor.greenhouse_observer_api \
  --profile-file "<absolute-path-to-profile.json>"
```

Default URLs:

- Health: `http://127.0.0.1:8776/api/health`
- Recommendation: `http://127.0.0.1:8776/api/greenhouse-observer/recommendation`
- History: `http://127.0.0.1:8776/api/greenhouse-observer/history`
- Session detail: `http://127.0.0.1:8776/api/greenhouse-observer/session`
- Ingest: `http://127.0.0.1:8776/api/greenhouse-observer/ingest`

## Load the extension

1. Open Chrome or Chromium.
2. Go to `chrome://extensions`.
3. Enable `Developer mode`.
4. Click `Load unpacked`.
5. Select `extension/greenhouse-observer/`.

## Use it

1. Start the local observer API.
2. Open a hosted Greenhouse job page.
3. The extension will begin observing automatically if `Automatic Observation` is enabled.
4. Optionally open the popup to:
   - confirm observer status
   - set backend URL
   - set resume decision source (`manual`, `chatgpt`, `assistant`, `mixed`)
   - set external recommendation, if any
   - refresh the backend recommendation
   - inspect recent captured sessions for the current Greenhouse job
5. Fill the application normally.
6. Submit the application.

Sessions are uploaded automatically when:

- a confirmation page is detected
- you navigate away from the Greenhouse application page
- you close the tab

You can still use the popup for manual overrides:

- `Observe This Tab Now` to force a session start
- `Upload Current Session` to finalize and upload immediately

## Output files

By default, captured sessions are appended to:

- `.state/greenhouse_manual_sessions.jsonl`
- `.state/greenhouse_manual_field_events.jsonl`
