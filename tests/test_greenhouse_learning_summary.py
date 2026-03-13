from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from career_monitor.greenhouse_learning_summary import greenhouse_learning_summary_payload


class GreenhouseLearningSummaryTests(unittest.TestCase):
    def test_learning_summary_surfaces_overrides_acceptances_and_defaults(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            session_path = Path(tmpdir) / "sessions.jsonl"
            events_path = Path(tmpdir) / "events.jsonl"

            sessions = [
                {
                    "record_type": "greenhouse_manual_session",
                    "manual_session_id": "s1",
                    "captured_at": "2026-03-08T02:00:00+00:00",
                    "application_key": "greenhouse:test:1",
                    "company_name": "Example Co",
                    "job_title": "Platform Engineer",
                    "submitted": True,
                    "manual_resume_decision": {"decision_source": "assistant_review"},
                },
                {
                    "record_type": "greenhouse_manual_session",
                    "manual_session_id": "s2",
                    "captured_at": "2026-03-08T03:00:00+00:00",
                    "application_key": "greenhouse:test:2",
                    "company_name": "Other Co",
                    "job_title": "Platform Engineer",
                    "submitted": False,
                    "manual_resume_decision": {"decision_source": "manual"},
                },
                {
                    "record_type": "greenhouse_manual_session",
                    "manual_session_id": "s3",
                    "captured_at": "2026-03-08T04:00:00+00:00",
                    "application_key": "greenhouse:test:3",
                    "company_name": "Third Co",
                    "job_title": "Platform Engineer",
                    "submitted": True,
                    "manual_resume_decision": {"decision_source": "assistant_review"},
                },
            ]
            session_path.write_text("".join(json.dumps(item) + "\n" for item in sessions), encoding="utf-8")

            events = [
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s1",
                    "sequence": 1,
                    "event_type": "blur",
                    "normalized_label": "how did you hear about this opportunity",
                    "question_label": "How did you hear about this opportunity?",
                    "question_group": "questions",
                    "company_name": "Example Co",
                    "captured_at": "2026-03-08T02:00:00+00:00",
                    "stored_value": "LinkedIn",
                    "recommended_value_preview": None,
                    "recommended_value_source": None,
                    "matched_recommendation": None,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s2",
                    "sequence": 1,
                    "event_type": "blur",
                    "normalized_label": "how did you hear about this opportunity",
                    "question_label": "How did you hear about this opportunity?",
                    "question_group": "questions",
                    "company_name": "Other Co",
                    "captured_at": "2026-03-08T03:00:00+00:00",
                    "stored_value": "LinkedIn",
                    "recommended_value_preview": None,
                    "recommended_value_source": None,
                    "matched_recommendation": None,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s1",
                    "sequence": 2,
                    "event_type": "blur",
                    "normalized_label": "do you have experience with csp iam and kubernetes rbac",
                    "question_label": "Do you have experience with CSP IAM and Kubernetes RBAC?",
                    "question_group": "questions",
                    "company_name": "Example Co",
                    "captured_at": "2026-03-08T02:00:00+00:00",
                    "stored_value": "Yes",
                    "recommended_value_preview": "No experience with CSP IAM and Kubernetes RBAC.",
                    "recommended_value_source": "slm_suggestion",
                    "matched_recommendation": False,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s2",
                    "sequence": 2,
                    "event_type": "blur",
                    "normalized_label": "do you have experience with csp iam and kubernetes rbac",
                    "question_label": "Do you have experience with CSP IAM and Kubernetes RBAC?",
                    "question_group": "questions",
                    "company_name": "Other Co",
                    "captured_at": "2026-03-08T03:00:00+00:00",
                    "stored_value": "Yes",
                    "recommended_value_preview": "No experience with CSP IAM and Kubernetes RBAC.",
                    "recommended_value_source": "slm_suggestion",
                    "matched_recommendation": False,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s1",
                    "sequence": 3,
                    "event_type": "blur",
                    "normalized_label": "do you have experience with operating kubernetes clusters in production",
                    "question_label": "Do you have experience with operating Kubernetes clusters in production?",
                    "question_group": "questions",
                    "company_name": "Example Co",
                    "captured_at": "2026-03-08T02:00:00+00:00",
                    "stored_value": "Yes",
                    "recommended_value_preview": "Yes",
                    "recommended_value_source": "slm_suggestion",
                    "matched_recommendation": True,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s2",
                    "sequence": 3,
                    "event_type": "blur",
                    "normalized_label": "do you have experience with operating kubernetes clusters in production",
                    "question_label": "Do you have experience with operating Kubernetes clusters in production?",
                    "question_group": "questions",
                    "company_name": "Other Co",
                    "captured_at": "2026-03-08T03:00:00+00:00",
                    "stored_value": "Yes",
                    "recommended_value_preview": "Yes",
                    "recommended_value_source": "slm_suggestion",
                    "matched_recommendation": True,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s3",
                    "sequence": 3,
                    "event_type": "blur",
                    "normalized_label": "do you have experience with operating kubernetes clusters in production",
                    "question_label": "Do you have experience with operating Kubernetes clusters in production?",
                    "question_group": "questions",
                    "company_name": "Third Co",
                    "captured_at": "2026-03-08T04:00:00+00:00",
                    "stored_value": "Yes",
                    "recommended_value_preview": "Yes",
                    "recommended_value_source": "slm_suggestion",
                    "matched_recommendation": True,
                },
                {
                    "record_type": "greenhouse_manual_field_event",
                    "manual_session_id": "s3",
                    "sequence": 4,
                    "event_type": "blur",
                    "normalized_label": "race",
                    "question_label": "Race",
                    "question_group": "demographic_questions",
                    "company_name": "Third Co",
                    "captured_at": "2026-03-08T04:00:00+00:00",
                    "stored_value": "Decline To Self Identify",
                    "recommended_value_preview": None,
                    "recommended_value_source": None,
                    "matched_recommendation": None,
                },
            ]
            events_path.write_text("".join(json.dumps(item) + "\n" for item in events), encoding="utf-8")

            payload = greenhouse_learning_summary_payload(
                session_file=session_path,
                field_events_file=events_path,
                limit=10,
            )

            self.assertTrue(payload["ok"])
            self.assertEqual(payload["summary"]["session_count"], 3)
            self.assertEqual(payload["summary"]["submitted_count"], 2)
            self.assertEqual(payload["summary"]["assistant_review_session_count"], 2)
            self.assertEqual(payload["summary"]["matched_recommendation_count"], 3)
            self.assertEqual(payload["summary"]["override_count"], 2)
            self.assertEqual(payload["summary"]["manual_only_answer_count"], 2)

            top_overrides = {item["normalized_label"]: item for item in payload["top_overrides"]}
            self.assertIn("do you have experience with csp iam and kubernetes rbac", top_overrides)
            self.assertEqual(
                top_overrides["do you have experience with csp iam and kubernetes rbac"]["top_final_answers"][0]["value"],
                "Yes",
            )

            stable_defaults = {item["normalized_label"]: item for item in payload["stable_defaults"]}
            self.assertIn("how did you hear about this opportunity", stable_defaults)
            self.assertEqual(
                stable_defaults["how did you hear about this opportunity"]["top_final_answers"][0]["value"],
                "LinkedIn",
            )
            self.assertNotIn("race", stable_defaults)

            top_acceptances = {item["normalized_label"]: item for item in payload["top_acceptances"]}
            self.assertIn("do you have experience with operating kubernetes clusters in production", top_acceptances)
            self.assertEqual(
                top_acceptances["do you have experience with operating kubernetes clusters in production"]["acceptance_rate"],
                1.0,
            )

            learning_candidates = {item["normalized_label"]: item for item in payload["learning_candidates"]}
            self.assertEqual(
                learning_candidates["do you have experience with csp iam and kubernetes rbac"]["candidate"]["type"],
                "promote_default",
            )
            self.assertEqual(
                learning_candidates["how did you hear about this opportunity"]["candidate"]["type"],
                "candidate_profile_default",
            )


if __name__ == "__main__":
    unittest.main()
