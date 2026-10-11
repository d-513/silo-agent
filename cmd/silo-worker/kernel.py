# Driver of one persistent Python session (see kernel.go). The worker writes
# one JSON request per line on stdin: {"n": nonce, "code": ..., "env": {...}}.
# Each is run in the same namespace, so what a cell defines the next one has.
# Output goes straight to stdout/stderr (one pipe); a marker line the cell
# cannot guess ends it and carries its exit status.
import json
import linecache
import os
import signal
import sys
import traceback


def _serve():
    # The requests come on a private copy of stdin; the cell's own stdin is
    # empty, so input() cannot eat one.
    ctl = os.fdopen(os.dup(0), "r", encoding="utf-8", newline="\n")
    null = os.open(os.devnull, os.O_RDONLY)
    os.dup2(null, 0)
    os.close(null)
    signal.signal(signal.SIGINT, signal.default_int_handler)

    ns = {"__name__": "__main__", "__builtins__": __builtins__}
    count = 0
    while True:
        try:
            line = ctl.readline()
        except KeyboardInterrupt:
            continue  # an interrupt that arrived after its cell ended
        if not line:
            return
        req = json.loads(line)
        count += 1
        status = 0
        try:
            for key, value in req.get("env", {}).items():
                if value:
                    os.environ[key] = value
                else:
                    os.environ.pop(key, None)
            name = "<cell %d>" % count
            code = req["code"]
            # Tracebacks quote the failing line of a cell from here.
            linecache.cache[name] = (len(code), None, code.splitlines(True), name)
            try:
                exec(compile(code, name, "exec"), ns)
            except SystemExit as e:
                if e.code is None:
                    status = 0
                elif isinstance(e.code, int):
                    status = e.code
                else:
                    print(e.code, file=sys.stderr)
                    status = 1
            except KeyboardInterrupt:
                kind, err, tb = sys.exc_info()
                traceback.print_exception(kind, err, tb.tb_next)
                status = 130
            except BaseException:
                kind, err, tb = sys.exc_info()
                traceback.print_exception(kind, err, tb.tb_next)
                status = 1
            for stream in (sys.stdout, sys.stderr):
                try:
                    stream.flush()
                except Exception:
                    pass
        except KeyboardInterrupt:
            status = 130
        os.write(1, ("\x00SILO-CELL-END %s %d\x00\n" % (req["n"], status)).encode())


_serve()
