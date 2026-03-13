from __future__ import annotations

import unittest

from career_monitor.greenhouse_survey import summarize_survey


class GreenhouseSurveyTests(unittest.TestCase):
    def test_summarize_survey_aggregates_labels_and_types(self) -> None:
        job_records = [
            {
                "company": "Example A",
                "board": "examplea",
                "job_id": "1",
                "job_url": "https://job-boards.greenhouse.io/examplea/jobs/1",
                "title": "Engineer",
                "job_location": "Remote",
                "question_count": 2,
                "required_question_count": 1,
                "group_counts": {"questions": 2},
                "ui_type_counts": {"text_input": 1, "textarea": 1},
                "api_type_counts": {"input_text": 1, "textarea": 1},
                "questions": [
                    {
                        "label": "First Name",
                        "normalized_label": "first name",
                        "group": "questions",
                        "required": True,
                        "inputs": [{"ui_type": "text_input", "api_type": "input_text"}],
                    },
                    {
                        "label": "Why do you want to work here?",
                        "normalized_label": "why do you want to work here",
                        "group": "questions",
                        "required": False,
                        "inputs": [{"ui_type": "textarea", "api_type": "textarea"}],
                    },
                ],
            },
            {
                "company": "Example B",
                "board": "exampleb",
                "job_id": "2",
                "job_url": "https://job-boards.greenhouse.io/exampleb/jobs/2",
                "title": "QA Engineer",
                "job_location": "Remote",
                "question_count": 2,
                "required_question_count": 2,
                "group_counts": {"questions": 1, "compliance": 1},
                "ui_type_counts": {"text_input": 1, "combobox": 1},
                "api_type_counts": {"input_text": 1, "multi_value_single_select": 1},
                "questions": [
                    {
                        "label": "First Name",
                        "normalized_label": "first name",
                        "group": "questions",
                        "required": True,
                        "inputs": [{"ui_type": "text_input", "api_type": "input_text"}],
                    },
                    {
                        "label": "Will you require visa sponsorship?",
                        "normalized_label": "will you require visa sponsorship",
                        "group": "compliance",
                        "required": True,
                        "inputs": [
                            {
                                "ui_type": "combobox",
                                "api_type": "multi_value_single_select",
                            }
                        ],
                    },
                ],
            },
        ]
        board_summaries = [
            {"company": "Example A", "board": "examplea", "jobs_discovered": 1, "jobs_surveyed": 1, "jobs_failed": 0, "top_labels": []},
            {"company": "Example B", "board": "exampleb", "jobs_discovered": 1, "jobs_surveyed": 1, "jobs_failed": 0, "top_labels": []},
        ]
        report = summarize_survey(job_records, board_summaries=board_summaries, failures=[])

        self.assertEqual(report["boards_surveyed"], 2)
        self.assertEqual(report["jobs_surveyed"], 2)
        self.assertEqual(report["group_counts"]["questions"], 3)
        self.assertEqual(report["group_counts"]["compliance"], 1)
        self.assertEqual(report["ui_type_counts"]["text_input"], 2)
        self.assertEqual(report["api_type_counts"]["input_text"], 2)
        self.assertEqual(report["question_stats"]["questions_per_job_avg"], 2.0)
        self.assertEqual(report["top_question_labels"][0]["label"], "First Name")
        self.assertEqual(report["top_question_labels"][0]["count"], 2)
        normalized = {item["normalized_label"]: item for item in report["top_normalized_questions"]}
        self.assertEqual(normalized["first name"]["count"], 2)
        signatures = {item["signature"]: item["count"] for item in report["field_signatures"]}
        self.assertEqual(signatures["text_input:input_text"], 2)


if __name__ == "__main__":
    unittest.main()
