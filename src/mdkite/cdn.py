"""Front-end libraries: served from the user cache, downloaded on first use."""

from __future__ import annotations

import hashlib
import os
import posixpath
import sys
import tempfile
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

from . import __version__, media_types

CDN_PREFIX = "/__cdn/"
CDN_HOSTS = frozenset({"cdn.jsdelivr.net", "unpkg.com"})
MAX_CDN_ASSET = 32 << 20

# mermaid@11 loads its diagram code as relative chunks, so the version is pinned
# before the page is served; a cached entry and a later chunk must not mix.
MERMAID_VERSION = "11.17.2"


class CdnError(Exception):
    """A CDN asset could not be fetched, so the browser should fetch it."""


def localize_cdn(page: str) -> str:
    """Point the UI at the cache proxy so CDN assets resolve offline."""
    page = page.replace("mermaid@11/", "mermaid@" + MERMAID_VERSION + "/")
    page = page.replace(
        "https://cdn.jsdelivr.net/", CDN_PREFIX + "https/cdn.jsdelivr.net/"
    )
    return page.replace("https://unpkg.com/", CDN_PREFIX + "https/unpkg.com/")


def cdn_upstream(local_path: str, raw_query: str) -> str | None:
    """Turn /__cdn/https/<host>/<path> back into a CDN URL, or None."""
    rest = local_path[len(CDN_PREFIX) :]
    scheme, _, rest = rest.partition("/")
    host, _, asset = rest.partition("/")
    if scheme != "https" or host.lower() not in CDN_HOSTS or not asset:
        return None
    if "\0" in asset or "\\" in asset:
        return None
    if any(part == ".." for part in asset.split("/")):
        return None
    cleaned = posixpath.normpath("/" + asset)
    if cleaned in ("/", "/.", "/.."):
        return None
    upstream = f"https://{host.lower()}{cleaned}"
    return f"{upstream}?{raw_query}" if raw_query else upstream


def cdn_host_allowed(host: str) -> bool:
    """A CDN redirect may only stay on jsDelivr, unpkg, or their subdomains."""
    host = host.lower()
    return any(host == allowed or host.endswith("." + allowed) for allowed in CDN_HOSTS)


def cache_directory() -> Path | None:
    """~/.cache/mdkite/cdn (XDG, macOS Cache, Windows LocalAppData), or None."""
    if xdg := os.environ.get("XDG_CACHE_HOME"):
        base = Path(xdg)
    elif sys.platform == "darwin":
        base = Path.home() / "Library" / "Caches"
    elif os.name == "nt":
        base = Path(os.environ.get("LOCALAPPDATA") or Path.home() / "AppData" / "Local")
    else:
        base = Path.home() / ".cache"
    directory = base / "mdkite" / "cdn"
    try:
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    except OSError as exc:
        print(f"mdkite: cannot create cache {directory}: {exc}", file=sys.stderr)
        return None
    return directory


class CdnCache:
    """Downloads on first use, then serves the cached copy.

    Requests arrive from a handful of browser connections at once; identical
    URLs downloaded twice simply write the same bytes twice, which is cheaper
    than coordinating them.
    """

    def __init__(self) -> None:
        self.directory = cache_directory()
        self._warned = False

    def get(self, upstream: str) -> tuple[bytes, str, bool]:
        """Return (body, content type, served from disk) for a CDN URL."""
        name = posixpath.basename(urlsplit(upstream).path) or "asset"
        key = hashlib.sha256(upstream.encode()).hexdigest()
        if self.directory is not None and (cached := self._read(key, name)):
            return *cached, True
        body, content_type = self._download(upstream, name)
        if self.directory is not None:
            self._write(key, body, content_type)
        return body, content_type, False

    def warn_once(self) -> None:
        """Report the fallback to direct loading once per run."""
        if not self._warned:
            self._warned = True
            print(
                "mdkite: cannot cache CDN assets; uncached ones will load directly",
                file=sys.stderr,
            )

    def _read(self, key: str, name: str) -> tuple[bytes, str] | None:
        try:
            body = (self.directory / key).read_bytes()
        except OSError:
            return None
        content_type = media_types.CONTENT_TYPES.get(media_types.suffix(name))
        if content_type is None:
            try:
                content_type = (self.directory / (key + ".type")).read_text().strip()
            except OSError:
                content_type = "application/octet-stream"
        return body, content_type

    def _download(self, upstream: str, name: str) -> tuple[bytes, str]:
        # identity: cache exactly what the CDN serves, without a compressed
        # body the proxy would have to decode.
        request = Request(
            upstream,
            headers={
                "User-Agent": f"mdkite/{__version__}",
                "Accept-Encoding": "identity",
            },
        )
        try:
            with urlopen(request, timeout=30) as response:
                final = urlsplit(response.geturl())
                if final.scheme != "https" or not cdn_host_allowed(
                    final.hostname or ""
                ):
                    raise CdnError(f"redirect to {response.geturl()}")
                if response.status != 200:
                    raise CdnError(f"{upstream}: cdn returned {response.status}")
                body = response.read(MAX_CDN_ASSET + 1)
                served_type = response.headers.get_content_type()
        except (HTTPError, URLError, OSError, ValueError) as exc:
            raise CdnError(f"{upstream}: {exc}") from exc
        if len(body) > MAX_CDN_ASSET:
            raise CdnError(f"{upstream}: asset exceeds {MAX_CDN_ASSET} bytes")
        return body, media_types.CONTENT_TYPES.get(
            media_types.suffix(name)
        ) or served_type

    def _write(self, key: str, body: bytes, content_type: str) -> None:
        try:
            handle, temp = tempfile.mkstemp(dir=self.directory, prefix=".asset-")
            with os.fdopen(handle, "wb") as out:
                out.write(body)
            os.replace(temp, self.directory / key)
            (self.directory / (key + ".type")).write_text(content_type)
        except OSError as exc:
            print(f"mdkite: cannot cache {key}: {exc}", file=sys.stderr)
