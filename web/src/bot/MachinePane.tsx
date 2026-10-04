import { useEffect, useRef } from "react";
import { ConsoleTerm } from "../Console";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { Lamp, statusText } from "../Lamp";
import { LinkSurface, retryDelay, useLinkPhase } from "../machineLink";
import { NeedMachine } from "../NeedMachine";

function Hatch({ botId, live, visible }: { botId: string; live: boolean; visible: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const { phase, setPhase, attempt, nextAttempt } = useLinkPhase();
  useEffect(() => {
    if (!live) {
      setPhase("off");
      return;
    }
    const el = ref.current;
    if (!el) return;
    let rfb: { disconnect: () => void } | null = null;
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let hang: ReturnType<typeof setTimeout> | undefined;
    const schedule = () => {
      if (!cancelled) retry = setTimeout(nextAttempt, retryDelay(attempt));
    };
    setPhase("connecting");
    const start = window.setTimeout(() => {
      if (cancelled || !el.isConnected) return;
      (async () => {
        const mod = await import("@novnc/novnc");
        if (cancelled || !el.isConnected) return;
        const RFB = mod.default;
        const proto = location.protocol === "https:" ? "wss" : "ws";
        const next = new RFB(el, `${proto}://${location.host}/vnc?bot=${botId}`, { shared: true });
        next.scaleViewport = true;
        next.clipViewport = true;
        next.background = "var(--color-matte)";
        next.addEventListener("connect", () => {
          if (cancelled) return;
          clearTimeout(hang);
          setPhase("connected");
        });
        next.addEventListener("disconnect", () => {
          if (cancelled) return;
          setPhase("lost");
          schedule();
        });
        next.addEventListener("securityfailure", () => {
          if (cancelled) return;
          setPhase("lost");
          schedule();
        });
        rfb = next;
        hang = setTimeout(() => {
          if (cancelled) return;
          try {
            rfb?.disconnect();
          } catch {
            /* retry via disconnect */
          }
        }, 8000);
      })().catch((e) => {
        console.error(e);
        if (!cancelled) {
          setPhase("lost");
          schedule();
        }
      });
    }, 50);
    return () => {
      cancelled = true;
      window.clearTimeout(start);
      clearTimeout(retry);
      clearTimeout(hang);
      try {
        rfb?.disconnect();
      } catch {
        /* already closed */
      }
      el.replaceChildren();
    };
  }, [botId, live, attempt, setPhase, nextAttempt]);
  useEffect(() => {
    if (visible) window.dispatchEvent(new Event("resize"));
  }, [visible]);
  return <LinkSurface noun="Desktop" phase={phase} surfaceRef={ref} surfaceClass="silo-hatch" />;
}

export function MachinePane({
  bot,
  onStart,
  visible,
  kind,
}: {
  bot: Bot;
  onStart: () => void;
  visible: boolean;
  kind: "desktop" | "console";
}) {
  const label = kind === "console" ? "Console" : "Desktop";
  if (!bot.workerConnected) {
    return (
      <NeedMachine
        copy={`${label} not connected`}
        starting={bot.status === "starting"}
        onStart={onStart}
      />
    );
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-panel bg-hatch">
      <div className="flex h-9 shrink-0 items-center gap-2 px-3 text-[13px] text-white/80">
        <Crest index={bot.crest} size={20} />
        <span className="min-w-0 truncate">{bot.name}</span>
        <span className="font-mono text-[12px] text-white/60">{label}</span>
        <Lamp status={bot.status} />
        <span className="ml-auto font-mono text-[12px] text-white/80">{statusText(bot.status)}</span>
      </div>
      <div className="mx-2 mb-2 min-h-0 flex-1 overflow-hidden rounded-sm bg-matte">
        {kind === "console" ? <ConsoleTerm botId={bot.id} live visible={visible} /> : <Hatch botId={bot.id} live visible={visible} />}
      </div>
    </div>
  );
}
