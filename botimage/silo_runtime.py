"""Local tools bus. Talks to the Worker unix socket. Never talks to the Control Plane.

Chromium: chrome_page() is Playwright on the headed desktop browser.
The worker opens silo-chromium if CDP :9222 is down.
Web: web_search(query) runs on the Control Plane.
"""

from __future__ import annotations

import json
import os
import socket
import time
from http.client import HTTPConnection

_CDP = "http://127.0.0.1:9222"
_pw = None
_browser = None


class _UDS(HTTPConnection):
    def __init__(self, path: str):
        super().__init__("localhost")
        self._path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self._path)


def _post(path: str, body: dict) -> dict:
    sock = os.environ.get("SILO_WORKER_SOCK", "/var/run/silo/worker.sock")
    c = _UDS(sock)
    raw = json.dumps(body).encode()
    c.request("POST", path, body=raw, headers={"Content-Type": "application/json"})
    res = c.getresponse()
    data = json.loads(res.read().decode() or "{}")
    c.close()
    if data.get("error"):
        raise RuntimeError(data["error"])
    return data


def get_secret(name: str) -> str:
    body = {"name": name}
    rid = os.environ.get("SILO_RUN_ID")
    if rid:
        body["run_id"] = rid
    return _post("/v1/secrets/get", body)["value"]


def chrome_page():
    """Playwright Page on the headed desktop Chromium. Opens it if needed."""
    global _pw, _browser
    _post("/v1/chrome/ensure", {})
    from playwright.sync_api import sync_playwright

    if _browser is None:
        _pw = sync_playwright().start()
        _browser = _pw.chromium.connect_over_cdp(_CDP)
    for _ in range(25):
        if _browser.contexts and _browser.contexts[0].pages:
            return _browser.contexts[0].pages[-1]
        time.sleep(0.1)
    ctx = _browser.contexts[0] if _browser.contexts else _browser.new_context()
    return ctx.new_page()


def look() -> str:
    data = call("desktop", "look", {})
    path = data.get("path") if isinstance(data, dict) else None
    return path if isinstance(path, str) and path else "bot/screen.png"


def click(x: int, y: int, button: str = "left") -> dict:
    return call("desktop", "click", {"x": x, "y": y, "button": button})


def type_text(text: str) -> dict:
    return call("desktop", "type", {"text": text})


def key(name: str) -> dict:
    return call("desktop", "key", {"name": name})


def scroll(x: int, y: int, dy: int) -> dict:
    return call("desktop", "scroll", {"x": x, "y": y, "dy": dy})


def web_search(query: str, max_results: int | None = None) -> dict:
    """Search the public web. Returns {results: [{title, url, snippet}, ...]}."""
    args: dict = {"query": query}
    if max_results is not None:
        args["max_results"] = max_results
    return call("web", "search", args)


def call(connector: str, action: str, args: dict | None = None) -> dict:
    clean = {k: v for k, v in (args or {}).items() if v is not None}
    body = {"connector": connector, "action": action, "args": clean}
    rid = os.environ.get("SILO_RUN_ID")
    if rid:
        body["run_id"] = rid
    data = _post("/v1/tools/call", body)
    result = data.get("result")
    if isinstance(result, dict):
        return result
    return {"result": result}
