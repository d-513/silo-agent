"""chrome_page asks the worker to open Chromium before it touches Playwright."""

from __future__ import annotations

import silo_runtime


def test_chrome_page_ensures():
    seen = []
    silo_runtime._post = lambda path, body: seen.append(path) or {}
    silo_runtime._pw = None
    silo_runtime._browser = None
    try:
        silo_runtime.chrome_page()
    except Exception:
        pass
    if seen[:1] != ["/v1/chrome/ensure"]:
        raise SystemExit(f"ensure first: {seen}")


if __name__ == "__main__":
    test_chrome_page_ensures()
    print("ok")
