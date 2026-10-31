"""Content types for the suffixes the preview serves.

Host suffix guessing is avoided on purpose: mimetypes consults /etc/mime.types,
so .js and .mjs resolve differently from system to system.
"""

import os

CONTENT_TYPES = {
    ".apng": "image/apng",
    ".avif": "image/avif",
    ".bmp": "image/bmp",
    ".css": "text/css; charset=utf-8",
    ".eot": "application/vnd.ms-fontobject",
    ".gif": "image/gif",
    ".ico": "image/x-icon",
    ".jpeg": "image/jpeg",
    ".jpg": "image/jpeg",
    ".js": "text/javascript; charset=utf-8",
    ".json": "application/json; charset=utf-8",
    ".map": "application/json; charset=utf-8",
    ".mjs": "text/javascript; charset=utf-8",
    ".otf": "font/otf",
    ".png": "image/png",
    ".svg": "image/svg+xml",
    ".ttf": "font/ttf",
    ".wasm": "application/wasm",
    ".webp": "image/webp",
    ".woff": "font/woff",
    ".woff2": "font/woff2",
}

# Only these may be served from the Markdown file's directory.
IMAGE_SUFFIXES = frozenset(
    {".apng", ".avif", ".bmp", ".gif", ".ico", ".jpeg", ".jpg", ".png", ".svg", ".webp"}
)


def suffix(name: str) -> str:
    """The lowercase extension of a file name, dot included."""
    return os.path.splitext(name)[1].lower()


def content_type(name: str) -> str:
    """The Content-Type for a file name; opaque bytes when the suffix is unknown."""
    return CONTENT_TYPES.get(suffix(name), "application/octet-stream")
