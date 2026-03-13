from __future__ import annotations

import sqlite3
import tempfile
import unittest
import json
from pathlib import Path
from unittest import mock

from career_monitor.jobs_db_sync import backfill_jobs_db_from_manual_sessions, sync_greenhouse_job_record


def create_jobs_db(path: Path) -> None:
    conn = sqlite3.connect(str(path))
    try:
        conn.executescript(
            """
            CREATE TABLE jobs (
                fingerprint TEXT PRIMARY KEY,
                company TEXT NOT NULL,
                title TEXT,
                url TEXT,
                source TEXT,
                external_id TEXT,
                location TEXT,
                team TEXT,
                posted_at TEXT,
                posted_at_ts TEXT,
                description TEXT,
                first_seen TEXT,
                last_seen TEXT,
                active INTEGER NOT NULL DEFAULT 1,
                application_status TEXT NOT NULL DEFAULT '',
                application_updated_at TEXT
            );
            """
        )
        conn.executemany(
            """
            INSERT INTO jobs (
                fingerprint, company, title, url, source, external_id, location, team,
                posted_at, posted_at_ts, description, first_seen, last_seen, active,
                application_status, application_updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                (
                    "fp_a",
                    "Cloudflare",
                    "Hardware Systems Engineer",
                    "https://boards.greenhouse.io/cloudflare/jobs/7082275?gh_jid=7082275",
                    "greenhouse",
                    "7082275",
                    "Remote",
                    "",
                    "",
                    "",
                    "",
                    "",
                    "",
                    1,
                    "",
                    None,
                ),
                (
                    "fp_b",
                    "Cloudflare",
                    "Hardware Systems Engineer",
                    "https://boards.greenhouse.io/cloudflare/jobs/7082275?gh_jid=7082275",
                    "greenhouse",
                    "7082275",
                    "Remote",
                    "",
                    "",
                    "",
                    "",
                    "",
                    "",
                    1,
                    "",
                    None,
                ),
                (
                    "fp_c",
                    "Example Co",
                    "Software Engineer",
                    "https://job-boards.greenhouse.io/example/jobs/12345",
                    "greenhouse",
                    "12345",
                    "Remote",
                    "",
                    "",
                    "",
                    "",
                    "",
                    "",
                    1,
                    "",
                    None,
                ),
            ],
        )
        conn.commit()
    finally:
        conn.close()


class JobsDBSyncTests(unittest.TestCase):
    @mock.patch("career_monitor.jobs_db_sync.utc_now", return_value="2026-03-07T19:30:00+00:00")
    def test_sync_greenhouse_job_record_updates_all_matching_rows(self, _mock_now: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "jobs.db"
            create_jobs_db(db_path)

            result = sync_greenhouse_job_record(
                requested_url="https://boards.greenhouse.io/cloudflare/jobs/7082275?gh_jid=7082275",
                public_url="https://boards.greenhouse.io/cloudflare/jobs/7082275?gh_jid=7082275",
                source="extension_observer",
                outcome="submitted",
                application_status="applied",
                trace_id="ghtrace_123",
                manual_session_id="ghmanual_456",
                auto_submit_eligible=False,
                review_pending_count=2,
                confirmation_detected=True,
                jobs_db_file=db_path,
                enabled=True,
            )

            self.assertTrue(result["stored"])
            self.assertEqual(result["matched_count"], 2)

            conn = sqlite3.connect(str(db_path))
            try:
                rows = conn.execute(
                    """
                    SELECT fingerprint, application_status, application_updated_at,
                           assistant_last_source, assistant_last_outcome,
                           assistant_last_trace_id, assistant_last_manual_session_id,
                           assistant_last_auto_submit_eligible, assistant_last_review_pending_count,
                           assistant_last_confirmation_detected
                    FROM jobs
                    WHERE fingerprint IN ('fp_a', 'fp_b')
                    ORDER BY fingerprint
                    """
                ).fetchall()
            finally:
                conn.close()

            self.assertEqual(len(rows), 2)
            for row in rows:
                self.assertEqual(row[1], "applied")
                self.assertEqual(row[2], "2026-03-07T19:30:00+00:00")
                self.assertEqual(row[3], "extension_observer")
                self.assertEqual(row[4], "submitted")
                self.assertEqual(row[5], "ghtrace_123")
                self.assertEqual(row[6], "ghmanual_456")
                self.assertEqual(row[7], 0)
                self.assertEqual(row[8], 2)
                self.assertEqual(row[9], 1)

    @mock.patch("career_monitor.jobs_db_sync.utc_now", return_value="2026-03-07T19:31:00+00:00")
    def test_sync_greenhouse_job_record_matches_queryless_public_url(self, _mock_now: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "jobs.db"
            create_jobs_db(db_path)

            result = sync_greenhouse_job_record(
                requested_url="https://job-boards.greenhouse.io/example/jobs/12345?utm_source=test",
                public_url="https://job-boards.greenhouse.io/example/jobs/12345",
                source="playwright_run",
                outcome="eligible",
                application_status=None,
                trace_id="ghtrace_789",
                manual_session_id=None,
                auto_submit_eligible=True,
                review_pending_count=0,
                confirmation_detected=False,
                jobs_db_file=db_path,
                enabled=True,
            )

            self.assertTrue(result["stored"])
            self.assertEqual(result["matched_count"], 1)
            self.assertEqual(result["fingerprints"], ["fp_c"])

            conn = sqlite3.connect(str(db_path))
            try:
                row = conn.execute(
                    """
                    SELECT application_status, application_updated_at,
                           assistant_last_source, assistant_last_outcome,
                           assistant_last_auto_submit_eligible,
                           assistant_last_review_pending_count,
                           assistant_last_confirmation_detected
                    FROM jobs
                    WHERE fingerprint = 'fp_c'
                    """
                ).fetchone()
            finally:
                conn.close()

            self.assertEqual(row[0], "")
            self.assertIsNone(row[1])
            self.assertEqual(row[2], "playwright_run")
            self.assertEqual(row[3], "eligible")
            self.assertEqual(row[4], 1)
            self.assertEqual(row[5], 0)
            self.assertEqual(row[6], 0)

    @mock.patch("career_monitor.jobs_db_sync.utc_now", return_value="2026-03-07T19:32:00+00:00")
    def test_backfill_jobs_db_from_manual_sessions_updates_matching_rows(self, _mock_now: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "jobs.db"
            session_path = Path(tmpdir) / "manual_sessions.jsonl"
            create_jobs_db(db_path)
            session_path.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "manual_session_id": "ghmanual_new_1",
                                "requested_url": "https://job-boards.greenhouse.io/example/jobs/12345?gh_src=test",
                                "public_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                                "confirmation_detected": True,
                                "submitted": True,
                            }
                        ),
                        json.dumps(
                            {
                                "manual_session_id": "ghmanual_new_2",
                                "requested_url": "https://job-boards.greenhouse.io/missing/jobs/99999",
                                "public_url": "https://job-boards.greenhouse.io/missing/jobs/99999",
                                "confirmation_detected": False,
                                "submitted": False,
                            }
                        ),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )

            result = backfill_jobs_db_from_manual_sessions(
                session_file=session_path,
                jobs_db_file=db_path,
                enabled=True,
            )

            self.assertTrue(result["stored"])
            self.assertEqual(result["attempted"], 2)
            self.assertEqual(result["stored_count"], 1)
            self.assertEqual(result["matched_count"], 1)

            conn = sqlite3.connect(str(db_path))
            try:
                row = conn.execute(
                    """
                    SELECT application_status, application_updated_at,
                           assistant_last_source, assistant_last_outcome,
                           assistant_last_manual_session_id,
                           assistant_last_confirmation_detected
                    FROM jobs
                    WHERE fingerprint = 'fp_c'
                    """
                ).fetchone()
            finally:
                conn.close()

            self.assertEqual(row[0], "applied")
            self.assertEqual(row[1], "2026-03-07T19:32:00+00:00")
            self.assertEqual(row[2], "manual_observer_backfill")
            self.assertEqual(row[3], "submitted")
            self.assertEqual(row[4], "ghmanual_new_1")
            self.assertEqual(row[5], 1)


if __name__ == "__main__":
    unittest.main()
