from __future__ import annotations

from typing import Any

import requests
from bs4 import BeautifulSoup

from .exceptions import CrawlBlockedError, CrawlFetchError
from .utils import classify_http_block


def requests_session() -> requests.Session:
    session = requests.Session()
    session.headers.update(
        {
            "User-Agent": "CareerPageMonitor/2.1 (+crawler-swarm)",
            "Accept": "application/json,text/html;q=0.9,*/*;q=0.8",
        }
    )
    return session


def first_title_from_html(html: str) -> str:
    soup = BeautifulSoup(html[:250000], "html.parser")
    if soup.title and soup.title.string:
        return " ".join(soup.title.string.split()).strip()
    return ""


def perform_request(
    session: requests.Session,
    method: str,
    url: str,
    timeout_seconds: int,
    **kwargs: Any,
) -> requests.Response:
    response = session.request(method=method, url=url, timeout=timeout_seconds, **kwargs)
    content_type = (response.headers.get("content-type") or "").lower()
    title = ""
    if "text/html" in content_type:
        title = first_title_from_html(response.text)
    if classify_http_block(response.status_code, title):
        detail = f"Blocked with HTTP {response.status_code}"
        if title:
            detail = f"{detail} ({title})"
        raise CrawlBlockedError(detail)
    if response.status_code >= 400:
        raise CrawlFetchError(f"HTTP {response.status_code} for {url}")
    return response

