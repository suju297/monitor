from __future__ import annotations

import json
import re
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

from .utils import utc_now

DEFAULT_PROFILE_CORPUS_DIR = "profile"
DEFAULT_PROFILE_KNOWLEDGE_CHUNKS_FILE = ".state/profile_knowledge_chunks.jsonl"
DEFAULT_PROFILE_STYLE_SNIPPETS_FILE = ".state/profile_style_snippets.jsonl"
DEFAULT_PROFILE_KNOWLEDGE_MANIFEST_FILE = ".state/profile_knowledge_manifest.json"

_INDEX_CACHE: dict[tuple[tuple[str, int, int], ...], tuple[list["ProfileKnowledgeChunk"], list["ProfileStyleSnippet"], "ProfileKnowledgeManifest"]] = {}

_TOPIC_KEYWORDS: dict[str, tuple[str, ...]] = {
    "ai": ("ai", "artificial intelligence", "applied ai", "ai engineer", "ai system"),
    "llm": ("llm", "language model", "langchain", "gpt", "claude", "prompt", "rag", "agent"),
    "evaluation": ("evaluation", "benchmark", "metrics", "correct reminders", "user studies", "quality"),
    "infra": ("infrastructure", "platform", "reliability", "scalability", "sre", "systems"),
    "cloud": ("cloud", "aws", "gcp", "azure", "s3", "terraform", "packer"),
    "kubernetes": ("kubernetes", "helm", "eks", "cluster", "autoscaling", "crossplane"),
    "frontend": ("frontend", "react", "next.js", "angular", "react native", "3.js"),
    "backend": ("backend", "fastapi", "django", "flask", "node.js", "express", "redis"),
    "healthcare": ("healthcare", "medical", "dicom", "mri", "ultrasound", "fhir", "hl7"),
    "imaging": ("image", "microscopy", "tiff", "viewer", "plotly", "3d visualization"),
    "research": ("research", "prolific", "study", "paper", "nsf", "institute"),
    "prompt_engineering": ("prompt engineering", "prompt", "reasoning model", "assistant", "conversation"),
    "security": ("security", "safe structure", "disallowed imports", "validation"),
    "finance": ("finance", "trading", "market", "financial", "copilot"),
}

_PROJECT_KEYWORDS: dict[str, tuple[str, ...]] = {
    "xellar": ("xellar", "organ-chip", "organ chip", "oc plex", "microscopy"),
    "ai-caring": ("ai-caring", "ai caring", "ambient reminder", "mild cognitive impairment", "aware home"),
    "capgemini": ("capgemini", "ge healthcare", "multimodality ai", "mmai"),
    "ltimindtree": ("ltimindtree", "electricity", "meter data management", "soa", "react native"),
    "gcp-infra": ("gcp", "cloud infra", "terraform", "packer", "serverless function"),
    "finddesk": ("findesk", "finrobot", "smart scheduler", "finance copilot"),
    "word2latex": ("word-to-latex", "word2latex", "conversion pipeline", "latex"),
}

_RESUME_VARIANT_TAGS: dict[str, tuple[str, ...]] = {
    "ai": ("ai", "llm", "evaluation", "research", "prompt_engineering"),
    "distributed_systems": ("infra", "backend", "kubernetes", "reliability"),
    "cloud": ("cloud", "infra", "kubernetes", "backend"),
}

_STOPWORDS = {
    "a",
    "about",
    "all",
    "an",
    "and",
    "are",
    "as",
    "at",
    "be",
    "but",
    "by",
    "do",
    "for",
    "from",
    "have",
    "how",
    "i",
    "if",
    "in",
    "into",
    "is",
    "it",
    "its",
    "of",
    "on",
    "or",
    "our",
    "should",
    "that",
    "the",
    "their",
    "them",
    "they",
    "this",
    "to",
    "us",
    "we",
    "what",
    "when",
    "where",
    "which",
    "why",
    "will",
    "with",
    "would",
    "you",
    "your",
}

_STYLE_PREFERRED_FILES = {"master-experience.md"}
_STYLE_HINTS = ("summary", "highlights", "resume", "interview-ready explanation", "interview summary")


@dataclass
class ProfileKnowledgeChunk:
    chunk_id: str
    source_file: str
    section_title: str
    text: str
    word_count: int
    topic_tags: list[str]
    evidence_type: str
    style_weight: float
    factual_weight: float
    resume_variant_affinity: list[str] = field(default_factory=list)


@dataclass
class ProfileStyleSnippet:
    snippet_id: str
    source_file: str
    section_title: str
    text: str
    word_count: int
    topic_tags: list[str]
    style_weight: float
    source_chunk_id: str | None = None


@dataclass
class ProfileKnowledgeManifest:
    generated_at: str
    source_dir: str
    chunk_count: int
    style_snippet_count: int
    source_files: list[dict[str, Any]] = field(default_factory=list)


@dataclass
class ProfileRetrievalItem:
    item_id: str
    source_file: str
    section_title: str
    text: str
    word_count: int
    topic_tags: list[str]
    score: float
    reasons: list[str] = field(default_factory=list)


@dataclass
class ProfileRetrievalResult:
    evidence_chunks: list[ProfileRetrievalItem] = field(default_factory=list)
    style_snippets: list[ProfileRetrievalItem] = field(default_factory=list)
    retrieval_summary: list[str] = field(default_factory=list)
    matched_tags: list[str] = field(default_factory=list)
    has_strong_evidence: bool = False


def _normalized_text(value: str | None) -> str:
    raw = str(value or "").strip().lower()
    normalized = "".join(ch if ch.isalnum() else " " for ch in raw)
    return " ".join(normalized.split())


def _tokenize(value: str | None) -> set[str]:
    normalized = _normalized_text(value)
    return {
        token
        for token in normalized.split()
        if len(token) > 2 and token not in _STOPWORDS
    }


def _stable_id(prefix: str, seed: str) -> str:
    import hashlib

    return f"{prefix}_{hashlib.sha256(seed.encode('utf-8')).hexdigest()[:16]}"


def _resolve_path(path: str | Path) -> Path:
    resolved = Path(path).expanduser()
    if not resolved.is_absolute():
        resolved = Path.cwd() / resolved
    return resolved


def _source_signature(profile_dir: Path) -> tuple[tuple[str, int, int], ...]:
    signature: list[tuple[str, int, int]] = []
    for path in sorted(profile_dir.glob("*.md")):
        stat = path.stat()
        signature.append((str(path.resolve()), stat.st_mtime_ns, stat.st_size))
    return tuple(signature)


def _source_role(path: Path) -> tuple[str, float, float]:
    name = path.name.lower()
    if name == "master-experience.md":
        return ("summary", 1.0, 0.85)
    if name == "experience.md":
        return ("experience", 0.25, 1.0)
    return ("project", 0.45, 0.9)


def _split_sections(text: str, source_file: str) -> list[tuple[str, str]]:
    sections: list[tuple[str, str]] = []
    headings: dict[int, str] = {}
    buffer: list[str] = []
    current_title = Path(source_file).stem.replace("-", " ").title()
    for line in text.splitlines():
        heading_match = re.match(r"^(#{1,6})\s+(.*\S)\s*$", line)
        if heading_match:
            content = "\n".join(buffer).strip()
            if content:
                sections.append((current_title, content))
            buffer = []
            level = len(heading_match.group(1))
            headings[level] = heading_match.group(2).strip()
            for stale in [key for key in headings if key > level]:
                headings.pop(stale, None)
            current_title = " > ".join(headings[index] for index in sorted(headings))
            continue
        buffer.append(line)
    content = "\n".join(buffer).strip()
    if content:
        sections.append((current_title, content))
    return sections


def _section_paragraphs(text: str) -> list[str]:
    paragraphs: list[str] = []
    for block in re.split(r"\n\s*\n", text):
        cleaned = " ".join(line.strip() for line in block.splitlines() if line.strip() and line.strip() != "---")
        if cleaned:
            paragraphs.append(cleaned)
    return paragraphs


def _chunk_paragraphs(paragraphs: list[str], *, minimum_words: int, maximum_words: int) -> list[str]:
    if not paragraphs:
        return []
    chunks: list[str] = []
    current: list[str] = []
    current_words = 0
    for paragraph in paragraphs:
        words = len(paragraph.split())
        if current and current_words >= minimum_words and current_words + words > maximum_words:
            chunks.append("\n\n".join(current))
            current = []
            current_words = 0
        current.append(paragraph)
        current_words += words
    if current:
        chunks.append("\n\n".join(current))
    return chunks


def _sentence_chunks(text: str, *, minimum_words: int, maximum_words: int) -> list[str]:
    sentences = [
        sentence.strip()
        for sentence in re.split(r"(?<=[.!?])\s+", " ".join(text.split()))
        if sentence.strip()
    ]
    if not sentences:
        return []
    chunks: list[str] = []
    current: list[str] = []
    current_words = 0
    for sentence in sentences:
        words = len(sentence.split())
        if current and current_words >= minimum_words and current_words + words > maximum_words:
            chunks.append(" ".join(current).strip())
            current = []
            current_words = 0
        current.append(sentence)
        current_words += words
    if current:
        chunks.append(" ".join(current).strip())
    return chunks


def _extract_topic_tags(*parts: str) -> list[str]:
    text = " ".join(part for part in parts if part).lower()
    tags = {
        tag
        for tag, keywords in _TOPIC_KEYWORDS.items()
        if any(keyword in text for keyword in keywords)
    }
    projects = {
        project
        for project, keywords in _PROJECT_KEYWORDS.items()
        if any(keyword in text for keyword in keywords)
    }
    return sorted(tags | projects)


def _resume_variant_affinity(tags: list[str]) -> list[str]:
    affinities = {
        variant
        for variant, required_tags in _RESUME_VARIANT_TAGS.items()
        if set(required_tags) & set(tags)
    }
    return sorted(affinities)


def _style_weight_for_section(source_file: str, section_title: str, base_weight: float) -> float:
    if source_file in _STYLE_PREFERRED_FILES:
        return max(base_weight, 0.95)
    normalized_title = _normalized_text(section_title)
    if any(hint in normalized_title for hint in _STYLE_HINTS):
        return max(base_weight, 0.7)
    return base_weight


def _write_jsonl(path: Path, records: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as handle:
        for record in records:
            handle.write(json.dumps(record, sort_keys=True, ensure_ascii=False))
            handle.write("\n")


def _write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False), encoding="utf-8")


def build_profile_knowledge_index(
    *,
    profile_dir: str | Path = DEFAULT_PROFILE_CORPUS_DIR,
    chunks_file: str | Path = DEFAULT_PROFILE_KNOWLEDGE_CHUNKS_FILE,
    style_file: str | Path = DEFAULT_PROFILE_STYLE_SNIPPETS_FILE,
    manifest_file: str | Path = DEFAULT_PROFILE_KNOWLEDGE_MANIFEST_FILE,
) -> tuple[list[ProfileKnowledgeChunk], list[ProfileStyleSnippet], ProfileKnowledgeManifest]:
    source_dir = _resolve_path(profile_dir)
    if not source_dir.exists():
        manifest = ProfileKnowledgeManifest(
            generated_at=utc_now(),
            source_dir=str(source_dir),
            chunk_count=0,
            style_snippet_count=0,
            source_files=[],
        )
        return [], [], manifest

    signature = _source_signature(source_dir)
    cached = _INDEX_CACHE.get(signature)
    if cached is not None:
        return cached

    chunks: list[ProfileKnowledgeChunk] = []
    snippets: list[ProfileStyleSnippet] = []
    source_files_payload: list[dict[str, Any]] = []

    for path in sorted(source_dir.glob("*.md")):
        raw_text = path.read_text(encoding="utf-8")
        evidence_type, base_style_weight, factual_weight = _source_role(path)
        source_files_payload.append(
            {
                "source_file": path.name,
                "evidence_type": evidence_type,
                "style_weight": base_style_weight,
                "factual_weight": factual_weight,
            }
        )
        for section_index, (section_title, section_text) in enumerate(_split_sections(raw_text, path.name), start=1):
            paragraphs = _section_paragraphs(section_text)
            for chunk_index, chunk_text in enumerate(
                _chunk_paragraphs(paragraphs, minimum_words=120, maximum_words=220),
                start=1,
            ):
                tags = _extract_topic_tags(path.name, section_title, chunk_text)
                chunk = ProfileKnowledgeChunk(
                    chunk_id=_stable_id(
                        "pkc",
                        f"{path.name}|{section_index}|{chunk_index}|{section_title}|{chunk_text}",
                    ),
                    source_file=path.name,
                    section_title=section_title,
                    text=chunk_text,
                    word_count=len(chunk_text.split()),
                    topic_tags=tags,
                    evidence_type=evidence_type,
                    style_weight=_style_weight_for_section(path.name, section_title, base_style_weight),
                    factual_weight=factual_weight,
                    resume_variant_affinity=_resume_variant_affinity(tags),
                )
                chunks.append(chunk)
                if chunk.style_weight < 0.55:
                    continue
                for snippet_index, snippet_text in enumerate(
                    _sentence_chunks(chunk_text, minimum_words=40, maximum_words=120),
                    start=1,
                ):
                    snippet_tags = _extract_topic_tags(path.name, section_title, snippet_text)
                    snippets.append(
                        ProfileStyleSnippet(
                            snippet_id=_stable_id(
                                "pks",
                                f"{chunk.chunk_id}|{snippet_index}|{snippet_text}",
                            ),
                            source_file=path.name,
                            section_title=section_title,
                            text=snippet_text,
                            word_count=len(snippet_text.split()),
                            topic_tags=snippet_tags,
                            style_weight=chunk.style_weight,
                            source_chunk_id=chunk.chunk_id,
                        )
                    )

    manifest = ProfileKnowledgeManifest(
        generated_at=utc_now(),
        source_dir=str(source_dir),
        chunk_count=len(chunks),
        style_snippet_count=len(snippets),
        source_files=source_files_payload,
    )

    _write_jsonl(_resolve_path(chunks_file), [asdict(chunk) for chunk in chunks])
    _write_jsonl(_resolve_path(style_file), [asdict(snippet) for snippet in snippets])
    _write_json(_resolve_path(manifest_file), asdict(manifest))

    _INDEX_CACHE.clear()
    _INDEX_CACHE[signature] = (chunks, snippets, manifest)
    return chunks, snippets, manifest


def _query_tags(*parts: str) -> list[str]:
    return _extract_topic_tags(*parts)


def _score_chunk(
    chunk: ProfileKnowledgeChunk,
    *,
    query_tokens: set[str],
    query_tags: set[str],
    query_projects: set[str],
    selected_resume_variant: str | None,
) -> tuple[float, list[str]]:
    heading_tokens = _tokenize(chunk.section_title)
    body_tokens = _tokenize(chunk.text)
    heading_overlap = heading_tokens & query_tokens
    body_overlap = body_tokens & query_tokens
    matched_tags = set(chunk.topic_tags) & query_tags
    matched_projects = set(chunk.topic_tags) & query_projects
    reasons: list[str] = []
    score = 0.0
    if heading_overlap:
        score += min(4.0, float(len(heading_overlap)) * 1.6)
        reasons.append(f"heading:{', '.join(sorted(heading_overlap)[:4])}")
    if body_overlap:
        score += min(5.0, float(len(body_overlap)) * 0.75)
        reasons.append(f"keywords:{', '.join(sorted(body_overlap)[:4])}")
    if matched_tags:
        score += float(len(matched_tags)) * 2.3
        reasons.append(f"tags:{', '.join(sorted(matched_tags))}")
    if matched_projects:
        score += float(len(matched_projects)) * 2.0
        reasons.append(f"projects:{', '.join(sorted(matched_projects))}")
    if selected_resume_variant and selected_resume_variant in chunk.resume_variant_affinity:
        score += 1.8
        reasons.append(f"resume_variant:{selected_resume_variant}")
    score *= max(0.1, chunk.factual_weight)
    return score, reasons


def _score_style_snippet(
    snippet: ProfileStyleSnippet,
    *,
    query_tokens: set[str],
    query_tags: set[str],
) -> tuple[float, list[str]]:
    heading_tokens = _tokenize(snippet.section_title)
    body_tokens = _tokenize(snippet.text)
    overlap = (heading_tokens | body_tokens) & query_tokens
    matched_tags = set(snippet.topic_tags) & query_tags
    reasons: list[str] = []
    score = float(len(overlap)) * 0.7
    if overlap:
        reasons.append(f"keywords:{', '.join(sorted(overlap)[:4])}")
    if matched_tags:
        score += float(len(matched_tags)) * 1.3
        reasons.append(f"tags:{', '.join(sorted(matched_tags))}")
    if snippet.source_file in _STYLE_PREFERRED_FILES:
        score += 1.5
        reasons.append("preferred_style_source")
    score *= max(0.1, snippet.style_weight)
    return score, reasons


def retrieve_profile_knowledge(
    *,
    question_label: str,
    question_description: str | None = None,
    option_labels: list[str] | None = None,
    job_title: str | None = None,
    job_excerpt: str | None = None,
    selected_resume_variant: str | None = None,
    profile_dir: str | Path = DEFAULT_PROFILE_CORPUS_DIR,
) -> ProfileRetrievalResult:
    chunks, snippets, _manifest = build_profile_knowledge_index(profile_dir=profile_dir)
    if not chunks:
        return ProfileRetrievalResult()

    excerpt = str(job_excerpt or "").strip()[:4000]
    option_blob = " ".join(option_labels or [])
    query_text = " ".join(
        part
        for part in (question_label, question_description or "", option_blob, job_title or "", excerpt)
        if str(part).strip()
    )
    query_tokens = _tokenize(query_text)
    query_tags = set(_query_tags(question_label, question_description or "", option_blob, job_title or "", excerpt))
    query_projects = {
        project
        for project, keywords in _PROJECT_KEYWORDS.items()
        if any(keyword in query_text.lower() for keyword in keywords)
    }

    evidence_hits: list[ProfileRetrievalItem] = []
    for chunk in chunks:
        score, reasons = _score_chunk(
            chunk,
            query_tokens=query_tokens,
            query_tags=query_tags,
            query_projects=query_projects,
            selected_resume_variant=selected_resume_variant,
        )
        if score < 2.5:
            continue
        evidence_hits.append(
            ProfileRetrievalItem(
                item_id=chunk.chunk_id,
                source_file=chunk.source_file,
                section_title=chunk.section_title,
                text=chunk.text,
                word_count=chunk.word_count,
                topic_tags=list(chunk.topic_tags),
                score=round(score, 3),
                reasons=reasons,
            )
        )
    evidence_hits.sort(key=lambda item: (-item.score, item.source_file, item.section_title))
    evidence_hits = evidence_hits[:5]

    style_hits: list[ProfileRetrievalItem] = []
    for snippet in snippets:
        score, reasons = _score_style_snippet(
            snippet,
            query_tokens=query_tokens,
            query_tags=query_tags,
        )
        if score < 1.2:
            continue
        style_hits.append(
            ProfileRetrievalItem(
                item_id=snippet.snippet_id,
                source_file=snippet.source_file,
                section_title=snippet.section_title,
                text=snippet.text,
                word_count=snippet.word_count,
                topic_tags=list(snippet.topic_tags),
                score=round(score, 3),
                reasons=reasons,
            )
        )
    style_hits.sort(key=lambda item: (-item.score, item.source_file, item.section_title))
    style_hits = style_hits[:2]

    strong_evidence = False
    if evidence_hits:
        strong_evidence = evidence_hits[0].score >= 5.0 or sum(item.score for item in evidence_hits[:2]) >= 8.0

    retrieval_summary: list[str] = []
    if query_tags:
        retrieval_summary.append(f"matched tags: {', '.join(sorted(query_tags))}")
    if query_projects:
        retrieval_summary.append(f"matched projects: {', '.join(sorted(query_projects))}")
    if selected_resume_variant:
        retrieval_summary.append(f"resume variant: {selected_resume_variant}")
    if evidence_hits:
        top_sources = ", ".join(
            f"{item.source_file}:{item.section_title}" for item in evidence_hits[:3]
        )
        retrieval_summary.append(f"top evidence: {top_sources}")
    if style_hits:
        retrieval_summary.append(
            "style sources: "
            + ", ".join(f"{item.source_file}:{item.section_title}" for item in style_hits[:2])
        )

    return ProfileRetrievalResult(
        evidence_chunks=evidence_hits,
        style_snippets=style_hits,
        retrieval_summary=retrieval_summary,
        matched_tags=sorted(query_tags),
        has_strong_evidence=strong_evidence,
    )
