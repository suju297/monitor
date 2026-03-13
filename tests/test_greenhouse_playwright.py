from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest import mock

from career_monitor.greenhouse_assistant import GreenhouseFillTarget, GreenhouseOption
from career_monitor.greenhouse_playwright import (
    _GreenhouseSharedBrowserSession,
    _apply_fill_targets,
    _browser_launch_kwargs,
    _build_submit_blockers,
    _context_storage_state_kwargs,
    _evaluate_confirmation_signals,
    _resolve_selected_options,
    _should_wait_for_manual_review,
    _wait_for_manual_review_completion,
    _truthy_choice_answer,
)
from career_monitor.greenhouse_submission_state import (
    build_greenhouse_application_key,
    get_submission_record,
    load_greenhouse_submission_state,
    record_submission,
    save_greenhouse_submission_state,
)


class GreenhousePlaywrightHelperTests(unittest.TestCase):
    @mock.patch("career_monitor.greenhouse_playwright._default_chrome_executable_path")
    def test_browser_launch_kwargs_prefers_installed_chrome_for_headed_runs(
        self,
        mock_default_chrome_executable_path: mock.Mock,
    ) -> None:
        mock_default_chrome_executable_path.return_value = (
            "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        )

        with mock.patch.dict("os.environ", {}, clear=False):
            self.assertEqual(
                _browser_launch_kwargs(headless=False),
                {
                    "headless": False,
                    "executable_path": "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
                },
            )

    def test_browser_launch_kwargs_can_force_chromium(self) -> None:
        with mock.patch.dict("os.environ", {"GREENHOUSE_BROWSER": "chromium"}, clear=False):
            self.assertEqual(_browser_launch_kwargs(headless=False), {"headless": False})

    def test_context_storage_state_kwargs_uses_existing_file_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            missing = Path(tmpdir) / "missing.json"
            existing = Path(tmpdir) / "state.json"
            existing.write_text("{}", encoding="utf-8")

            self.assertEqual(_context_storage_state_kwargs(missing), {})
            self.assertEqual(_context_storage_state_kwargs(existing), {"storage_state": str(existing)})

    def test_shared_browser_session_opens_new_tab_from_existing_page(self) -> None:
        session = _GreenhouseSharedBrowserSession.__new__(_GreenhouseSharedBrowserSession)

        class FakeLock:
            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc, tb):
                return False

        class FakePage:
            def __init__(self) -> None:
                self._closed = False
                self.evaluate_calls = 0
                self.wait_calls = 0

            def is_closed(self) -> bool:
                return self._closed

            def evaluate(self, _script: str) -> None:
                self.evaluate_calls += 1
                pages.append(new_page)

            def wait_for_timeout(self, _milliseconds: int) -> None:
                self.wait_calls += 1

        class FakeContext:
            @property
            def pages(self):
                return pages

            def new_page(self):
                return fallback_page

        session._lock = FakeLock()
        opener = FakePage()
        new_page = FakePage()
        fallback_page = FakePage()
        pages = [opener]
        session._context = FakeContext()

        opened = session.new_page()

        self.assertIs(opened, new_page)
        self.assertEqual(opener.evaluate_calls, 1)

    def test_should_wait_for_manual_review_only_for_headed_unsubmitted_runs(self) -> None:
        self.assertTrue(
            _should_wait_for_manual_review(headless=False, keep_open_for_review=True, submitted=False)
        )
        self.assertFalse(
            _should_wait_for_manual_review(headless=True, keep_open_for_review=True, submitted=False)
        )
        self.assertFalse(
            _should_wait_for_manual_review(headless=False, keep_open_for_review=True, submitted=True)
        )
        self.assertFalse(
            _should_wait_for_manual_review(headless=False, keep_open_for_review=False, submitted=False)
        )

    def test_truthy_choice_answer_normalizes_yes_and_no_values(self) -> None:
        self.assertTrue(_truthy_choice_answer("Yes"))
        self.assertTrue(_truthy_choice_answer("opt in"))
        self.assertFalse(_truthy_choice_answer("No"))
        self.assertFalse(_truthy_choice_answer("opt out"))
        self.assertIsNone(_truthy_choice_answer("Maybe"))

    def test_resolve_selected_options_matches_multi_select_values(self) -> None:
        target = GreenhouseFillTarget(
            question_label="Privacy choices",
            api_name="question_privacy",
            ui_type="checkbox_group",
            selector="[name='question_privacy[]']",
            profile_key="question_privacy",
            value="Phone; Email",
            options=[
                GreenhouseOption(label="Email", value="email"),
                GreenhouseOption(label="Phone", value="phone"),
                GreenhouseOption(label="SMS", value="sms"),
            ],
            value_source="approved_answer",
        )

        self.assertEqual(
            _resolve_selected_options(target),
            [("Phone", "phone"), ("Email", "email")],
        )

    def test_resolve_selected_options_uses_single_checkbox_for_yes_answer(self) -> None:
        target = GreenhouseFillTarget(
            question_label="Privacy policy",
            api_name="question_privacy",
            ui_type="checkbox_group",
            selector="[name='question_privacy[]']",
            profile_key="question_privacy",
            value="Yes",
            options=[GreenhouseOption(label="I acknowledge", value="ack")],
            value_source="approved_answer",
        )

        self.assertEqual(
            _resolve_selected_options(target),
            [("I acknowledge", "ack")],
        )

    def test_confirmation_signals_accept_confirmation_url(self) -> None:
        confirmed, note = _evaluate_confirmation_signals(
            current_url="https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
            previous_url="https://job-boards.greenhouse.io/example/jobs/12345",
            title="Apply",
            page_text="Thanks",
            form_visible=True,
        )

        self.assertTrue(confirmed)
        self.assertEqual(note, "Confirmation URL detected after submit.")

    def test_confirmation_signals_accept_thank_you_copy(self) -> None:
        confirmed, note = _evaluate_confirmation_signals(
            current_url="https://job-boards.greenhouse.io/example/jobs/12345",
            previous_url="https://job-boards.greenhouse.io/example/jobs/12345",
            title="Thank you for applying",
            page_text="Your application has been submitted.",
            form_visible=False,
        )

        self.assertTrue(confirmed)
        self.assertEqual(note, "Confirmation text detected after submit.")

    @mock.patch("career_monitor.greenhouse_playwright._collect_challenge_blockers")
    @mock.patch("career_monitor.greenhouse_playwright._fill_target")
    def test_apply_fill_targets_continues_in_supervised_mode_when_challenge_visible(
        self,
        mock_fill_target: mock.Mock,
        mock_collect_challenge_blockers: mock.Mock,
    ) -> None:
        class FakePage:
            def wait_for_timeout(self, _milliseconds: int) -> None:
                return None

        mock_fill_target.side_effect = [
            mock.Mock(success=True),
            mock.Mock(success=True),
        ]
        mock_collect_challenge_blockers.return_value = ["Visible CAPTCHA challenge detected."]
        targets = [
            GreenhouseFillTarget(
                question_label="First Name",
                api_name="first_name",
                ui_type="text_input",
                selector="#first_name",
                profile_key="first_name",
                value="Ada",
            ),
            GreenhouseFillTarget(
                question_label="Last Name",
                api_name="last_name",
                ui_type="text_input",
                selector="#last_name",
                profile_key="last_name",
                value="Lovelace",
            ),
        ]

        filled, skipped, blockers = _apply_fill_targets(
            FakePage(),
            targets,
            timeout_ms=1000,
            safe_interaction_mode=True,
            stop_on_challenge=False,
        )

        self.assertEqual(len(filled), 2)
        self.assertEqual(len(skipped), 0)
        self.assertEqual(blockers, ["Visible CAPTCHA challenge detected."])
        self.assertEqual(mock_fill_target.call_count, 2)

    @mock.patch("career_monitor.greenhouse_playwright._collect_challenge_blockers")
    @mock.patch("career_monitor.greenhouse_playwright._fill_target")
    def test_apply_fill_targets_stops_early_when_challenge_is_hard_blocker(
        self,
        mock_fill_target: mock.Mock,
        mock_collect_challenge_blockers: mock.Mock,
    ) -> None:
        class FakePage:
            def wait_for_timeout(self, _milliseconds: int) -> None:
                return None

        mock_fill_target.side_effect = [
            mock.Mock(success=True),
            mock.Mock(success=True),
        ]
        mock_collect_challenge_blockers.return_value = ["Visible CAPTCHA challenge detected."]
        targets = [
            GreenhouseFillTarget(
                question_label="First Name",
                api_name="first_name",
                ui_type="text_input",
                selector="#first_name",
                profile_key="first_name",
                value="Ada",
            ),
            GreenhouseFillTarget(
                question_label="Last Name",
                api_name="last_name",
                ui_type="text_input",
                selector="#last_name",
                profile_key="last_name",
                value="Lovelace",
            ),
        ]

        filled, skipped, blockers = _apply_fill_targets(
            FakePage(),
            targets,
            timeout_ms=1000,
            safe_interaction_mode=True,
            stop_on_challenge=True,
        )

        self.assertEqual(len(filled), 1)
        self.assertEqual(len(skipped), 0)
        self.assertEqual(blockers, ["Visible CAPTCHA challenge detected."])
        self.assertEqual(mock_fill_target.call_count, 1)

    def test_submit_blockers_respect_duplicate_and_button_state(self) -> None:
        blockers = _build_submit_blockers(
            {
                "eligible": True,
                "eligible_after_browser_validation": True,
                "blockers": [],
                "browser_blockers": [],
            },
            duplicate_record={
                "submitted_at": "2026-03-07T10:00:00+00:00",
            },
            challenge_blockers=["Visible verification challenge detected."],
            submit_button_found=True,
            submit_button_enabled=False,
        )

        self.assertIn("Application was already submitted on 2026-03-07T10:00:00+00:00.", blockers)
        self.assertIn("Submit button is disabled.", blockers)
        self.assertIn("Visible verification challenge detected.", blockers)

    @mock.patch("career_monitor.greenhouse_playwright._live_confirmation_state")
    @mock.patch("career_monitor.greenhouse_playwright.drain_greenhouse_page_observer")
    def test_wait_for_manual_review_completion_returns_observed_events_on_confirmation(
        self,
        mock_drain: mock.Mock,
        mock_live_confirmation_state: mock.Mock,
    ) -> None:
        class FakePage:
            def is_closed(self) -> bool:
                return False

            def wait_for_timeout(self, _milliseconds: int) -> None:
                raise AssertionError("wait_for_timeout should not be called after immediate confirmation")

        mock_drain.side_effect = [
            [{"event_type": "change", "question_label": "Phone"}],
            [{"event_type": "submit_click", "question_label": "Submit"}],
        ]
        mock_live_confirmation_state.return_value = (
            True,
            "Confirmation text detected after submit.",
            "https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
        )

        outcome = _wait_for_manual_review_completion(
            FakePage(),
            previous_url="https://job-boards.greenhouse.io/example/jobs/12345",
        )

        self.assertTrue(outcome["confirmation_detected"])
        self.assertEqual(len(outcome["observed_events"]), 2)
        self.assertEqual(outcome["observed_events"][0]["event_type"], "change")
        self.assertEqual(outcome["observed_events"][1]["event_type"], "submit_click")


class GreenhouseSubmissionStateTests(unittest.TestCase):
    def test_build_greenhouse_application_key_prefers_board_and_job_id(self) -> None:
        key = build_greenhouse_application_key(
            board_token="example",
            job_id="12345",
            public_url="https://job-boards.greenhouse.io/example/jobs/12345",
        )

        self.assertEqual(key, "greenhouse:example:12345")

    def test_record_submission_round_trip(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            state_path = Path(tmpdir) / "greenhouse_submissions.json"
            state = load_greenhouse_submission_state(state_path)
            application_key = build_greenhouse_application_key(
                board_token="example",
                job_id="12345",
                public_url="https://job-boards.greenhouse.io/example/jobs/12345",
            )

            record_submission(
                state,
                application_key=application_key,
                board_token="example",
                job_id="12345",
                company_name="Example Co",
                title="Software Engineer",
                public_url="https://job-boards.greenhouse.io/example/jobs/12345",
                confirmation_url="https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
                confirmation_note="Confirmation text detected after submit.",
                page_url="https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
            )
            save_greenhouse_submission_state(state_path, state)

            reloaded = load_greenhouse_submission_state(state_path)
            record = get_submission_record(reloaded, application_key=application_key)

            assert record is not None
            self.assertEqual(record["company_name"], "Example Co")
            self.assertEqual(record["title"], "Software Engineer")
            self.assertEqual(
                record["confirmation_url"],
                "https://job-boards.greenhouse.io/example/jobs/12345/confirmation",
            )
            self.assertIsNotNone(record["submitted_at"])


if __name__ == "__main__":
    unittest.main()
