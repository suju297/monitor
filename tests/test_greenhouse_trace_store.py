from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from career_monitor.greenhouse_assistant import (
    GreenhouseSuggestedAnswer,
    analyze_greenhouse_application,
    build_approved_answer_targets,
    build_autofill_targets,
    load_greenhouse_application_schema,
)
from career_monitor.greenhouse_trace_store import (
    append_greenhouse_manual_observation,
    append_greenhouse_trace,
)

from test_greenhouse_assistant import sample_api_payload, sample_policy_payload


def _sample_result(analysis, profile, approved_answers):
    autofill_targets = build_autofill_targets(analysis, profile=profile)
    approved_targets = build_approved_answer_targets(
        analysis,
        approved_answers=approved_answers,
        profile=profile,
    )
    result = {
        "analysis": analysis.to_dict(),
        "page_url": analysis.schema.public_url,
        "form_visible": True,
        "filled": [
            {
                "question_label": "First Name",
                "api_name": "first_name",
                "ui_type": "text_input",
                "profile_key": "first_name",
                "value_source": "profile",
                "selector": "#first_name",
                "success": True,
                "value_preview": "Ada",
                "bound_via": "selector:#first_name",
                "verification": "Ada",
                "error": None,
            },
            {
                "question_label": "Why do you want to work here?",
                "api_name": "question_1",
                "ui_type": "textarea",
                "profile_key": "question_1",
                "value_source": "approved_answer",
                "selector": "#question_1",
                "success": True,
                "value_preview": "I want to build reliable systems.",
                "bound_via": "label_exact",
                "verification": "I want to build reliable systems.",
                "error": None,
            },
        ],
        "skipped": [],
        "errors": [],
        "browser_validation_errors": [],
        "submit_safety": {
            "eligible": True,
            "blockers": [],
            "requires_browser_validation": True,
            "eligible_after_browser_validation": True,
            "browser_blockers": [],
        },
        "submission": {
            "requested": False,
            "attempted": False,
            "submitted": False,
            "confirmation_detected": False,
        },
        "resume_selection": {
            "selected_resume_variant": profile.get("selected_resume_variant"),
            "selected_resume_path": profile.get("selected_resume_path") or profile.get("resume_path"),
            "selected_resume_file": Path(str(profile.get("selected_resume_path") or profile.get("resume_path") or "")).name or None,
            "resume_selection_source": profile.get("resume_selection_source"),
        },
        "trace": None,
    }
    return result, autofill_targets + approved_targets


class GreenhouseTraceStoreTests(unittest.TestCase):
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_append_greenhouse_trace_writes_run_and_question_records(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
            "selected_resume_variant": "ai",
            "selected_resume_path": "/tmp/resume.pdf",
            "resume_selection_source": "slm_recommended",
        }
        approved_answers = {
            "Why do you want to work here?": "I want to build reliable systems.",
        }
        analysis = analyze_greenhouse_application(
            schema,
            profile=profile,
            approved_answers=approved_answers,
        )
        result, fill_targets = _sample_result(analysis, profile, approved_answers)

        with tempfile.TemporaryDirectory() as tmpdir:
            trace_file = Path(tmpdir) / "run_traces.jsonl"
            examples_file = Path(tmpdir) / "training_examples.jsonl"

            metadata = append_greenhouse_trace(
                analysis=analysis,
                result=result,
                profile=profile,
                approved_answers=approved_answers,
                fill_targets=fill_targets,
                application_key="greenhouse:example:12345",
                requested_url=schema.public_url,
                trace_file=trace_file,
                training_examples_file=examples_file,
            )

            self.assertTrue(metadata["stored"])
            self.assertEqual(metadata["run_record_count"], 1)
            self.assertEqual(metadata["question_record_count"], len(analysis.schema.questions))

            run_records = [
                json.loads(line)
                for line in trace_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            question_records = [
                json.loads(line)
                for line in examples_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]

            self.assertEqual(len(run_records), 1)
            self.assertEqual(len(question_records), len(analysis.schema.questions))
            run_record = run_records[0]
            self.assertTrue(run_record["candidate"]["candidate_id"].startswith("cand_"))
            self.assertEqual(run_record["candidate"]["email_domain"], "example.com")
            self.assertNotIn("ada@example.com", json.dumps(run_record))
            self.assertEqual(run_record["run_resume_selection"]["selected_resume_variant"], "ai")
            self.assertEqual(run_record["run_resume_selection"]["resume_selection_source"], "slm_recommended")

            why_record = next(
                item for item in question_records if item["question_label"] == "Why do you want to work here?"
            )
            self.assertEqual(why_record["resolved_answer_source"], "approved_answer")
            self.assertEqual(why_record["resolved_answer"], "I want to build reliable systems.")
            self.assertEqual(why_record["execution_status"], "filled")

            first_name_record = next(
                item for item in question_records if item["question_label"] == "First Name"
            )
            self.assertEqual(first_name_record["resolved_answer_source"], "profile")
            self.assertIsNone(first_name_record["resolved_answer"])
            self.assertEqual(first_name_record["resolved_answer_preview"], "Ada")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_append_greenhouse_trace_stores_profile_knowledge_provenance(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload(with_consent=False)
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        analysis.suggested_answers = [
            GreenhouseSuggestedAnswer(
                question_label="Why do you want to work here?",
                api_name="question_1",
                value="I want to build reliable systems.",
                source="slm",
                reason="Supported by [chunk:pkc_mission].",
                confidence=0.81,
                draft_source="profile_knowledge",
                retrieved_chunk_ids=["pkc_mission"],
                style_snippet_ids=["pks_style"],
                retrieval_summary=["matched tags: ai, systems"],
            )
        ]
        approved_answers = {
            "Why do you want to work here?": "I want to build reliable systems.",
            "question_2": "No",
        }
        result, fill_targets = _sample_result(analysis, profile, approved_answers)

        with tempfile.TemporaryDirectory() as tmpdir:
            trace_file = Path(tmpdir) / "run_traces.jsonl"
            examples_file = Path(tmpdir) / "training_examples.jsonl"

            append_greenhouse_trace(
                analysis=analysis,
                result=result,
                profile=profile,
                approved_answers=approved_answers,
                fill_targets=fill_targets,
                application_key="greenhouse:example:12345",
                requested_url=schema.public_url,
                trace_file=trace_file,
                training_examples_file=examples_file,
            )

            question_records = [
                json.loads(line)
                for line in examples_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            why_record = next(
                item for item in question_records if item["question_label"] == "Why do you want to work here?"
            )

            self.assertEqual(why_record["draft_source"], "profile_knowledge")
            self.assertEqual(why_record["retrieved_chunk_ids"], ["pkc_mission"])
            self.assertEqual(why_record["style_snippet_ids"], ["pks_style"])
            self.assertEqual(why_record["retrieval_summary"], ["matched tags: ai, systems"])

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    @mock.patch(
        "career_monitor.greenhouse_trace_store.utc_now",
        side_effect=["2026-03-07T10:00:00+00:00", "2026-03-07T10:00:01+00:00"],
    )
    def test_append_greenhouse_trace_is_append_only(
        self,
        _mock_now: mock.Mock,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
        }
        approved_answers = {
            "Why do you want to work here?": "I want to build reliable systems.",
        }
        analysis = analyze_greenhouse_application(
            schema,
            profile=profile,
            approved_answers=approved_answers,
        )
        result, fill_targets = _sample_result(analysis, profile, approved_answers)

        with tempfile.TemporaryDirectory() as tmpdir:
            trace_file = Path(tmpdir) / "run_traces.jsonl"
            examples_file = Path(tmpdir) / "training_examples.jsonl"

            append_greenhouse_trace(
                analysis=analysis,
                result=result,
                profile=profile,
                approved_answers=approved_answers,
                fill_targets=fill_targets,
                application_key="greenhouse:example:12345",
                requested_url=schema.public_url,
                trace_file=trace_file,
                training_examples_file=examples_file,
            )
            append_greenhouse_trace(
                analysis=analysis,
                result=result,
                profile=profile,
                approved_answers=approved_answers,
                fill_targets=fill_targets,
                application_key="greenhouse:example:12345",
                requested_url=schema.public_url,
                trace_file=trace_file,
                training_examples_file=examples_file,
            )

            run_lines = [line for line in trace_file.read_text(encoding="utf-8").splitlines() if line.strip()]
            question_lines = [
                line for line in examples_file.read_text(encoding="utf-8").splitlines() if line.strip()
            ]

            self.assertEqual(len(run_lines), 2)
            self.assertEqual(len(question_lines), 2 * len(analysis.schema.questions))

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_append_greenhouse_manual_observation_tracks_resume_override_and_keeps_profile_field_values(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        payload = sample_policy_payload()
        payload["questions"].append(
            {
                "label": "Why do you want to work here?",
                "required": False,
                "fields": [{"name": "question_1", "type": "textarea", "values": []}],
            }
        )
        mock_fetch_api.return_value = payload
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "last_name": "Lovelace",
            "email": "ada@example.com",
            "resume_variants": {
                "ai": "/tmp/resume-ai.pdf",
                "cloud": "/tmp/resume-cloud.pdf",
            },
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        fill_targets = build_autofill_targets(analysis, profile=profile)
        observed_events = [
            {
                "sequence": 1,
                "offset_ms": 10,
                "event_type": "focus",
                "page_url": schema.public_url,
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
                "offset_ms": 50,
                "event_type": "change",
                "page_url": schema.public_url,
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
                "sequence": 3,
                "offset_ms": 100,
                "event_type": "change",
                "page_url": schema.public_url,
                "question_label": "Resume/CV",
                "api_name": "resume",
                "element_id": "resume",
                "element_name": "resume",
                "tag_name": "input",
                "input_type": "file",
                "required": True,
                "value": "",
                "checked": None,
                "file_names": ["resume-cloud.pdf"],
            },
            {
                "sequence": 4,
                "offset_ms": 150,
                "event_type": "change",
                "page_url": schema.public_url,
                "question_label": "Why do you want to work here?",
                "api_name": "question_1",
                "element_id": "question_1",
                "element_name": "question_1",
                "tag_name": "textarea",
                "input_type": "",
                "required": False,
                "value": "I like the mission and the product quality bar.",
                "checked": None,
                "file_names": [],
            },
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            session_file = Path(tmpdir) / "manual_sessions.jsonl"
            field_events_file = Path(tmpdir) / "manual_field_events.jsonl"

            metadata = append_greenhouse_manual_observation(
                analysis=analysis,
                observed_events=observed_events,
                profile=profile,
                fill_targets=fill_targets,
                application_key="greenhouse:example:77777",
                requested_url=schema.public_url,
                final_page_url=schema.public_url,
                confirmation_detected=False,
                ended_reason="page_closed",
                resume_decision_source="manual",
                external_resume_recommendation="ai",
                session_file=session_file,
                field_events_file=field_events_file,
            )

            self.assertTrue(metadata["stored"])
            session_records = [
                json.loads(line)
                for line in session_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            field_records = [
                json.loads(line)
                for line in field_events_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]

            self.assertEqual(len(session_records), 1)
            session_record = session_records[0]
            self.assertEqual(session_record["assistant_recommendation"]["resume_variant"], "ai")
            self.assertEqual(session_record["manual_resume_decision"]["selected_variant"], "cloud")
            self.assertFalse(session_record["manual_resume_decision"]["matches_assistant_recommendation"])
            self.assertEqual(session_record["manual_resume_decision"]["decision_source"], "manual")
            self.assertEqual(session_record["manual_resume_decision"]["external_recommendation"], "ai")

            first_name_record = next(
                item
                for item in field_records
                if item["question_label"] == "First Name" and item["event_type"] == "change"
            )
            self.assertEqual(first_name_record["stored_value"], "Ada")
            self.assertEqual(first_name_record["value_preview"], "Ada")
            self.assertIsNone(first_name_record["recommended_value_source"])
            self.assertIsNotNone(first_name_record["value_hash"])

            why_record = next(item for item in field_records if item["question_label"] == "Why do you want to work here?")
            self.assertEqual(why_record["stored_value"], "I like the mission and the product quality bar.")
            self.assertEqual(why_record["decision_action"], "QUEUE")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_manual_observation_links_final_answers_to_profile_knowledge_chunks(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        payload = sample_api_payload(with_consent=False)
        mock_fetch_api.return_value = payload
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        analysis.suggested_answers = [
            GreenhouseSuggestedAnswer(
                question_label="Why do you want to work here?",
                api_name="question_1",
                value="I want to build reliable systems.",
                source="slm",
                reason="Supported by [chunk:pkc_mission].",
                confidence=0.81,
                draft_source="profile_knowledge",
                retrieved_chunk_ids=["pkc_mission"],
                style_snippet_ids=["pks_style"],
                retrieval_summary=["matched tags: ai, systems"],
            )
        ]
        fill_targets = build_autofill_targets(analysis, profile=profile)
        observed_events = [
            {
                "sequence": 1,
                "offset_ms": 10,
                "event_type": "change",
                "page_url": schema.public_url,
                "question_label": "Why do you want to work here?",
                "api_name": "question_1",
                "element_id": "question_1",
                "element_name": "question_1",
                "tag_name": "textarea",
                "input_type": "",
                "required": False,
                "value": "I like the mission and the product quality bar.",
                "checked": None,
                "file_names": [],
            },
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            session_file = Path(tmpdir) / "manual_sessions.jsonl"
            field_events_file = Path(tmpdir) / "manual_field_events.jsonl"

            append_greenhouse_manual_observation(
                analysis=analysis,
                observed_events=observed_events,
                profile=profile,
                fill_targets=fill_targets,
                application_key="greenhouse:example:12345",
                requested_url=schema.public_url,
                final_page_url=schema.public_url,
                confirmation_detected=False,
                ended_reason="page_closed",
                session_file=session_file,
                field_events_file=field_events_file,
            )

            field_records = [
                json.loads(line)
                for line in field_events_file.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            why_record = field_records[0]
            self.assertEqual(why_record["draft_source"], "profile_knowledge")
            self.assertEqual(why_record["retrieved_chunk_ids"], ["pkc_mission"])
            self.assertEqual(why_record["style_snippet_ids"], ["pks_style"])


if __name__ == "__main__":
    unittest.main()
