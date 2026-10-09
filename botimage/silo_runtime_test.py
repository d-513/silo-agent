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


def test_transcribe_call():
    seen = []

    def post(path, body):
        seen.append((path, body))
        return {"result": {"text": "hi", "language": "en", "seconds": 1.0}}

    silo_runtime._post = post
    out = silo_runtime.transcribe("memo.mp3", "en")
    if out["text"] != "hi":
        raise SystemExit(f"result {out}")
    if seen != [("/v1/tools/call", {"connector": "bot", "action": "transcribe", "args": {"path": "memo.mp3", "language": "en"}})]:
        raise SystemExit(f"body {seen}")
    seen.clear()
    silo_runtime.transcribe("memo.mp3")
    if seen[0][1]["args"] != {"path": "memo.mp3"}:
        raise SystemExit(f"omit language {seen}")


def test_taskboard_calls():
    seen = []

    def post(path, body):
        seen.append(body)
        return {"result": {"items": []}}

    silo_runtime._post = post
    silo_runtime.task_add("[bob] one")
    silo_runtime.task_done(2)
    silo_runtime.task_done([1, 3], "saved")
    silo_runtime.task_list()
    silo_runtime.task_reset()
    want = [
        {"connector": "tasks", "action": "add", "args": {"tasks": ["[bob] one"]}},
        {"connector": "tasks", "action": "done", "args": {"ids": [2]}},
        {"connector": "tasks", "action": "done", "args": {"ids": [1, 3], "note": "saved"}},
        {"connector": "tasks", "action": "read", "args": {}},
        {"connector": "tasks", "action": "reset", "args": {}},
    ]
    if seen != want:
        raise SystemExit(f"bodies {seen}")

def test_tunnel_calls():
    seen = []

    def post(path, body):
        seen.append((path, body))
        return {"result": {"ok": True}}

    silo_runtime._post = post
    silo_runtime.open_tunnel(8000)
    silo_runtime.open_tunnel(8001, public=True)
    silo_runtime.list_tunnels()
    silo_runtime.close_tunnel(name="quiet-amber-heron")
    silo_runtime.close_tunnel(port=8001)
    args = [(b["connector"], b["action"], b["args"]) for _, b in seen]
    want = [
        ("tunnels", "open", {"port": 8000}),
        ("tunnels", "open", {"port": 8001, "public": True}),
        ("tunnels", "list", {}),
        ("tunnels", "close", {"name": "quiet-amber-heron"}),
        ("tunnels", "close", {"port": 8001}),
    ]
    if args != want:
        raise SystemExit(f"calls {args}")


def test_mail_calls():
    seen = []

    def post(path, body):
        seen.append((path, body))
        return {"result": {"ok": True}}

    silo_runtime._post = post
    silo_runtime.list_mail()
    silo_runtime.list_mail(unread=True, limit=5)
    silo_runtime.read_mail("9f2c41aa")
    silo_runtime.read_mail("9f2c41aa", save_attachments=True, offset=30000)
    args = [(b["connector"], b["action"], b["args"]) for _, b in seen]
    want = [
        ("mailbox", "list", {}),
        ("mailbox", "list", {"unread": True, "limit": 5}),
        ("mailbox", "read", {"id": "9f2c41aa"}),
        ("mailbox", "read", {"id": "9f2c41aa", "save_attachments": True, "offset": 30000}),
    ]
    if args != want:
        raise SystemExit(f"calls {args}")


if __name__ == "__main__":
    test_chrome_page_ensures()
    test_web_search_call()
    test_transcribe_call()
    test_taskboard_calls()
    test_tunnel_calls()
    test_mail_calls()
    print("ok")
