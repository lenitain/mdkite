"""Allow `python -m mdkite` alongside the installed `mdkite` command."""

import sys

from .cli import main

sys.exit(main())
