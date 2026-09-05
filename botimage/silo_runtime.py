"""Local tools bus. Talks to the Worker unix socket. Never talks to the Control Plane."""

from __future__ import annotations

import json
import os
import socket
from http.client import HTTPConnection


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
