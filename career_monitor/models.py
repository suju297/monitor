from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class Job:
    company: str
    source: str
    title: str
    url: str
    external_id: str | None = None
    location: str | None = None
    team: str | None = None
    posted_at: str | None = None


@dataclass
class CrawlOutcome:
    company: str
    attempted_sources: list[str]
    status: str
    selected_source: str | None = None
    jobs: list[Job] = field(default_factory=list)
    message: str = ""

