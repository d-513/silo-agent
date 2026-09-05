import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

export function ConsoleTerm({ botId, live, visible }: { botId: string; live: boolean; visible: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const [phase, setPhase] = useState<"off" | "connecting" | "connected" | "lost">("off");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!live) {
      setPhase("off");
      return;
    }
    const el = ref.current;
    if (!el) return;
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const backoff = Math.min(15000, 2000 * 2 ** Math.min(attempt, 3));
    const schedule = () => {
      if (!cancelled) retry = setTimeout(() => setAttempt((n) => n + 1), backoff);
    };
    setPhase("connecting");
    const termInst = new Terminal({
      cursorBlink: true,
      fontFamily: '"IBM Plex Mono", ui-monospace, monospace',
      fontSize: 13,
      lineHeight: 1.4,
      theme: {
        background: "#12141A",
        foreground: "#F3F0E8",
        cursor: "#F3F0E8",
        cursorAccent: "#12141A",
        selectionBackground: "#2A3F5F",
        selectionForeground: "#F3F0E8",
      },
    });
    const fit = new FitAddon();
    termInst.loadAddon(fit);
    termInst.open(el);
    fit.fit();
    fitRef.current = fit;
    termRef.current = termInst;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const sock = new WebSocket(`${proto}://${location.host}/console?bot=${botId}`);
    sock.binaryType = "arraybuffer";
    const sendSize = () => {
      if (sock.readyState !== WebSocket.OPEN) return;
      sock.send(JSON.stringify({ rows: termInst.rows, cols: termInst.cols }));
    };
    sock.onopen = () => {
      if (cancelled) return;
      setPhase("connected");
      sendSize();
      termInst.focus();
    };
    sock.onmessage = (ev) => {
      if (cancelled || typeof ev.data === "string") return;
      termInst.write(new Uint8Array(ev.data as ArrayBuffer));
    };
    sock.onerror = () => {
      if (cancelled) return;
      setPhase("lost");
    };
    sock.onclose = () => {
      if (cancelled) return;
      setPhase("lost");
      schedule();
    };
    const enc = new TextEncoder();
    const sub = termInst.onData((data) => {
      if (sock.readyState === WebSocket.OPEN) sock.send(enc.encode(data));
    });
    const subR = termInst.onResize(() => sendSize());
    return () => {
      cancelled = true;
      clearTimeout(retry);
      sub.dispose();
      subR.dispose();
      try {
        sock.close();
      } catch {
        /* already closed */
      }
      termInst.dispose();
      fitRef.current = null;
      termRef.current = null;
      el.replaceChildren();
    };
  }, [botId, live, attempt]);

  useEffect(() => {
    if (!visible) return;
    fitRef.current?.fit();
    termRef.current?.focus();
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => fitRef.current?.fit());
    ro.observe(el);
    return () => ro.disconnect();
  }, [visible]);

  return (
    <div className="relative h-full min-h-0 w-full bg-matte">
      <div ref={ref} className="silo-console h-full min-h-[320px] w-full" onMouseDown={() => termRef.current?.focus()} />
      {phase !== "connected" && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center font-mono text-[13px] text-stone">
          {phase === "off" ? "Console not connected" : phase === "connecting" ? "Opening console…" : "Console lost"}
        </div>
      )}
    </div>
  );
}
