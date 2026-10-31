"""Loopback HTTP server: the page, the Markdown file, its images, CDN assets."""

from __future__ import annotations

from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from . import media_types
from .cdn import CDN_PREFIX, CdnCache, CdnError, cdn_upstream
from .source import markdown_body, resolve_image
from .ui import build_page


class Preview:
    """What a request needs: the Markdown source, the rendered page, the cache."""

    def __init__(self, source: Path) -> None:
        self.source = source
        self.base_dir = source.parent
        self.page = build_page(source.name)
        self.cdn = CdnCache()


class PreviewServer(ThreadingHTTPServer):
    """Loopback server; threads keep parallel CDN loads from serializing."""

    daemon_threads = True

    def __init__(self, preview: Preview) -> None:
        super().__init__(("127.0.0.1", 0), PreviewHandler)
        self.preview = preview


class PreviewHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self) -> None:
        self.route()

    def do_HEAD(self) -> None:
        self.route()

    def log_message(self, format: str, *args: object) -> None:
        """Stay quiet: the browser devtools show the same requests."""

    def route(self) -> None:
        if not loopback_host(self.headers.get("Host", "")):
            self.send_text(HTTPStatus.FORBIDDEN, "forbidden host")
            return
        url = urlsplit(self.path)
        preview: Preview = self.server.preview  # type: ignore[attr-defined]
        if url.path in ("/", "/index.html"):
            self.send_body(HTTPStatus.OK, preview.page, "text/html; charset=utf-8")
        elif url.path == "/content.md":
            try:
                body = markdown_body(preview.source)
            except OSError as exc:
                self.send_text(
                    HTTPStatus.INTERNAL_SERVER_ERROR,
                    f"cannot read {preview.source.name}: {exc}",
                )
                return
            self.send_body(HTTPStatus.OK, body, "text/markdown; charset=utf-8")
        elif url.path == "/asset":
            self.send_asset(preview.base_dir, parse_qs(url.query).get("p", [""])[0])
        elif url.path.startswith(CDN_PREFIX):
            self.send_cdn(preview.cdn, url.path, url.query)
        elif url.path == "/favicon.ico":
            self.send_response(HTTPStatus.NO_CONTENT)
            self.end_headers()
        else:
            self.send_text(HTTPStatus.NOT_FOUND, "not found")

    def send_asset(self, base_dir: Path, relative: str) -> None:
        target = resolve_image(base_dir, relative)
        if target is None:
            self.send_text(HTTPStatus.NOT_FOUND, "not found")
            return
        try:
            body = target.read_bytes()
        except OSError:
            self.send_text(HTTPStatus.NOT_FOUND, "not found")
            return
        extra = (
            (("Content-Security-Policy", "sandbox"),)
            if media_types.suffix(target.name) == ".svg"
            else ()
        )
        self.send_body(
            HTTPStatus.OK,
            body,
            media_types.content_type(target.name),
            cache_control="no-cache",
            extra=extra,
        )

    def send_cdn(self, cdn: CdnCache, local_path: str, query: str) -> None:
        upstream = cdn_upstream(local_path, query)
        if upstream is None:
            self.send_text(HTTPStatus.NOT_FOUND, "not found")
            return
        try:
            body, content_type, cached = cdn.get(upstream)
        except CdnError:
            # This process can have a different network path than the browser
            # (a proxy, for one), so let the browser fetch it directly rather
            # than breaking a preview that worked before the cache.
            cdn.warn_once()
            self.send_response(HTTPStatus.TEMPORARY_REDIRECT)
            self.send_header("Location", upstream)
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        self.send_body(
            HTTPStatus.OK,
            body,
            content_type,
            cache_control="public, max-age=31536000, immutable",
            extra=(("X-Mdkite-Cache", "hit" if cached else "miss"),),
        )

    def send_text(self, status: HTTPStatus, message: str) -> None:
        self.send_body(status, message.encode() + b"\n", "text/plain; charset=utf-8")

    def send_body(
        self,
        status: HTTPStatus,
        body: bytes,
        content_type: str,
        cache_control: str = "no-store",
        extra: tuple[tuple[str, str], ...] = (),
    ) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", cache_control)
        self.send_header("X-Content-Type-Options", "nosniff")
        for name, value in extra:
            self.send_header(name, value)
        self.end_headers()
        if self.command != "HEAD":
            try:
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass


def loopback_host(host_header: str) -> bool:
    """Reject a Host that is not loopback.

    Without it, a DNS-rebinding page could read the preview from 127.0.0.1.
    """
    host = urlsplit("//" + host_header).hostname or ""
    return host in ("127.0.0.1", "localhost", "::1")
