#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
from html import unescape as html_unescape
from urllib.parse import urljoin, urlparse

BLOCKED_TITLE_MARKERS = (
    "attention required",
    "just a moment",
    "are you a robot",
    "security checkpoint",
    "captcha",
    "access denied",
)

DEFAULT_KEYWORDS = (
    "job",
    "jobs",
    "career",
    "careers",
    "opening",
    "openings",
    "position",
    "vacancy",
    "vacancies",
    "engineering",
    "software",
)

NOISE_TITLE_EXACT = {
    "careers",
    "career",
    "jobs",
    "search jobs",
    "search results",
    "benefits",
    "culture",
    "locations",
    "overview",
    "privacy",
    "legal",
    "cookie preferences",
    "home",
    "open positions",
    "all jobs",
    "learn more",
    "view jobs",
    "internships",
    "internships and programs",
    "leadership principles",
    "internal careers site",
    "development and engineering",
    "engineering and development",
    "join our talent community",
    "apply now",
}

ASSET_URL_MARKERS = (
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".svg",
    ".ico",
    ".css",
    ".js",
    ".mjs",
    ".woff",
    ".woff2",
    ".ttf",
    ".otf",
)

JOB_URL_HINTS = (
    "/job/",
    "/jobs/",
    "/details/",
    "/careers/job",
    "/position/",
    "/vacancy/",
    "/openings/",
    "jobid=",
    "job_id=",
    "gh_jid=",
    "reqid=",
    "requisition",
)

NON_JOB_URL_MARKERS = (
    "/jointalentcommunity",
    "/talentcommunity",
    "/talent-community",
    "/join-our-talent-community",
    "/search-results",
    "/search-jobs",
    "/jobsearch",
    "/locations/",
    "/departments/",
    "/teams/",
    "/benefits/",
    "/culture",
    "/about/",
    "/blog/",
    "/events/",
    "/news/",
    "/why-work-here",
    "/internships-and-programs",
    "/early-careers",
    "/c/",
)

ROLE_TITLE_HINTS = (
    "engineer",
    "developer",
    "scientist",
    "analyst",
    "architect",
    "manager",
    "specialist",
    "intern",
    "internship",
    "director",
    "principal",
    "staff",
    "lead",
    "consultant",
    "sre",
    "devops",
    "qa",
    "product manager",
    "program manager",
    "researcher",
    "administrator",
    "technician",
    "recruiter",
    "designer",
    "owner",
    "swe",
)

NOISE_TEXT_MARKERS = (
    "<img",
    "const t=",
    ".css-",
    "{display:",
    "data-gatsby-image",
    "queryselectorall(",
    "window.",
    "document.",
    "srcset",
)

DESCRIPTION_QUALITY_MARKERS = (
    "responsibil",
    "requirement",
    "qualification",
    "about the role",
    "what you will do",
    "what you'll do",
    "about you",
    "experience",
    "job description",
)

US_STATE_NAMES = (
    "alabama",
    "alaska",
    "arizona",
    "arkansas",
    "california",
    "colorado",
    "connecticut",
    "delaware",
    "florida",
    "georgia",
    "hawaii",
    "idaho",
    "illinois",
    "indiana",
    "iowa",
    "kansas",
    "kentucky",
    "louisiana",
    "maine",
    "maryland",
    "massachusetts",
    "michigan",
    "minnesota",
    "mississippi",
    "missouri",
    "montana",
    "nebraska",
    "nevada",
    "new hampshire",
    "new jersey",
    "new mexico",
    "new york",
    "north carolina",
    "north dakota",
    "ohio",
    "oklahoma",
    "oregon",
    "pennsylvania",
    "rhode island",
    "south carolina",
    "south dakota",
    "tennessee",
    "texas",
    "utah",
    "vermont",
    "virginia",
    "washington",
    "west virginia",
    "wisconsin",
    "wyoming",
    "district of columbia",
    "washington dc",
    "washington d.c.",
)

US_STATE_CODES = (
    "AL",
    "AK",
    "AZ",
    "AR",
    "CA",
    "CO",
    "CT",
    "DE",
    "FL",
    "GA",
    "HI",
    "ID",
    "IL",
    "IN",
    "IA",
    "KS",
    "KY",
    "LA",
    "ME",
    "MD",
    "MA",
    "MI",
    "MN",
    "MS",
    "MO",
    "MT",
    "NE",
    "NV",
    "NH",
    "NJ",
    "NM",
    "NY",
    "NC",
    "ND",
    "OH",
    "OK",
    "OR",
    "PA",
    "RI",
    "SC",
    "SD",
    "TN",
    "TX",
    "UT",
    "VT",
    "VA",
    "WA",
    "WV",
    "WI",
    "WY",
    "DC",
)

LOCATION_TAIL_RE = re.compile(
    r"(?P<location>(?:[A-Z][A-Za-z .'\-]+,\s*(?:[A-Z]{2}|[A-Za-z][A-Za-z .'\-]+)(?:\s*;\s*[A-Z][A-Za-z .'\-]+,\s*(?:[A-Z]{2}|[A-Za-z][A-Za-z .'\-]+))*|Remote(?:\s*[-,]\s*(?:US|USA|United States))?))$"
)
US_COUNTRY_RE = re.compile(r"\b(?:united states(?: of america)?|u\.s\.a?\.|usa)\b", flags=re.IGNORECASE)
US_REMOTE_RE = re.compile(
    r"\b(?:remote|hybrid)\b[^\n]{0,28}\bUS\b|\bUS\b[^\n]{0,28}\b(?:remote|hybrid)\b"
)
US_STATE_NAME_RE = re.compile(
    r",\s*(?:"
    + "|".join(sorted((re.escape(item) for item in US_STATE_NAMES), key=len, reverse=True))
    + r")\b",
    flags=re.IGNORECASE,
)
US_STATE_CODE_RE = re.compile(r",\s*(?:" + "|".join(US_STATE_CODES) + r")\b")

DETAIL_EXTRACTION_SCRIPT = """
() => {
  const normalize = (value, limit = 4200) => {
    const compact = (value || '').replace(/\\s+/g, ' ').trim();
    if (!compact) return '';
    return compact.length > limit ? `${compact.slice(0, limit).trim()}...` : compact;
  };
  const cloneWithoutNoise = (node) => {
    if (!node) return null;
    const copy = node.cloneNode(true);
    const selectors = [
      'script',
      'style',
      'noscript',
      'svg',
      'canvas',
      'iframe',
      'header',
      'footer',
      'nav',
      'aside',
      '[role="navigation"]',
      '[aria-hidden="true"]',
      '[class*="cookie"]',
      '[id*="cookie"]',
      '[class*="breadcrumb"]',
      '[id*="breadcrumb"]',
      '[class*="share"]',
      '[id*="share"]'
    ];
    copy.querySelectorAll(selectors.join(',')).forEach((el) => el.remove());
    return copy;
  };
  const nodeText = (node, limit = 4200) => {
    if (!node) return '';
    const copy = cloneWithoutNoise(node);
    if (!copy) return '';
    return normalize(copy.innerText || copy.textContent || '', limit);
  };
  const extractDateCandidate = (value) => {
    const text = normalize(value, 240);
    if (!text) return '';
    const patterns = [
      /\\b\\d{4}-\\d{1,2}-\\d{1,2}(?:[T ]\\d{1,2}:\\d{2}(?::\\d{2})?)?(?:Z|[+-]\\d{2}:?\\d{2})?\\b/i,
      /\\b\\d{1,2}\\/\\d{1,2}\\/\\d{2,4}(?:\\s+\\d{1,2}:\\d{2}(?::\\d{2})?(?:\\s?[APMapm]{2})?)?\\b/i,
      /\\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\\.?\\s+\\d{1,2},?\\s+\\d{4}\\b/i,
      /\\b\\d{1,2}\\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\\.?\\s+\\d{4}\\b/i,
      /\\b(?:today|yesterday|just now|\\d+\\s*(?:minute|hour|day|week|month|year)s?\\s+ago)\\b/i
    ];
    for (const re of patterns) {
      const match = text.match(re);
      if (match && match[0]) {
        return normalize(match[0], 140);
      }
    }
    return '';
  };
  const collectDateCandidates = () => {
    const out = [];
    const push = (value) => {
      const candidate = normalize(value, 260);
      if (!candidate) return;
      out.push(candidate);
    };
    const metaSelectors = [
      'meta[itemprop="datePosted"]',
      'meta[itemprop="datePublished"]',
      'meta[property="article:published_time"]',
      'meta[property="og:updated_time"]',
      'meta[name="publish-date"]',
      'meta[name="date"]',
      'meta[name="lastmod"]'
    ];
    for (const selector of metaSelectors) {
      document.querySelectorAll(selector).forEach((el) => push(el.getAttribute('content') || ''));
    }
    document.querySelectorAll('time').forEach((el) => {
      push(el.getAttribute('datetime') || '');
      push(el.textContent || '');
    });
    document.querySelectorAll('[datetime], [data-posted], [data-posted-at], [data-date], [data-datetime], [data-created-at]').forEach((el) => {
      for (const key of ['datetime', 'data-posted', 'data-posted-at', 'data-date', 'data-datetime', 'data-created-at']) {
        push(el.getAttribute(key) || '');
      }
    });
    const lines = (document.body?.innerText || '').split(/\\n+/).map((line) => line.trim()).filter(Boolean);
    for (const line of lines) {
      const lower = line.toLowerCase();
      if (lower.includes('posted') || lower.includes('published') || lower.includes('date posted') || lower.includes('updated')) {
        push(line);
      }
    }
    document.querySelectorAll('script[type="application/ld+json"]').forEach((el) => {
      const raw = el.textContent || '';
      if (!raw) return;
      try {
        const parsed = JSON.parse(raw);
        const stack = [parsed];
        while (stack.length > 0) {
          const current = stack.pop();
          if (!current || typeof current !== 'object') continue;
          if (Array.isArray(current)) {
            current.forEach((entry) => stack.push(entry));
            continue;
          }
          ['datePosted', 'datePublished', 'dateCreated', 'uploadDate'].forEach((key) => {
            if (current[key]) push(String(current[key]));
          });
          Object.values(current).forEach((value) => {
            if (value && typeof value === 'object') stack.push(value);
          });
        }
      } catch (_) {
      }
    });
    return out;
  };
  const descriptionScore = (text, selector) => {
    if (!text) return -100;
    const words = text.split(/\\s+/).filter(Boolean).length;
    if (words < 10) return -60;
    let score = 0;
    if (words >= 20) score += 4;
    if (words >= 45) score += 4;
    if (words >= 90) score += 3;
    if (words > 450) score -= Math.floor((words - 450) / 25);
    const lower = text.toLowerCase();
    for (const marker of ['responsibil', 'requirement', 'qualification', 'about the role', 'what you', 'experience', 'job description']) {
      if (lower.includes(marker)) {
        score += 8;
        break;
      }
    }
    if ((lower.includes('pay range transparency') || lower.includes('equal employment opportunity')) && words < 260) {
      score -= 12;
    }
    for (const marker of ['cookie', 'privacy', 'legal', 'learn more', 'sign in', 'create account', 'language']) {
      if (lower.includes(marker)) {
        score -= 6;
        break;
      }
    }
    if (selector.includes('job-description') || selector.includes('description') || selector.includes('detail')) {
      score += 2;
    }
    return score;
  };

  const descriptionSelectors = [
    '[data-testid*="job-description"]',
    '[class*="job-description"]',
    '[id*="job-description"]',
    '[class*="description"]',
    '[id*="description"]',
    '[class*="responsibil"]',
    '[class*="requirement"]',
    'article',
    'main',
    'section',
    'body'
  ];

  let bestDescription = '';
  let bestScore = -999;
  const seen = new Set();
  for (const selector of descriptionSelectors) {
    const nodes = document.querySelectorAll(selector);
    let scanned = 0;
    for (const node of nodes) {
      if (scanned >= 6) break;
      const text = nodeText(node, 4200);
      if (!text || seen.has(text)) continue;
      seen.add(text);
      const score = descriptionScore(text, selector);
      if (score > bestScore) {
        bestScore = score;
        bestDescription = text;
      }
      scanned += 1;
    }
  }
  if (!bestDescription) {
    const meta = document.querySelector('meta[name="description"], meta[property="og:description"], meta[property="twitter:description"]');
    bestDescription = normalize(meta?.getAttribute('content') || '', 1400);
  }

  let title = normalize(document.querySelector('h1')?.innerText || document.querySelector('h1')?.textContent || '', 260);
  if (!title) {
    title = normalize(document.title || '', 260).replace(/\\s*[|\\-].*$/, '').trim();
  }

  let postedAt = '';
  for (const rawCandidate of collectDateCandidates()) {
    const normalized = extractDateCandidate(rawCandidate);
    if (normalized) {
      postedAt = normalized;
      break;
    }
  }

  return {
    title,
    description: bestDescription,
    posted_at: postedAt
  };
}
"""


def emit(payload: dict) -> int:
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def title_from_url(url: str) -> str:
    parsed = urlparse(url)
    path_parts = [part for part in parsed.path.split("/") if part]
    if not path_parts:
        return "Possible opening"
    raw = path_parts[-1]
    cleaned = re.sub(r"[-_]+", " ", raw).strip()
    cleaned = re.sub(r"\s+", " ", cleaned)
    return cleaned[:150] or "Possible opening"


def is_blocked_title(title: str) -> bool:
    value = title.strip().lower()
    if not value:
        return False
    return any(marker in value for marker in BLOCKED_TITLE_MARKERS)


def keyword_list() -> tuple[str, ...]:
    raw = os.getenv("PW_KEYWORDS", "").strip()
    if not raw:
        return DEFAULT_KEYWORDS
    parsed = [item.strip().lower() for item in raw.split(",") if item.strip()]
    return tuple(parsed) if parsed else DEFAULT_KEYWORDS


def parse_bool_env(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def normalize_text(value: str, limit: int = 900) -> str:
    text = re.sub(r"\s+", " ", (value or "")).strip()
    if not text:
        return ""
    return f"{text[:limit].rstrip()}..." if len(text) > limit else text


def extract_location_hint(*values: str | None) -> str | None:
    for value in values:
        text = normalize_text(str(value or ""), 260)
        if not text:
            continue
        match = LOCATION_TAIL_RE.search(text)
        if match:
            location = normalize_location(match.group("location"))
            if location:
                return location
    return None


def has_us_location_marker(value: str | None) -> bool:
    text = normalize_text(str(value or ""), 320)
    if not text:
        return False
    if US_COUNTRY_RE.search(text):
        return True
    if US_REMOTE_RE.search(text):
        return True
    if US_STATE_NAME_RE.search(text):
        return True
    if US_STATE_CODE_RE.search(text):
        return True
    return False


def is_us_job(title: str, location: str | None) -> bool:
    return has_us_location_marker(location) or has_us_location_marker(title)


def is_noise_text(value: str) -> bool:
    lowered = normalize_text(value, 2400).lower()
    if not lowered:
        return True
    if any(marker in lowered for marker in NOISE_TEXT_MARKERS):
        return True
    if re.search(r"\.[a-z0-9_-]+\{[^}]*\}", lowered):
        return True
    if len(lowered) > 5000:
        return True
    return False


def link_matches(text: str, url: str, keywords: tuple[str, ...]) -> bool:
    combined = f"{text} {url}".lower()
    return any(keyword in combined for keyword in keywords)


def looks_like_asset_url(url: str) -> bool:
    value = url.strip().lower()
    return any(marker in value for marker in ASSET_URL_MARKERS)


def looks_like_non_job_url(url: str) -> bool:
    value = url.strip().lower()
    if not value:
        return True
    if any(marker in value for marker in NON_JOB_URL_MARKERS):
        return True
    if "/apply?" in value or "/apply/" in value:
        # Treat application funnels as noise unless there is a strong job path hint.
        return not any(hint in value for hint in JOB_URL_HINTS)
    return False


def is_noise_title(title: str) -> bool:
    value = re.sub(r"\s+", " ", title.strip().lower())
    if not value:
        return True
    if len(value) > 240:
        return True
    if value in NOISE_TITLE_EXACT:
        return True
    if (value.startswith("view ") or value.startswith("browse ") or value.startswith("see ")) and " jobs" in value:
        return True
    if value.startswith("apply now about "):
        return True
    if " available jobs" in value:
        return True
    for marker in ("<img", "const t=", ".css-", "{display:", "data-gatsby-image", "queryselectorall("):
        if marker in value:
            return True
    for marker in (
        "provides investment management solutions",
        "envisions, builds, and deploys",
        "identifies, monitors, evaluates",
        "enables business to flow",
        "discover life at",
        "explore our firm",
        "advice and tips",
        "programs for professionals",
    ):
        if marker in value:
            return True
    if len(value.split()) <= 2 and value in {"engineering", "sales", "marketing", "operations", "finance"}:
        return True
    return False


def normalize_title(value: str) -> str:
    title = normalize_text(value, 260)
    if not title:
        return ""
    title = re.sub(r"\s*(?:\||-)\s*(careers?|jobs?)\s*$", "", title, flags=re.IGNORECASE).strip()
    return title


def normalize_description(value: str | None) -> str | None:
    if not value:
        return None
    cleaned = normalize_text(value, 2000)
    if not cleaned:
        return None
    lowered = cleaned.lower()
    if lowered.startswith("skip to main content"):
        marker = "back to search results"
        marker_idx = lowered.find(marker)
        if marker_idx >= 0:
            cleaned = cleaned[marker_idx + len(marker) :].strip()
            lowered = cleaned.lower()
        cleaned = re.sub(r"^skip to main content\s*", "", cleaned, flags=re.IGNORECASE).strip()
        lowered = cleaned.lower()
    if lowered.startswith("overviewculturebenefitsdiversityengineeringresearchstudents"):
        cleaned = re.sub(
            r"^overviewculturebenefitsdiversityengineeringresearchstudents\s*&?\s*new\s*grads",
            "",
            cleaned,
            flags=re.IGNORECASE,
        ).strip()
    lowered = cleaned.lower()
    apply_now_idx = lowered.find("apply now")
    if 0 <= apply_now_idx <= 180:
        cleaned = cleaned[apply_now_idx + len("apply now") :].strip()
    if is_noise_text(cleaned):
        return None
    return cleaned


def normalize_location(value: str | None, limit: int = 220) -> str | None:
    if not value:
        return None
    cleaned = normalize_text(value, limit)
    if not cleaned:
        return None
    cleaned = re.sub(r"^(?:job\s+)?locations?\s*[:\-]\s*", "", cleaned, flags=re.IGNORECASE).strip(" ,;|-")
    if not cleaned:
        return None
    lowered = cleaned.lower()
    if lowered in {
        "location",
        "locations",
        "all locations",
        "select location",
        "search by location",
    }:
        return None
    return cleaned


def normalize_country(value: str | None) -> str | None:
    if not value:
        return None
    cleaned = normalize_text(value, 80).strip(" ,;|-")
    if not cleaned:
        return None
    return cleaned


def compose_output_location(location: str | None, country: str | None) -> str | None:
    normalized_location = normalize_location(location)
    normalized_country = normalize_country(country)
    if normalized_location and normalized_country:
        if normalized_country.lower() not in normalized_location.lower():
            return normalize_location(f"{normalized_location}, {normalized_country}")
        return normalized_location
    if normalized_location:
        return normalized_location
    return normalized_country


def walk_objects(value):
    stack = [value]
    while stack:
        current = stack.pop()
        if isinstance(current, dict):
            yield current
            for child in current.values():
                if isinstance(child, (dict, list)):
                    stack.append(child)
        elif isinstance(current, list):
            for child in current:
                if isinstance(child, (dict, list)):
                    stack.append(child)


def compose_location(locality: str | None, region: str | None, country: str | None) -> str | None:
    parts: list[str] = []
    for part in [locality, region, country]:
        normalized = normalize_location(part, 120)
        if not normalized:
            continue
        if normalized not in parts:
            parts.append(normalized)
    if not parts:
        return None
    return normalize_location(", ".join(parts), 220)


def extract_location_from_html(html_text: str) -> tuple[str | None, str | None]:
    if not html_text:
        return None, None

    locations: list[str] = []
    countries: list[str] = []

    def push(location: str | None = None, country: str | None = None) -> None:
        normalized_country = normalize_country(country)
        normalized_location = normalize_location(location)
        if normalized_country and normalized_country not in countries:
            countries.append(normalized_country)
        if normalized_location and normalized_location not in locations:
            locations.append(normalized_location)

    def parse_job_location(value) -> None:
        if isinstance(value, list):
            for entry in value:
                parse_job_location(entry)
            return
        if isinstance(value, dict):
            name = normalize_location(str(value.get("name", "")).strip() or None)
            if name:
                push(location=name)
            address = value.get("address")
            if isinstance(address, dict):
                locality = normalize_location(str(address.get("addressLocality", "")).strip() or None)
                region = normalize_location(str(address.get("addressRegion", "")).strip() or None)
                country = normalize_country(str(address.get("addressCountry", "")).strip() or None)
                push(location=compose_location(locality, region, country), country=country)
            return
        if isinstance(value, str):
            push(location=value)

    for raw_script in re.findall(
        r"<script[^>]*type=['\"]application/ld\+json['\"][^>]*>(.*?)</script>",
        html_text,
        flags=re.IGNORECASE | re.DOTALL,
    ):
        payload_text = html_unescape(raw_script or "").strip()
        if not payload_text:
            continue
        try:
            payload = json.loads(payload_text)
        except Exception:
            continue
        for obj in walk_objects(payload):
            if "jobLocation" in obj:
                parse_job_location(obj.get("jobLocation"))
            if "addressCountry" in obj:
                locality = normalize_location(str(obj.get("addressLocality", "")).strip() or None)
                region = normalize_location(str(obj.get("addressRegion", "")).strip() or None)
                country = normalize_country(str(obj.get("addressCountry", "")).strip() or None)
                push(location=compose_location(locality, region, country), country=country)
            if "standardised_multi_location" in obj and isinstance(obj.get("standardised_multi_location"), list):
                for row in obj.get("standardised_multi_location") or []:
                    if not isinstance(row, dict):
                        continue
                    locality = normalize_location(str(row.get("standardisedCity", "")).strip() or None)
                    region = normalize_location(
                        str(row.get("standardisedStateCode", "") or row.get("standardisedState", "")).strip() or None
                    )
                    country = normalize_country(str(row.get("standardisedCountry", "")).strip() or None)
                    push(
                        location=compose_location(
                            locality=locality,
                            region=region,
                            country=country,
                        ),
                        country=country,
                    )

    unescaped_quotes_text = html_text.replace('\\"', '"').replace("\\/", "/")
    fallback_texts = [
        html_text,
        html_unescape(html_text),
        unescaped_quotes_text,
        html_unescape(unescaped_quotes_text),
    ]
    if not countries:
        for blob in fallback_texts:
            match = re.search(r'"addressCountry"\s*:\s*"([^"]+)"', blob, flags=re.IGNORECASE)
            if not match:
                match = re.search(r'"countryName"\s*:\s*"([^"]+)"', blob, flags=re.IGNORECASE)
            if match:
                push(country=match.group(1))
                break
    if not locations:
        for blob in fallback_texts:
            match = re.search(r'"standardisedMapQueryLocation"\s*:\s*"([^"]+)"', blob, flags=re.IGNORECASE)
            if match:
                push(location=html_unescape(match.group(1)))
                break
            apple_loc = re.search(
                r'"city"\s*:\s*"([^"]+)"[^{}]{0,240}"stateProvince"\s*:\s*"([^"]*)"[^{}]{0,240}"countryName"\s*:\s*"([^"]+)"',
                blob,
                flags=re.IGNORECASE | re.DOTALL,
            )
            if apple_loc:
                city = normalize_location(apple_loc.group(1))
                region = normalize_location(apple_loc.group(2))
                country = normalize_country(apple_loc.group(3))
                push(location=compose_location(city, region, country), country=country)
                break

    location = locations[0] if locations else None
    country = countries[0] if countries else None
    if not location and country:
        location = country
    return location, country


def location_matches_filters(
    location: str | None,
    country: str | None,
    include_re: re.Pattern[str] | None,
    exclude_re: re.Pattern[str] | None,
) -> bool:
    haystack = " ".join(part for part in [location, country] if part).strip()
    if include_re and (not haystack or not include_re.search(haystack)):
        return False
    if exclude_re and haystack and exclude_re.search(haystack):
        return False
    return True


def title_needs_detail(title: str) -> bool:
    normalized = normalize_title(title)
    if not normalized:
        return True
    if re.search(r"[a-z][A-Z]", normalized):
        return True
    if len(normalized.split()) < 2:
        return True
    return False


def description_needs_detail(description: str | None) -> bool:
    if not description:
        return True
    cleaned = normalize_description(description)
    if not cleaned:
        return True
    words = len(cleaned.split())
    if words < 24:
        return True
    lowered = cleaned.lower()
    if any(marker in lowered for marker in ("learn more", "view jobs", "search jobs", "open positions")):
        return True
    if not any(marker in lowered for marker in DESCRIPTION_QUALITY_MARKERS) and words < 40:
        return True
    return False


def has_description_job_marker(description: str | None) -> bool:
    if not description:
        return False
    lowered = normalize_text(description, 2000).lower()
    return any(marker in lowered for marker in DESCRIPTION_QUALITY_MARKERS)


def has_job_opening_signal(title: str, url: str, description: str | None, posted_at: str | None) -> bool:
    url_value = url.strip().lower()
    title_value = title.strip().lower()
    if any(hint in url_value for hint in JOB_URL_HINTS):
        return True
    if any(hint in title_value for hint in ROLE_TITLE_HINTS):
        return True
    if posted_at:
        return True
    if description and len(description.split()) >= 24 and has_description_job_marker(description):
        return True
    return False


def is_likely_job_opening(title: str, url: str, description: str | None, posted_at: str | None) -> bool:
    if is_noise_title(title):
        return False
    if looks_like_asset_url(url):
        return False
    if looks_like_non_job_url(url):
        return False
    if not has_job_opening_signal(title=title, url=url, description=description, posted_at=posted_at):
        return False
    return True


def extract_detail_for_job(detail_page, job_url: str, timeout_ms: int, wait_after_load_ms: int) -> dict:
    try:
        response = detail_page.goto(job_url, wait_until="domcontentloaded", timeout=timeout_ms)
        if response and response.status in {401, 403, 404, 429}:
            return {}
        if is_blocked_title(detail_page.title() or ""):
            return {}
        try:
            detail_page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 12000))
        except Exception:
            pass
        if wait_after_load_ms:
            detail_page.wait_for_timeout(wait_after_load_ms)
        payload = detail_page.evaluate(DETAIL_EXTRACTION_SCRIPT) or {}
        if not isinstance(payload, dict):
            return {}
        page_html = detail_page.content() or ""
        location, country = extract_location_from_html(page_html)
        return {
            "title": normalize_title(str(payload.get("title", "")).strip()),
            "description": normalize_description(str(payload.get("description", "")).strip() or None),
            "posted_at": normalize_text(str(payload.get("posted_at", "")).strip(), 140) or None,
            "location": location,
            "country": country,
        }
    except Exception:
        return {}


def wait_for_listing_settle(page, timeout_ms: int, wait_after_load_ms: int, timeout_error_cls) -> None:
    try:
        page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 15000))
    except timeout_error_cls:
        pass
    except Exception:
        pass
    if wait_after_load_ms:
        page.wait_for_timeout(wait_after_load_ms)


def extract_listing_links(page, selector: str) -> list[dict]:
    try:
        links = page.eval_on_selector_all(
            selector,
            """
            (nodes) => {
              const normalize = (value, limit = 1200) => {
                const compact = (value || '').replace(/\\s+/g, ' ').trim();
                if (!compact) return '';
                return compact.length > limit ? `${compact.slice(0, limit).trim()}...` : compact;
              };
              const cleanNodeText = (root, limit = 1500) => {
                if (!root) return '';
                const copy = root.cloneNode(true);
                copy.querySelectorAll('script,style,noscript,svg,canvas,iframe').forEach((el) => el.remove());
                return normalize(copy.innerText || copy.textContent || '', limit);
              };
              const pickTime = (root) => {
                if (!root) return '';
                const direct = root.querySelector('time');
                if (direct) {
                  const dt = (direct.getAttribute('datetime') || '').trim();
                  if (dt) return dt;
                  const text = normalize(direct.textContent || '', 120);
                  if (text) return text;
                }
                const attrNode = root.querySelector('[datetime], [data-posted], [data-posted-at], [data-date], [data-datetime]');
                if (!attrNode) return '';
                for (const key of ['datetime', 'data-posted', 'data-posted-at', 'data-date', 'data-datetime']) {
                  const val = (attrNode.getAttribute(key) || '').trim();
                  if (val) return val;
                }
                return '';
              };
              return nodes.map((node) => {
                const href = node.href || '';
                const card = node.closest(
                  'article, li, [data-job-id], [class*="job"], [class*="posting"], [class*="role"], tr'
                ) || node.parentElement;
                const titleLines = (node.innerText || node.textContent || '')
                  .split(/\\n+/)
                  .map((line) => line.trim())
                  .filter(Boolean);
                const title = normalize(titleLines[0] || '', 260);
                let description = '';
                let location = '';
                if (card) {
                  const descNode = card.querySelector('[class*="description"], [class*="summary"], [class*="snippet"], [data-testid*="description"]');
                  if (descNode) {
                    description = cleanNodeText(descNode, 1200);
                  }
                  const locationNode = card.querySelector('[class*="location"], [id*="location"], [data-testid*="location"], [data-qa*="location"]');
                  if (locationNode) {
                    location = normalize(locationNode.innerText || locationNode.textContent || '', 180);
                  }
                  if (!description) {
                    const context = cleanNodeText(card, 1600);
                    if (context && context !== title) {
                      description = normalize(context.replace(title, ' '), 1200);
                    }
                  }
                }
                if (!location && titleLines.length > 1) {
                  location = normalize(titleLines.slice(1).join(' '), 180);
                }
                const postedAt = pickTime(card) || pickTime(node.parentElement) || '';
                return {
                  href,
                  text: title,
                  location,
                  description,
                  posted_at: postedAt,
                };
              });
            }
            """,
        )
    except Exception:
        return []
    return links if isinstance(links, list) else []


def listing_link_signature(page, selector: str) -> tuple[str, ...]:
    out: list[str] = []
    for link in extract_listing_links(page, selector):
        href = str((link or {}).get("href", "")).strip()
        if not href:
            continue
        out.append(urljoin(page.url, href))
        if len(out) >= 40:
            break
    return tuple(out)


def scroll_listing_page(page, selector: str, rounds: int) -> bool:
    if rounds <= 0:
        return False
    changed = False
    previous = listing_link_signature(page, selector)
    for _ in range(rounds):
        page.mouse.wheel(0, 2200)
        page.wait_for_timeout(450)
        current = listing_link_signature(page, selector)
        if current != previous:
            changed = True
            previous = current
    return changed


def advance_listing_page(page, selector: str, timeout_ms: int, wait_after_load_ms: int, timeout_error_cls) -> bool:
    before_url = page.url
    before_signature = listing_link_signature(page, selector)

    control_patterns = [
        ("role_button", re.compile(r"^(?:load|show|see|view)\s+more|^next(?:\s+page)?$", flags=re.IGNORECASE)),
        ("role_link", re.compile(r"^(?:load|show|see|view)\s+more|^next(?:\s+page)?$", flags=re.IGNORECASE)),
    ]
    locators = []
    for kind, pattern in control_patterns:
        if kind == "role_button":
            locators.append(page.get_by_role("button", name=pattern).first)
        else:
            locators.append(page.get_by_role("link", name=pattern).first)
    locators.extend(
        [
            page.locator("a[rel='next']").first,
            page.locator("button[rel='next']").first,
            page.locator("[aria-label*='next' i]").first,
            page.locator("[data-testid*='next' i]").first,
            page.locator("[class*='next' i]").first,
            page.locator("button:has-text('Load more')").first,
            page.locator("button:has-text('Show more')").first,
            page.locator("button:has-text('See more')").first,
            page.locator("a:has-text('Next')").first,
        ]
    )

    for locator in locators:
        try:
            if locator.count() == 0:
                continue
            control = locator.first
            if not control.is_visible(timeout=800):
                continue
            text = normalize_text(control.inner_text(timeout=800), 120).lower()
            aria = normalize_text(control.get_attribute("aria-label") or "", 120).lower()
            combined = f"{text} {aria}".strip()
            if any(marker in combined for marker in ("previous", "prev", "back")):
                continue
            if (control.get_attribute("disabled") or "").strip() or (control.get_attribute("aria-disabled") or "").strip().lower() == "true":
                continue
            control.scroll_into_view_if_needed(timeout=1500)
            control.click(timeout=min(timeout_ms, 4000))
            wait_for_listing_settle(page, timeout_ms=timeout_ms, wait_after_load_ms=wait_after_load_ms, timeout_error_cls=timeout_error_cls)
            after_signature = listing_link_signature(page, selector)
            if page.url != before_url or after_signature != before_signature:
                return True
        except Exception:
            continue
    return False


def score_candidate(title: str, url: str, description: str | None, posted_at: str | None) -> int:
    value = title.strip().lower()
    url_value = url.strip().lower()
    score = 0
    if posted_at:
        score += 5
    if any(hint in url_value for hint in JOB_URL_HINTS):
        score += 6
    has_role_hint = any(hint in value for hint in ROLE_TITLE_HINTS)
    if has_role_hint:
        score += 4
    if re.search(r"\d{4,}", url_value):
        score += 4
    words = len(value.split())
    if words >= 4:
        score += 3
    elif words >= 2:
        score += 1
    if description and len(description.split()) >= 20:
        score += 3
    if has_description_job_marker(description):
        score += 2
    for marker in ("/teams/", "/locations/", "/benefits/", "/about/", "/blog/", "/events/", "/news/"):
        if marker in url_value:
            score -= 4
    if looks_like_non_job_url(url_value):
        score -= 8
    if is_noise_title(title):
        score -= 10
    if not has_job_opening_signal(title=title, url=url, description=description, posted_at=posted_at):
        score -= 8
    return score


def main() -> int:
    careers_url = os.getenv("CAREERS_URL", "").strip()
    if not careers_url:
        return emit({"status": "error", "message": "CAREERS_URL is not set", "jobs": []})

    max_links = max(1, int(os.getenv("PW_MAX_LINKS", "100")))
    timeout_ms = max(5000, int(os.getenv("PW_TIMEOUT_MS", "45000")))
    wait_after_load_ms = max(0, int(os.getenv("PW_WAIT_AFTER_LOAD_MS", "2500")))
    headless = parse_bool_env("PW_HEADLESS", True)
    user_agent = os.getenv("PW_USER_AGENT", "").strip() or None
    locale = os.getenv("PW_LOCALE", "").strip() or None
    timezone_id = os.getenv("PW_TIMEZONE_ID", "").strip() or None
    accept_language = os.getenv("PW_ACCEPT_LANGUAGE", "").strip() or None
    selector = os.getenv("PW_LINK_SELECTOR", "a[href]").strip() or "a[href]"
    include_pattern = os.getenv("PW_INCLUDE_PATTERN", "").strip()
    include_re = re.compile(include_pattern, flags=re.IGNORECASE) if include_pattern else None
    location_include_pattern = os.getenv("PW_LOCATION_INCLUDE_PATTERN", "").strip()
    location_include_re = (
        re.compile(location_include_pattern, flags=re.IGNORECASE) if location_include_pattern else None
    )
    location_exclude_pattern = os.getenv("PW_LOCATION_EXCLUDE_PATTERN", "").strip()
    location_exclude_re = (
        re.compile(location_exclude_pattern, flags=re.IGNORECASE) if location_exclude_pattern else None
    )
    enrich_details = parse_bool_env("PW_ENRICH_DETAILS", True)
    detail_fetch_limit = max(0, int(os.getenv("PW_DETAIL_FETCH_LIMIT", "24")))
    detail_timeout_ms = max(5000, int(os.getenv("PW_DETAIL_TIMEOUT_MS", str(timeout_ms))))
    detail_wait_after_load_ms = max(0, int(os.getenv("PW_DETAIL_WAIT_AFTER_LOAD_MS", "800")))
    pagination_max_pages = max(1, int(os.getenv("PW_PAGINATION_MAX_PAGES", "8")))
    scroll_rounds = max(0, int(os.getenv("PW_SCROLL_ROUNDS", "4")))
    us_only = parse_bool_env("PW_ONLY_US", False)
    keywords = keyword_list()

    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import sync_playwright
    except Exception:
        return emit(
            {
                "status": "error",
                "message": "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium",
                "jobs": [],
            }
        )

    try:
        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=headless)
            context_kwargs: dict[str, object] = {}
            if user_agent:
                context_kwargs["user_agent"] = user_agent
            if locale:
                context_kwargs["locale"] = locale
            if timezone_id:
                context_kwargs["timezone_id"] = timezone_id
            if accept_language:
                context_kwargs["extra_http_headers"] = {"Accept-Language": accept_language}
            context = browser.new_context(**context_kwargs)
            page = context.new_page()
            response = page.goto(careers_url, wait_until="domcontentloaded", timeout=timeout_ms)
            if response and response.status in {401, 403, 429}:
                browser.close()
                return emit(
                    {
                        "status": "blocked",
                        "message": f"HTTP {response.status} on initial page load",
                        "jobs": [],
                    }
                )

            wait_for_listing_settle(
                page,
                timeout_ms=timeout_ms,
                wait_after_load_ms=wait_after_load_ms,
                timeout_error_cls=PlaywrightTimeoutError,
            )

            title = (page.title() or "").strip()
            if is_blocked_title(title):
                browser.close()
                return emit(
                    {
                        "status": "blocked",
                        "message": f"Challenge page detected ({title})",
                        "jobs": [],
                    }
                )

            candidates = []
            seen_urls = set()
            page_states_seen: set[tuple[str, tuple[str, ...]]] = set()
            for _ in range(pagination_max_pages):
                if scroll_rounds:
                    scroll_listing_page(page, selector=selector, rounds=scroll_rounds)

                links = extract_listing_links(page, selector)
                state_signature = (page.url, listing_link_signature(page, selector))
                if state_signature in page_states_seen:
                    break
                page_states_seen.add(state_signature)

                for link in links:
                    href = str((link or {}).get("href", "")).strip()
                    if not href:
                        continue
                    absolute_url = urljoin(page.url, href)
                    parsed = urlparse(absolute_url)
                    if parsed.scheme not in {"http", "https"}:
                        continue
                    if looks_like_asset_url(absolute_url):
                        continue
                    if looks_like_non_job_url(absolute_url):
                        continue
                    if include_re and not include_re.search(absolute_url):
                        continue

                    text = re.sub(r"\s+", " ", str((link or {}).get("text", "")).strip())
                    text = normalize_title(text)
                    if not link_matches(text, absolute_url, keywords):
                        continue
                    if is_noise_title(text):
                        continue
                    if absolute_url in seen_urls:
                        continue
                    seen_urls.add(absolute_url)
                    title = text or title_from_url(absolute_url)
                    description = normalize_description(str((link or {}).get("description", "")).strip() or None)
                    location = normalize_location(str((link or {}).get("location", "")).strip() or None)
                    if not location and not us_only:
                        location = extract_location_hint(description, title)
                    if location and normalize_text(location, 180).lower() == normalize_text(title, 180).lower():
                        location = None
                    posted_at = normalize_text(str((link or {}).get("posted_at", "")).strip(), 140) or None
                    score = score_candidate(title=title, url=absolute_url, description=description, posted_at=posted_at)
                    candidates.append(
                        {
                            "score": score,
                            "title": title,
                            "url": absolute_url,
                            "location": location,
                            "country": None,
                            "description": description,
                            "posted_at": posted_at,
                        }
                    )
                    if len(candidates) >= max_links * 8:
                        break

                if len(candidates) >= max_links * 8:
                    break
                if not advance_listing_page(
                    page,
                    selector=selector,
                    timeout_ms=timeout_ms,
                    wait_after_load_ms=wait_after_load_ms,
                    timeout_error_cls=PlaywrightTimeoutError,
                ):
                    break

            candidates.sort(key=lambda item: (int(item.get("score", 0)), str(item.get("posted_at") or "")), reverse=True)
            jobs = []
            for item in candidates:
                if int(item.get("score", 0)) < 2 and len(jobs) >= max(5, max_links // 3):
                    continue
                jobs.append(
                    {
                        "title": item["title"],
                        "url": item["url"],
                        "location": item.get("location"),
                        "country": item.get("country"),
                        "description": item.get("description"),
                        "posted_at": item.get("posted_at"),
                    }
                )
                if len(jobs) >= max_links:
                    break

            if enrich_details and detail_fetch_limit > 0 and jobs:
                detail_page = context.new_page()
                enriched = 0
                effective_detail_fetch_limit = detail_fetch_limit
                if location_include_re or location_exclude_re or us_only:
                    effective_detail_fetch_limit = max(effective_detail_fetch_limit, len(jobs))
                for item in jobs:
                    needs_title = title_needs_detail(item.get("title", ""))
                    needs_description = description_needs_detail(item.get("description"))
                    needs_posted_at = not str(item.get("posted_at", "") or "").strip()
                    needs_location = bool(location_include_re or location_exclude_re or us_only) or not str(
                        item.get("location", "") or ""
                    ).strip()
                    if not (needs_title or needs_description or needs_posted_at or needs_location):
                        continue
                    if enriched >= effective_detail_fetch_limit:
                        break
                    detail = extract_detail_for_job(
                        detail_page=detail_page,
                        job_url=str(item.get("url", "")),
                        timeout_ms=detail_timeout_ms,
                        wait_after_load_ms=detail_wait_after_load_ms,
                    )
                    if detail:
                        detailed_title = normalize_title(detail.get("title", "") or "")
                        detailed_description = normalize_description(detail.get("description"))
                        detailed_posted = normalize_text(str(detail.get("posted_at", "") or "").strip(), 140) or None
                        detailed_location = normalize_location(detail.get("location"))
                        detailed_country = normalize_country(detail.get("country"))
                        if needs_title and detailed_title and not is_noise_title(detailed_title):
                            item["title"] = detailed_title
                        if detailed_description:
                            current_words = len(str(item.get("description", "") or "").split())
                            detailed_words = len(detailed_description.split())
                            if needs_description or detailed_words > current_words + 8:
                                item["description"] = detailed_description
                        if needs_posted_at and detailed_posted:
                            item["posted_at"] = detailed_posted
                        if detailed_location:
                            item["location"] = detailed_location
                        if detailed_country:
                            item["country"] = detailed_country
                    enriched += 1
                detail_page.close()

            if location_include_re or location_exclude_re or us_only:
                filtered_jobs = []
                for item in jobs:
                    raw_location = item.get("location")
                    location = normalize_location(str(raw_location).strip() if raw_location is not None else None)
                    if not location and not us_only:
                        location = extract_location_hint(str(item.get("title", "")))
                    raw_country = item.get("country")
                    country = normalize_country(str(raw_country).strip() if raw_country is not None else None)
                    item["location"] = location
                    item["country"] = country

                    if location_include_re or location_exclude_re:
                        if not location_matches_filters(
                            location=location,
                            country=country,
                            include_re=location_include_re,
                            exclude_re=location_exclude_re,
                        ):
                            continue

                    if us_only:
                        us_value = ", ".join(part for part in [location, country] if part)
                        if not is_us_job(title=str(item.get("title", "")), location=us_value):
                            continue

                    filtered_jobs.append(item)
                jobs = filtered_jobs

            verified_jobs = []
            for item in jobs:
                title_value = str(item.get("title", "") or "").strip()
                url_value = str(item.get("url", "") or "").strip()
                description_value = normalize_description(item.get("description"))
                posted_value = normalize_text(str(item.get("posted_at", "") or "").strip(), 140) or None
                location_value = compose_output_location(
                    str(item.get("location", "") or "").strip() or None,
                    str(item.get("country", "") or "").strip() or None,
                )
                if not is_likely_job_opening(
                    title=title_value,
                    url=url_value,
                    description=description_value,
                    posted_at=posted_value,
                ):
                    continue
                item["description"] = description_value
                item["posted_at"] = posted_value
                item["location"] = location_value
                verified_jobs.append(item)
            jobs = verified_jobs

            browser.close()
            return emit(
                {
                    "status": "ok",
                    "message": f"Extracted {len(jobs)} job link(s)",
                    "jobs": jobs,
                }
            )
    except Exception as exc:  # noqa: BLE001
        message = f"{type(exc).__name__}: {exc}"
        if any(token in message.lower() for token in ["403", "429", "captcha", "challenge"]):
            return emit({"status": "blocked", "message": message, "jobs": []})
        return emit({"status": "error", "message": message, "jobs": []})


if __name__ == "__main__":
    sys.exit(main())
