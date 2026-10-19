"""AI worker package."""

__all__ = ["health"]


def health() -> dict[str, str]:
    """Return a health payload that identifies this process."""
    return {"status": "ok", "service": "ai-worker"}
