import { Hand, Play } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ui } from "../api";
import { ConsoleTerm } from "../Console";
import { Crest } from "../Crest";
import { fail } from "../errors";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { Lamp, statusText } from "../Lamp";
import { LinkSurface, retryDelay, useLinkPhase } from "../machineLink";
import { NeedMachine } from "../NeedMachine";
import { setBot } from "../query";
import { Tip, TipAction, TipTitle } from "../Tip";
import { t } from "../i18n.ts";

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
  return <LinkSurface noun={t("Desktop")} phase={phase} surfaceRef={ref} surfaceClass="silo-hatch" />;
}

// PauseSwitch hands the desktop to the owner: while it is on, the Bot's
// clicks, keys, typing, scrolling and Chromium are refused (it can still look).
function PauseSwitch({ bot, onError }: { bot: Bot; onError?: (s: string) => void }) {
  const [busy, setBusy] = useState(false);
  const on = bot.desktopPaused;
  async function flip() {
    if (busy) return;
    setBusy(true);
    try {
      const res = await ui.setDesktopPaused({ botId: bot.id, paused: !on });
      setBot({ ...bot, desktopPaused: res.paused });
    } catch (e) {
      onError?.(fail(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Tip
      content={
        <div className="max-w-[280px]">
          <TipTitle aside={on ? t("Paused") : t("Running")}>{t("Pause the Bot on the desktop")}</TipTitle>
          <p className="mt-1 text-[12px] leading-[17px] text-ink-2">
            {t("While paused, the Bot cannot click, type, press keys, scroll or use Chromium here. It is told you are in control. It can still look.")}
          </p>
          <TipAction>{on ? t("Click to hand the desktop back") : t("Click to take over")}</TipAction>
        </div>
      }
    >
      <button
        type="button"
        role="switch"
        aria-checked={on}
        aria-busy={busy}
        onClick={flip}
        className={`flex h-6 shrink-0 items-center gap-1.5 rounded-full px-2.5 font-mono text-[11.5px] transition-colors duration-[160ms] ease-quiet ${
          on ? "bg-vermilion text-on-accent hover:bg-vermilion-deep" : "text-on-hatch/75 shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--color-on-hatch)_18%,transparent)] hover:bg-on-hatch/10 hover:text-on-hatch"
        } ${busy ? "opacity-60" : ""}`}
      >
        {on ? <Play size={12} /> : <Hand size={12} />}
        {on ? t("You're in control · Resume") : t("Pause Bot")}
      </button>
    </Tip>
  );
}

export function MachinePane({
  bot,
  onStart,
  visible,
  kind,
  actions,
  onError,
}: {
  bot: Bot;
  onStart: () => void;
  visible: boolean;
  kind: "desktop" | "console";
  // Controls for the end of the hatch's header (dock, open as a page, close).
  actions?: ReactNode;
  onError?: (s: string) => void;
}) {
  const label = kind === "console" ? t("Console") : t("Desktop");
  if (!bot.workerConnected) {
    return (
      <NeedMachine
        copy={t("{{name}} not connected", { name: label })}
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
        {kind === "desktop" ? <PauseSwitch bot={bot} onError={onError} /> : null}
        {actions ? <span className="-mr-1.5 flex shrink-0 items-center gap-0.5">{actions}</span> : null}
      </div>
      <div
        className={`mx-2 mb-2 min-h-0 flex-1 overflow-hidden rounded-sm bg-matte ${
          kind === "desktop" && bot.desktopPaused ? "shadow-[0_0_0_2px_var(--color-vermilion)]" : ""
        }`}
      >
        {kind === "console" ? <ConsoleTerm botId={bot.id} live visible={visible} /> : <Hatch botId={bot.id} live visible={visible} />}
      </div>
    </div>
  );
}
