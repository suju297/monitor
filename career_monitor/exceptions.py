class CrawlBlockedError(RuntimeError):
    """Raised when a target blocks the crawler (403/429/challenge page)."""


class CrawlFetchError(RuntimeError):
    """Raised when a target fetch fails in a non-blocked way."""

