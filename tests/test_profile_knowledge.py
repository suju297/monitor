from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from career_monitor.greenhouse_assistant import _call_greenhouse_slm, load_greenhouse_application_schema
from career_monitor.profile_knowledge import (
    ProfileRetrievalItem,
    ProfileRetrievalResult,
    build_profile_knowledge_index,
    retrieve_profile_knowledge,
)

from test_greenhouse_assistant import sample_anthropic_like_payload, sample_grafana_payload


def _write_profile_file(directory: Path, name: str, text: str) -> None:
    (directory / name).write_text(text, encoding="utf-8")


class _FakeResponse:
    def __init__(self, payload: dict[str, object]) -> None:
        self._payload = payload

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        return None

    def read(self) -> bytes:
        return json.dumps(self._payload).encode("utf-8")


class ProfileKnowledgeTests(unittest.TestCase):
    def test_build_profile_knowledge_index_creates_artifacts_and_stable_ids(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            profile_dir = Path(tmpdir) / "profile"
            profile_dir.mkdir(parents=True, exist_ok=True)
            _write_profile_file(
                profile_dir,
                "master-experience.md",
                """# Summary

## Professional Summary
I build reliable AI, cloud, and full stack systems. I work in first person, keep answers grounded, and prefer clear, direct language with practical engineering detail.

## Experience
### AI-CARING
I built an end-to-end reminder system with LangChain, code generation, evaluation, and user studies. I improved correctness through evaluation-driven redesign and grounded trigger generation.""",
            )
            _write_profile_file(
                profile_dir,
                "gcp-infra.md",
                """# Cloud Infra Project Summary

## What Was Implemented
Built Terraform-managed cloud infrastructure, a serverless function, and Kubernetes-adjacent deployment workflows. The project focused on infra automation, packaging, and reproducible deployment on GCP with strong validation.""",
            )

            chunks_file = Path(tmpdir) / "chunks.jsonl"
            style_file = Path(tmpdir) / "style.jsonl"
            manifest_file = Path(tmpdir) / "manifest.json"

            chunks_a, snippets_a, manifest_a = build_profile_knowledge_index(
                profile_dir=profile_dir,
                chunks_file=chunks_file,
                style_file=style_file,
                manifest_file=manifest_file,
            )
            chunks_b, snippets_b, manifest_b = build_profile_knowledge_index(
                profile_dir=profile_dir,
                chunks_file=chunks_file,
                style_file=style_file,
                manifest_file=manifest_file,
            )

            self.assertGreaterEqual(manifest_a.chunk_count, 2)
            self.assertGreaterEqual(manifest_a.style_snippet_count, 1)
            self.assertTrue(chunks_file.exists())
            self.assertTrue(style_file.exists())
            self.assertTrue(manifest_file.exists())
            self.assertEqual([item.chunk_id for item in chunks_a], [item.chunk_id for item in chunks_b])
            self.assertEqual([item.snippet_id for item in snippets_a], [item.snippet_id for item in snippets_b])
            self.assertEqual(manifest_a.chunk_count, manifest_b.chunk_count)
            self.assertTrue(any("ai" in item.topic_tags for item in chunks_a))
            self.assertTrue(any(item.source_file == "master-experience.md" for item in snippets_a))

    def test_retrieve_profile_knowledge_prefers_matching_domain_chunks(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            profile_dir = Path(tmpdir) / "profile"
            profile_dir.mkdir(parents=True, exist_ok=True)
            _write_profile_file(
                profile_dir,
                "master-experience.md",
                """# Summary

## Professional Summary
I build LLM systems, prompt pipelines, evaluation workflows, and user-facing AI applications. My writing style is concise, factual, and first-person.

## AI-CARING
Built a LangChain-based reminder system with structured intent extraction, code generation, evaluation, and two user studies. Improved correctness from 38.6% to 88.3%.""",
            )
            _write_profile_file(
                profile_dir,
                "gcp-infra.md",
                """# Cloud Infra Project Summary

## What Was Implemented
Implemented Terraform infrastructure, packaging, and deployment automation for a cloud application. Worked with infra validation, GCP services, deployment pipelines, and Kubernetes-oriented operational concerns.""",
            )

            infra_result = retrieve_profile_knowledge(
                question_label="Describe your Kubernetes or cloud infrastructure experience",
                job_title="Platform Engineer",
                job_excerpt="The role focuses on infrastructure, Terraform, cloud deployment, and Kubernetes operations.",
                selected_resume_variant="cloud",
                profile_dir=profile_dir,
            )
            ai_result = retrieve_profile_knowledge(
                question_label="Have you worked with LLMs in research or production?",
                job_title="Applied AI Engineer",
                job_excerpt="The role requires LLM pipelines, prompt engineering, and evaluation.",
                selected_resume_variant="ai",
                profile_dir=profile_dir,
            )

            self.assertTrue(infra_result.has_strong_evidence)
            self.assertEqual(infra_result.evidence_chunks[0].source_file, "gcp-infra.md")
            self.assertTrue(any("cloud" in item for item in infra_result.retrieval_summary))
            self.assertTrue(ai_result.has_strong_evidence)
            self.assertEqual(ai_result.evidence_chunks[0].source_file, "master-experience.md")
            self.assertTrue(any(hit.source_file == "master-experience.md" for hit in ai_result.style_snippets))

    @mock.patch("career_monitor.greenhouse_assistant.urllib.request.urlopen")
    @mock.patch("career_monitor.greenhouse_assistant.retrieve_profile_knowledge")
    @mock.patch("career_monitor.greenhouse_assistant._greenhouse_slm_enabled")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_call_greenhouse_slm_includes_retrieved_profile_context(
        self,
        mock_fetch_api: mock.Mock,
        mock_slm_enabled: mock.Mock,
        mock_retrieve: mock.Mock,
        mock_urlopen: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_grafana_payload()
        mock_slm_enabled.return_value = True
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/grafanalabs/jobs/5809023004",
            session=object(),  # type: ignore[arg-type]
        )
        question = next(
            item
            for item in schema.questions
            if item.label == "Do you have experience with provisioning Kubernetes clusters and operating in production?"
        )
        mock_retrieve.return_value = ProfileRetrievalResult(
            evidence_chunks=[
                ProfileRetrievalItem(
                    item_id="pkc_infra",
                    source_file="gcp-infra.md",
                    section_title="What Was Implemented",
                    text="Implemented Terraform and cloud infrastructure automation with Kubernetes-oriented deployment workflows.",
                    word_count=13,
                    topic_tags=["cloud", "infra", "kubernetes"],
                    score=7.2,
                    reasons=["tags:cloud, infra, kubernetes"],
                )
            ],
            style_snippets=[
                ProfileRetrievalItem(
                    item_id="pks_style",
                    source_file="master-experience.md",
                    section_title="Professional Summary",
                    text="I write clearly and ground answers in shipped systems.",
                    word_count=10,
                    topic_tags=["ai"],
                    score=2.8,
                    reasons=["preferred_style_source"],
                )
            ],
            retrieval_summary=["matched tags: cloud, infra, kubernetes"],
            matched_tags=["cloud", "infra", "kubernetes"],
            has_strong_evidence=True,
        )
        captured_body: dict[str, object] = {}

        def _fake_urlopen(request, timeout=None):
            del timeout
            captured_body.update(json.loads(request.data.decode("utf-8")))
            return _FakeResponse(
                {
                    "message": {
                        "content": json.dumps(
                            {
                                "should_fill": True,
                                "answer": "Yes",
                                "confidence": 0.83,
                                "reason": "Supported by [chunk:pkc_infra].",
                            }
                        )
                    }
                }
            )

        mock_urlopen.side_effect = _fake_urlopen

        suggestion = _call_greenhouse_slm(
            schema=schema,
            question=question,
            profile={"selected_resume_variant": "cloud"},
        )

        self.assertIsNotNone(suggestion)
        assert suggestion is not None
        self.assertTrue(suggestion.value.startswith("Yes — Implemented Terraform and cloud infrastructure automation"))
        self.assertEqual(suggestion.draft_source, "profile_knowledge")
        self.assertEqual(suggestion.retrieved_chunk_ids, ["pkc_infra"])
        self.assertEqual(suggestion.style_snippet_ids, ["pks_style"])
        self.assertEqual(captured_body, {})
        mock_urlopen.assert_not_called()

    @mock.patch("career_monitor.greenhouse_assistant.urllib.request.urlopen")
    @mock.patch("career_monitor.greenhouse_assistant.retrieve_profile_knowledge")
    @mock.patch("career_monitor.greenhouse_assistant._greenhouse_slm_enabled")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_call_greenhouse_slm_returns_none_when_retrieval_is_weak(
        self,
        mock_fetch_api: mock.Mock,
        mock_slm_enabled: mock.Mock,
        mock_retrieve: mock.Mock,
        mock_urlopen: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_anthropic_like_payload()
        mock_slm_enabled.return_value = True
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/anthropic/jobs/5098984008",
            session=object(),  # type: ignore[arg-type]
        )
        question = next(item for item in schema.questions if item.label == "Why Anthropic?")
        mock_retrieve.return_value = ProfileRetrievalResult(
            evidence_chunks=[],
            style_snippets=[],
            retrieval_summary=[],
            matched_tags=[],
            has_strong_evidence=False,
        )

        suggestion = _call_greenhouse_slm(
            schema=schema,
            question=question,
            profile={"selected_resume_variant": "ai"},
        )

        self.assertIsNone(suggestion)
        mock_urlopen.assert_not_called()


if __name__ == "__main__":
    unittest.main()
