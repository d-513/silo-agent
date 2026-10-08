import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { LinkSurface, retryDelay, useLinkPhase } from "./machineLink";
import { useTheme, type Theme } from "./theme";
import { palette } from "./tokens";

// xterm paints to a canvas, so it takes hex. Only the ground follows the
// theme: the hatch is dark in both, a shade deeper in the dark room.
function termTheme(theme: Theme) {
  const matte = theme === "dark" ? palette.matteDark : palette.matte;
  return {
    background: matte,
    foreground: palette.canvas,
    cursor: palette.canvas,
    cursorAccent: matte,
    selectionBackground: palette.cobalt,
    selectionForeground: palette.white,
  };
}

export function ConsoleTerm({ botId, live, visible }: { botId: string; live: boolean; visible: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const { phase, setPhase, attempt, nextAttempt } = useLinkPhase();
  const theme = useTheme();
  const themeRef = useRef(theme);
  themeRef.current = theme;
  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = termTheme(theme);
  }, [theme]);

  useEffect(() => {
    if (!live) {
      setPhase("off");
      return;
    }
    const el = ref.current;
    if (!el) return;
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const schedule = () => {
      if (!cancelled) retry = setTimeout(nextAttempt, retryDelay(attempt));
    };
    setPhase("connecting");
    const termInst = new Terminal({
      cursorBlink: true,
      fontFamily: '"Geist Mono Variable", "Geist Mono", ui-monospace, monospace',
      fontSize: 13,
      lineHeight: 1.4,
      theme: termTheme(themeRef.current),
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
  }, [botId, live, attempt, setPhase, nextAttempt]);

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

  return <LinkSurface noun="Console" phase={phase} surfaceRef={ref} surfaceClass="silo-console" onMouseDown={() => termRef.current?.focus()} />;
}
