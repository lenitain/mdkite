"""Command line entry point: one file, one browser tab."""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

from . import UI_VERSION, __version__
from .server import Preview, PreviewServer


def parse_arguments(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="mdkite",
        description="Render one Markdown file in the browser. Reload the tab after "
        "saving to see changes.",
        epilog="Front-end assets are downloaded on first use and kept in the user cache "
        "directory.\n"
        f"Preview UI: mdkite.nvim {UI_VERSION} (MIT), "
        "Copyright (c) 2025 Selim Acerbas.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("file", help="Markdown file to render")
    parser.add_argument(
        "--version", action="version", version=f"%(prog)s {__version__}"
    )
    return parser.parse_args(argv)


def open_browser(url: str) -> bool:
    """Hand the URL to the desktop's default browser."""
    if sys.platform == "darwin":
        command = ["open", url]
    elif os.name == "nt":
        command = ["rundll32", "url.dll,FileProtocolHandler", url]
    else:
        command = ["xdg-open", url]
    try:
        subprocess.Popen(
            command,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
    except OSError as exc:
        print(f"mdkite: cannot open browser: {exc}", file=sys.stderr)
        return False
    return True


def main(argv: list[str] | None = None) -> int:
    args = parse_arguments(argv)
    source = Path(args.file).expanduser()
    if not source.is_file():
        print(f"mdkite: cannot open {args.file}", file=sys.stderr)
        return 1
    source = source.resolve()
    try:
        source.read_bytes()
    except OSError as exc:
        print(f"mdkite: cannot read {args.file}: {exc}", file=sys.stderr)
        return 1

    preview = Preview(source)
    server = PreviewServer(preview)
    host, port = server.server_address[:2]
    url = f"http://{host}:{port}/"

    print(f"mdkite {__version__}", file=sys.stderr)
    print(f"  file  {source}", file=sys.stderr)
    if preview.cdn.directory is not None:
        print(f"  cache {preview.cdn.directory}", file=sys.stderr)
    print(f"  url   {url}", file=sys.stderr)
    print("  stop  Ctrl+C", file=sys.stderr)

    if not open_browser(url):
        print(f"mdkite: open {url} manually", file=sys.stderr)

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
