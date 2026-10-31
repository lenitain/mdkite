"""The Markdown source side: the body to serve and the images next to it."""

from __future__ import annotations

import posixpath
from pathlib import Path

from . import media_types


def markdown_body(source: Path) -> bytes:
    """The file as Markdown, wrapping a bare mermaid diagram in a fence."""
    body = source.read_bytes()
    if source.suffix.lower() not in (".mmd", ".mermaid"):
        return body
    return b"```mermaid\n" + body + (b"" if body.endswith(b"\n") else b"\n") + b"```\n"


def resolve_image(base_dir: Path, relative: str) -> Path | None:
    """Resolve a Markdown-relative image path, refusing anything outside base_dir.

    Links are resolved before the containment check, so an image pointing out
    of the Markdown file's directory is not served.
    """
    if not relative or "\0" in relative or "\\" in relative or ":" in relative:
        return None
    if relative.startswith("/"):
        return None
    if posixpath.basename(relative) in ("", ".", ".."):
        return None
    if media_types.suffix(relative) not in media_types.IMAGE_SUFFIXES:
        return None
    try:
        base = base_dir.resolve()
        target = (base / relative).resolve(strict=True)
    except OSError:
        return None
    # The link name can point at a file of another kind; judge the served name.
    if media_types.suffix(target.name) not in media_types.IMAGE_SUFFIXES:
        return None
    if not target.is_relative_to(base) or not target.is_file():
        return None
    return target
