from __future__ import annotations

import json
import sqlite3
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from career_monitor.greenhouse_observer_api import (
    AssistantRunManager,
    _recommendation_payload,
    assistant_trace_detail_payload,
    ingest_extension_observation,
    recent_assistant_runs_payload,
    manual_session_detail_payload,
    recent_manual_sessions_payload,
)
from career_monitor.greenhouse_assistant_queue import (
    GreenhouseAssistantQueue,
    latest_jobs_db_greenhouse_batch_payload,
)

from test_greenhouse_assistant import sample_api_payload, sample_policy_payload


class GreenhouseObserverAPITests(unittest.TestCase):
    def _create_jobs_db(self, path: Path) -> None:
        conn = sqlite3.connect(str(path))
        try:
            conn.execute(
                """
                CREATE TABLE jobs (
                    fingerprint TEXT PRIMARY KEY,
                    company TEXT NOT NULL,
                    title TEXT,
                    url TEXT,
                    source TEXT,
                    first_seen TEXT,
                    posted_at TEXT,
                    active INTEGER NOT NULL DEFAULT 1,
                    application_status TEXT NOT NULL DEFAULT '',
                    work_auth_status TEXT NOT NULL DEFAULT 'unknown',
                    assistant_last_outcome TEXT NOT NULL DEFAULT '',
                    assistant_last_source TEXT NOT NULL DEFAULT '',
                    assistant_last_review_pending_count INTEGER NOT NULL DEFAULT 0,
                    assistant_last_confirmation_detected INTEGER NOT NULL DEFAULT 0
                )
                """
            )
            conn.executemany(
                """
                INSERT INTO jobs (
                    fingerprint, company, title, url, source, first_seen, posted_at, active,
                    application_status, work_auth_status, assistant_last_outcome, assistant_last_source,
                    assistant_last_review_pending_count, assistant_last_confirmation_detected
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                [
                    (
                        "fp_1",
                        "My Greenhouse",
                        "Software Engineer",
                        "https://job-boards.greenhouse.io/example/jobs/12345?gh_src=my.greenhouse.search",
                        "my_greenhouse",
                        "2026-03-07T04:06:30Z",
                        "2026-03-07T03:00:00Z",
                        1,
                        "",
                        "unknown",
                        "",
                        "",
                        0,
                        0,
                    ),
                    (
                        "fp_2",
                        "My Greenhouse",
                        "Backend Engineer",
                        "https://job-boards.greenhouse.io/example/jobs/99999?gh_src=my.greenhouse.search",
                        "my_greenhouse",
                        "2026-03-07T04:06:30Z",
                        "2026-03-07T03:30:00Z",
                        1,
                        "applied",
                        "unknown",
                        "submitted",
                        "manual",
                        0,
                        1,
                    ),
                    (
                        "fp_3",
                        "Other",
                        "Not Greenhouse",
                        "https://jobs.example.com/roles/1",
                        "web",
                        "2026-03-07T04:06:30Z",
                        "2026-03-07T04:00:00Z",
                        1,
                        "",
                        "unknown",
                        "",
                        "",
                        0,
                        0,
                    ),
                ],
            )
            conn.commit()
        finally:
            conn.close()

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_recommendation_payload_exposes_resume_recommendation(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_policy_payload()
        profile = {
            "resume_variants": {
                "ai": "/tmp/resume-ai.pdf",
                "cloud": "/tmp/resume-cloud.pdf",
            }
        }

        payload = _recommendation_payload(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            profile=profile,
            approved_answers=None,
            timeout_seconds=5,
        )

        self.assertTrue(payload["ok"])
        self.assertEqual(payload["recommended_resume_file"], "resume-ai.pdf")
        self.assertEqual(payload["recommended_resume_variant"], "ai")
        self.assertEqual(payload["resume_selection_source"], "heuristic_recommended")
        self.assertEqual(len(payload["available_resume_variants"]), 2)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_ingest_extension_observation_persists_manual_trace(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        payload = {
            "url": "https://job-boards.greenhouse.io/example/jobs/12345",
            "final_page_url": "https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
            "confirmation_detected": True,
            "ended_reason": "confirmation_detected",
            "resume_decision_source": "manual",
            "external_resume_recommendation": "distributed_systems",
            "events": [
                {
                    "sequence": 1,
                    "offset_ms": 10,
                    "event_type": "change",
                    "page_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                    "question_label": "First Name",
                    "api_name": "first_name",
                    "element_id": "first_name",
                    "element_name": "first_name",
                    "tag_name": "input",
                    "input_type": "text",
                    "required": True,
                    "value": "Ada",
                    "checked": None,
                    "file_names": [],
                },
                {
                    "sequence": 2,
                    "offset_ms": 20,
                    "event_type": "change",
                    "page_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                    "question_label": "Why do you want to work here?",
                    "api_name": "question_1",
                    "element_id": "question_1",
                    "element_name": "question_1",
                    "tag_name": "textarea",
                    "input_type": "",
                    "required": False,
                    "value": "I care about the mission.",
                    "checked": None,
                    "file_names": [],
                },
            ],
        }
        profile = {
            "first_name": "Ada",
            "resume_path": "/tmp/resume.pdf",
        }

        with tempfile.TemporaryDirectory() as tmpdir:
            session_file = Path(tmpdir) / "sessions.jsonl"
            field_events_file = Path(tmpdir) / "field_events.jsonl"

            result = ingest_extension_observation(
                payload,
                profile=profile,
                approved_answers=None,
                timeout_seconds=5,
                session_file=str(session_file),
                field_events_file=str(field_events_file),
                sync_jobs_db=False,
            )

            self.assertTrue(result["ok"])
            self.assertEqual(result["event_count"], 2)
            session_records = [
                json.loads(line)
                for line in session_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            self.assertEqual(len(session_records), 1)
            self.assertTrue(session_records[0]["confirmation_detected"])
            self.assertEqual(session_records[0]["manual_resume_decision"]["decision_source"], "manual")

    def test_recent_manual_sessions_payload_filters_by_current_application(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            session_file = Path(tmpdir) / "sessions.jsonl"
            session_file.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "record_type": "greenhouse_manual_session",
                                "manual_session_id": "ghmanual_new",
                                "captured_at": "2026-03-07T18:51:23+00:00",
                                "application_key": "greenhouse:example:12345",
                                "company_name": "Example Co",
                                "job_title": "Software Engineer",
                                "requested_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "public_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "final_page_url": "https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
                                "submitted": True,
                                "confirmation_detected": True,
                                "ended_reason": "confirmation_detected",
                                "observation_summary": {
                                    "event_count": 20,
                                    "override_count": 1,
                                    "review_answer_count": 2,
                                },
                                "assistant_recommendation": {"resume_variant": "ai", "resume_file": "resume-ai.pdf"},
                                "manual_resume_decision": {
                                    "selected_variant": "cloud",
                                    "selected_file": "resume-cloud.pdf",
                                    "matches_assistant_recommendation": False,
                                },
                            }
                        ),
                        json.dumps(
                            {
                                "record_type": "greenhouse_manual_session",
                                "manual_session_id": "ghmanual_old",
                                "captured_at": "2026-03-06T18:51:23+00:00",
                                "application_key": "greenhouse:other:55555",
                                "company_name": "Other Co",
                                "job_title": "Backend Engineer",
                                "requested_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "public_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "final_page_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "submitted": False,
                                "confirmation_detected": False,
                                "ended_reason": "manual_stop",
                                "observation_summary": {"event_count": 12, "override_count": 0},
                                "assistant_recommendation": {"resume_variant": "distributed_systems"},
                                "manual_resume_decision": {"selected_variant": "distributed_systems"},
                            }
                        ),
                    ]
                ),
                encoding="utf-8",
            )

            payload = recent_manual_sessions_payload(
                "https://job-boards.greenhouse.io/example/jobs/12345",
                session_file=session_file,
                limit=5,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["application_key"], "greenhouse:example:12345")
            self.assertEqual(payload["session_count"], 1)
            self.assertEqual(payload["sessions"][0]["manual_session_id"], "ghmanual_new")
            self.assertEqual(payload["sessions"][0]["manual_resume_decision"]["selected_variant"], "cloud")

    def test_manual_session_detail_payload_returns_limited_events(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            session_file = Path(tmpdir) / "sessions.jsonl"
            field_events_file = Path(tmpdir) / "field_events.jsonl"
            session_file.write_text(
                json.dumps(
                    {
                        "record_type": "greenhouse_manual_session",
                        "manual_session_id": "ghmanual_detail",
                        "captured_at": "2026-03-07T18:51:23+00:00",
                        "application_key": "greenhouse:example:12345",
                        "company_name": "Example Co",
                        "job_title": "Software Engineer",
                    }
                )
                + "\n",
                encoding="utf-8",
            )
            field_events_file.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "record_type": "greenhouse_manual_field_event",
                                "manual_session_id": "ghmanual_detail",
                                "sequence": 1,
                                "offset_ms": 10,
                                "question_label": "First Name",
                            }
                        ),
                        json.dumps(
                            {
                                "record_type": "greenhouse_manual_field_event",
                                "manual_session_id": "ghmanual_detail",
                                "sequence": 2,
                                "offset_ms": 20,
                                "question_label": "Email",
                            }
                        ),
                        json.dumps(
                            {
                                "record_type": "greenhouse_manual_field_event",
                                "manual_session_id": "ghmanual_detail",
                                "sequence": 3,
                                "offset_ms": 30,
                                "question_label": "Phone",
                            }
                        ),
                    ]
                ),
                encoding="utf-8",
            )

            payload = manual_session_detail_payload(
                "ghmanual_detail",
                session_file=session_file,
                field_events_file=field_events_file,
                event_limit=2,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["event_count_total"], 3)
            self.assertEqual(payload["event_count_returned"], 2)
            self.assertEqual([event["question_label"] for event in payload["events"]], ["Email", "Phone"])

    def test_recent_assistant_runs_payload_filters_by_current_application(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            trace_file = Path(tmpdir) / "traces.jsonl"
            trace_file.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "record_type": "greenhouse_run_trace",
                                "trace_id": "ghtrace_new",
                                "captured_at": "2026-03-07T19:00:00+00:00",
                                "application_key": "greenhouse:example:12345",
                                "company_name": "Example Co",
                                "job_title": "Software Engineer",
                                "requested_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "public_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "page_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "schema_source": "job_board_api",
                                "result_summary": {
                                    "filled_count": 7,
                                    "skipped_count": 1,
                                    "review_queue_count": 2,
                                    "submitted": False,
                                },
                                "result": {},
                            }
                        ),
                        json.dumps(
                            {
                                "record_type": "greenhouse_run_trace",
                                "trace_id": "ghtrace_old",
                                "captured_at": "2026-03-06T19:00:00+00:00",
                                "application_key": "greenhouse:other:55555",
                                "company_name": "Other Co",
                                "job_title": "Backend Engineer",
                                "requested_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "public_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "page_url": "https://job-boards.greenhouse.io/other/jobs/55555",
                                "schema_source": "job_board_api",
                                "result_summary": {
                                    "filled_count": 5,
                                    "skipped_count": 0,
                                    "review_queue_count": 0,
                                    "submitted": True,
                                },
                                "result": {},
                            }
                        ),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )

            payload = recent_assistant_runs_payload(
                "https://job-boards.greenhouse.io/example/jobs/12345",
                trace_file=trace_file,
                limit=5,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["application_key"], "greenhouse:example:12345")
            self.assertEqual(payload["run_count"], 1)
            self.assertEqual(payload["runs"][0]["trace_id"], "ghtrace_new")

            detail = assistant_trace_detail_payload("ghtrace_new", trace_file=trace_file)
            self.assertTrue(detail["ok"])
            self.assertEqual(detail["trace"]["company_name"], "Example Co")

    def test_latest_jobs_db_greenhouse_batch_skips_applied_jobs(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            jobs_db = Path(tmpdir) / "jobs.db"
            queue_file = Path(tmpdir) / "queue.json"
            self._create_jobs_db(jobs_db)

            payload = latest_jobs_db_greenhouse_batch_payload(
                jobs_db_file=jobs_db,
                queue_file=queue_file,
                mode="latest_batch",
                limit=10,
                skip_applied=True,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["candidate_count"], 1)
            self.assertEqual(payload["candidates"][0]["job_title"], "Software Engineer")
            self.assertEqual(payload["skipped_count"], 1)
            self.assertEqual(payload["skipped"][0]["reason"], "already_applied")

    def test_latest_jobs_db_greenhouse_batch_skips_work_auth_blocked_jobs(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            jobs_db = Path(tmpdir) / "jobs.db"
            queue_file = Path(tmpdir) / "queue.json"
            self._create_jobs_db(jobs_db)
            conn = sqlite3.connect(str(jobs_db))
            try:
                conn.execute(
                    "UPDATE jobs SET work_auth_status = 'blocked' WHERE fingerprint = ?",
                    ("fp_1",),
                )
                conn.commit()
            finally:
                conn.close()

            payload = latest_jobs_db_greenhouse_batch_payload(
                jobs_db_file=jobs_db,
                queue_file=queue_file,
                mode="latest_batch",
                limit=10,
                skip_applied=True,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["candidate_count"], 0)
            self.assertEqual(payload["skipped_count"], 2)
            self.assertEqual(
                sorted(item["reason"] for item in payload["skipped"]),
                ["already_applied", "work_auth_blocked"],
            )

    def test_assistant_queue_enqueues_latest_batch_and_tracks_duplicates(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            jobs_db = Path(tmpdir) / "jobs.db"
            queue_file = Path(tmpdir) / "queue.json"
            self._create_jobs_db(jobs_db)
            queue = GreenhouseAssistantQueue(queue_file=queue_file, jobs_db_file=jobs_db)

            first = queue.enqueue_latest_jobs(mode="latest_batch", limit=10, skip_applied=True)
            second = queue.enqueue_latest_jobs(mode="latest_batch", limit=10, skip_applied=True)

            self.assertEqual(first["added_count"], 1)
            self.assertEqual(first["skipped_count"], 1)
            self.assertEqual(second["duplicate_count"], 0)
            self.assertEqual(second["skipped_count"], 2)
            self.assertEqual(sorted(item["reason"] for item in second["skipped"]), ["already_applied", "already_queued"])
            snapshot = queue.snapshot()
            self.assertEqual(snapshot["queue_count"], 1)
            self.assertEqual(snapshot["items"][0]["status"], "queued")

    def test_assistant_queue_persists_selected_resume_for_latest_batch(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            jobs_db = Path(tmpdir) / "jobs.db"
            queue_file = Path(tmpdir) / "queue.json"
            self._create_jobs_db(jobs_db)
            queue = GreenhouseAssistantQueue(queue_file=queue_file, jobs_db_file=jobs_db)

            result = queue.enqueue_latest_jobs(
                mode="latest_batch",
                limit=10,
                skip_applied=True,
                selection_by_application_key={
                    "greenhouse:example:12345": {
                        "selected_resume_variant": "cloud",
                        "selected_resume_path": "/tmp/resume-cloud.pdf",
                        "resume_selection_source": "slm_recommended",
                    }
                },
            )

            self.assertEqual(result["added_count"], 1)
            snapshot = queue.snapshot()
            self.assertEqual(snapshot["items"][0]["selected_resume_variant"], "cloud")
            self.assertEqual(snapshot["items"][0]["selected_resume_path"], "/tmp/resume-cloud.pdf")
            self.assertEqual(snapshot["items"][0]["resume_selection_source"], "slm_recommended")

    @mock.patch("career_monitor.greenhouse_observer_api.execute_greenhouse_autofill")
    def test_assistant_run_manager_starts_and_completes_run(
        self,
        mock_execute_autofill: mock.Mock,
    ) -> None:
        class DummyResult:
            def to_dict(self) -> dict[str, object]:
                return {
                    "analysis": {
                        "schema": {"company_name": "Example Co", "title": "Software Engineer"},
                        "review_queue": [{"status": "pending"}],
                        "auto_submit_eligible": False,
                    },
                    "filled": [{"question_label": "First Name"}],
                    "skipped": [],
                    "errors": [],
                    "browser_validation_errors": [],
                    "submit_safety": {"eligible_after_browser_validation": False},
                    "submission": {"submitted": False, "confirmation_detected": False},
                    "trace": {"trace_id": "ghtrace_live"},
                    "jobs_db_sync": {"stored": True},
                }

        mock_execute_autofill.return_value = DummyResult()

        manager = AssistantRunManager(
            profile_file=None,
            answers_file=None,
            timeout_seconds=5,
            trace_file=".state/test_run_traces.jsonl",
            training_examples_file=".state/test_training_examples.jsonl",
            storage_state_file=".state/test_storage_state.json",
            sync_jobs_db=False,
            jobs_db_file=".state/jobs.db",
        )

        started = manager.start_run(
            url="https://job-boards.greenhouse.io/example/jobs/12345",
            allow_submit=False,
            headless=False,
        )
        self.assertTrue(started["ok"])
        self.assertIn(started["status"], {"queued", "running", "completed", "review"})

        final = None
        for _ in range(40):
            time.sleep(0.01)
            current = manager.get_run(started["run_id"])
            if current["status"] in {"completed", "review"}:
                final = current
                break

        self.assertIsNotNone(final)
        self.assertEqual(final["result_summary"]["company_name"], "Example Co")
        self.assertEqual(final["result_summary"]["trace_id"], "ghtrace_live")
        self.assertEqual(final["result_summary"]["review_pending_count"], 1)
        self.assertEqual(final["status"], "review")
        self.assertTrue(mock_execute_autofill.call_args.kwargs["reuse_browser_session"])
        listing = manager.list_runs()
        self.assertTrue(listing["ok"])
        self.assertEqual(listing["run_count"], 1)
        self.assertEqual(listing["runs"][0]["run_id"], started["run_id"])

    @mock.patch("career_monitor.greenhouse_observer_api.execute_greenhouse_autofill")
    def test_assistant_run_manager_accepts_custom_hosted_greenhouse_url(
        self,
        mock_execute_autofill: mock.Mock,
    ) -> None:
        class DummyResult:
            def to_dict(self) -> dict[str, object]:
                return {
                    "analysis": {
                        "schema": {"company_name": "Roku", "title": "Software Engineer"},
                        "review_queue": [],
                        "auto_submit_eligible": False,
                    },
                    "filled": [],
                    "skipped": [],
                    "errors": [],
                    "browser_validation_errors": [],
                    "submit_safety": {"eligible_after_browser_validation": False},
                    "submission": {"submitted": False, "confirmation_detected": False},
                    "trace": {"trace_id": "ghtrace_hosted"},
                    "jobs_db_sync": {"stored": False},
                }

        mock_execute_autofill.return_value = DummyResult()

        manager = AssistantRunManager(
            profile_file=None,
            answers_file=None,
            timeout_seconds=5,
            trace_file=".state/test_run_traces.jsonl",
            training_examples_file=".state/test_training_examples.jsonl",
            storage_state_file=".state/test_storage_state.json",
            sync_jobs_db=False,
            jobs_db_file=".state/jobs.db",
        )

        started = manager.start_run(
            url="https://www.weareroku.com/jobs/7680250?gh_jid=7680250&gh_src=my.greenhouse.search",
            allow_submit=False,
            headless=False,
        )
        self.assertTrue(started["ok"])
        self.assertEqual(
            started["requested_url"],
            "https://www.weareroku.com/jobs/7680250?gh_jid=7680250&gh_src=my.greenhouse.search",
        )

        final = None
        for _ in range(40):
            time.sleep(0.01)
            current = manager.get_run(started["run_id"])
            if current["status"] in {"completed", "review"}:
                final = current
                break

        self.assertIsNotNone(final)
        assert final is not None
        self.assertEqual(final["result_summary"]["company_name"], "Roku")

    @mock.patch("career_monitor.greenhouse_observer_api.execute_greenhouse_autofill")
    def test_assistant_run_manager_starts_next_queued_run_and_marks_review(
        self,
        mock_execute_autofill: mock.Mock,
    ) -> None:
        class DummyResult:
            def to_dict(self) -> dict[str, object]:
                return {
                    "analysis": {
                        "schema": {"company_name": "Example Co", "title": "Software Engineer"},
                        "review_queue": [{"status": "pending"}],
                        "auto_submit_eligible": False,
                    },
                    "filled": [{"question_label": "First Name"}],
                    "skipped": [],
                    "errors": [],
                    "browser_validation_errors": [],
                    "submit_safety": {"eligible_after_browser_validation": False},
                    "submission": {"submitted": False, "confirmation_detected": False},
                    "trace": {"trace_id": "ghtrace_queue"},
                    "jobs_db_sync": {"stored": False},
                }

        mock_execute_autofill.return_value = DummyResult()

        with tempfile.TemporaryDirectory() as tmpdir:
            jobs_db = Path(tmpdir) / "jobs.db"
            queue_file = Path(tmpdir) / "queue.json"
            self._create_jobs_db(jobs_db)
            queue = GreenhouseAssistantQueue(queue_file=queue_file, jobs_db_file=jobs_db)
            queue.enqueue_latest_jobs(
                mode="latest_batch",
                limit=10,
                skip_applied=True,
                selection_by_application_key={
                    "greenhouse:example:12345": {
                        "selected_resume_variant": "cloud",
                        "selected_resume_path": "/tmp/resume-cloud.pdf",
                        "resume_selection_source": "slm_recommended",
                    }
                },
            )

            manager = AssistantRunManager(
                profile_file=None,
                answers_file=None,
                timeout_seconds=5,
                trace_file=".state/test_run_traces.jsonl",
                training_examples_file=".state/test_training_examples.jsonl",
                storage_state_file=".state/test_storage_state.json",
                sync_jobs_db=False,
                jobs_db_file=str(jobs_db),
                queue_manager=queue,
            )

            started = manager.start_next_queued_run(allow_submit=False, headless=False)
            self.assertTrue(started["ok"])
            self.assertEqual(started["requested_url"], "https://job-boards.greenhouse.io/example/jobs/12345?gh_src=my.greenhouse.search")

            final = None
            for _ in range(40):
                time.sleep(0.01)
                current = manager.get_run(started["run_id"])
                if current["status"] in {"completed", "review"}:
                    final = current
                    break

            self.assertIsNotNone(final)
            self.assertTrue(mock_execute_autofill.call_args.kwargs["reuse_browser_session"])
            self.assertEqual(mock_execute_autofill.call_args.kwargs["profile"]["selected_resume_variant"], "cloud")
            self.assertEqual(mock_execute_autofill.call_args.kwargs["profile"]["selected_resume_path"], "/tmp/resume-cloud.pdf")
            snapshot = queue.snapshot()
            self.assertEqual(snapshot["items"][0]["status"], "review")
            self.assertEqual(snapshot["items"][0]["last_trace_id"], "ghtrace_queue")

    @mock.patch("career_monitor.greenhouse_observer_api.execute_greenhouse_autofill")
    def test_assistant_run_manager_marks_job_policy_blockers_as_review(
        self,
        mock_execute_autofill: mock.Mock,
    ) -> None:
        class DummyResult:
            def to_dict(self) -> dict[str, object]:
                return {
                    "analysis": {
                        "schema": {"company_name": "GRVTY", "title": "Software Developer"},
                        "review_queue": [],
                        "auto_submit_eligible": False,
                    },
                    "filled": [],
                    "skipped": [],
                    "errors": ["Job description: role requires U.S. citizenship and/or security clearance."],
                    "browser_validation_errors": [],
                    "submit_safety": {
                        "eligible_after_browser_validation": False,
                        "job_blockers": [
                            "Job description: role requires U.S. citizenship and/or security clearance."
                        ],
                    },
                    "submission": {"submitted": False, "confirmation_detected": False},
                    "trace": {},
                    "jobs_db_sync": {"stored": False},
                }

        mock_execute_autofill.return_value = DummyResult()

        manager = AssistantRunManager(
            profile_file=None,
            answers_file=None,
            timeout_seconds=5,
            trace_file=".state/test_run_traces.jsonl",
            training_examples_file=".state/test_training_examples.jsonl",
            storage_state_file=".state/test_storage_state.json",
            sync_jobs_db=False,
            jobs_db_file=".state/jobs.db",
        )

        started = manager.start_run(
            url="https://job-boards.greenhouse.io/grvty/jobs/4178136009",
            allow_submit=False,
            headless=False,
        )
        final = None
        for _ in range(40):
            time.sleep(0.01)
            current = manager.get_run(started["run_id"])
            if current["status"] in {"completed", "review"}:
                final = current
                break

        self.assertIsNotNone(final)
        assert final is not None
        self.assertEqual(final["status"], "review")
        self.assertEqual(final["result_summary"]["job_blocker_count"], 1)


if __name__ == "__main__":
    unittest.main()
