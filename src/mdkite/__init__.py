"""mdkite renders one Markdown file in the browser."""

from importlib.metadata import PackageNotFoundError, version

try:
    __version__ = version("mdkite")
except PackageNotFoundError:  # running from a source tree that was never installed
    __version__ = "unknown"

# The preview UI is derived from this mdkite.nvim release.
UI_VERSION = "v2.1.0"
