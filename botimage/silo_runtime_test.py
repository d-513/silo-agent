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


def test_web_search_call():
    seen = []

    def post(path, body):
        seen.append((path, body))
        return {"result": {"results": []}}

    silo_runtime._post = post
    out = silo_runtime.web_search("alpha", 3)
    if out != {"results": []}:
        raise SystemExit(f"result {out}")
    if seen != [("/v1/tools/call", {"connector": "web", "action": "search", "args": {"query": "alpha", "max_results": 3}})]:
        raise SystemExit(f"body {seen}")
    seen.clear()
    silo_runtime.web_search("beta")
    if seen[0][1]["args"] != {"query": "beta"}:
        raise SystemExit(f"omit max {seen}")


if __name__ == "__main__":
    test_chrome_page_ensures()
    test_web_search_call()
    print("ok")
