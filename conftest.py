"""Make the extracted modules importable without installing the package.

🔴 `lib/` AND `tests/` BOTH GO ON THE PATH, and neither is a package. The modules
import each other by bare name (`from entry_shape import …`), which is what lets
the same files be vendored into a consumer's own tree unchanged. A package
rename would be a silent behaviour change for every one of those imports.
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
for p in (ROOT / "lib", ROOT / "tests", ROOT / "server"):
    s = str(p)
    if s not in sys.path:
        sys.path.insert(0, s)
