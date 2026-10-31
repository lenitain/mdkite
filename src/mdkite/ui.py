"""The preview page: the bundled UI, titled and pointed at the cache proxy."""

from __future__ import annotations

import html
from importlib.resources import files

from .cdn import localize_cdn


def asset_text(name: str) -> str:
    """Read a UI asset shipped inside the package."""
    return (files("mdkite") / "assets" / name).read_bytes().decode("utf-8")


def build_page(base_name: str) -> bytes:
    """The preview UI, titled with the file name and carrying the UI licence."""
    page = asset_text("index.html")
    title = html.escape(base_name) + " · mdkite"
    page = page.replace("<title>mdkite</title>", f"<title>{title}</title>", 1)
    # A comment before <!doctype> would force quirks mode, so it goes inside
    # <head>. The notice travels even when only the package is handed on.
    page = page.replace("<head>", "<head>\n<!--\n" + asset_text("LICENSE") + "-->", 1)
    return localize_cdn(page).encode()
