import { MonitorPlay, SquareTerminal } from "lucide-react";
import type { ReactNode } from "react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";

// The demo's stand-in for ../bot/MachinePane.tsx: the Desktop and the Console
// are a live machine's screen and shell, and the demo has no machine.
export function MachinePane({ bot, kind, actions }: { bot: Bot; onStart: () => void; visible: boolean; kind: "desktop" | "console"; actions?: ReactNode; onError?: (s: string) => void }) {
  const Icon = kind === "console" ? SquareTerminal : MonitorPlay;
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-panel bg-hatch">
      <div className="flex h-9 shrink-0 items-center gap-2 px-3 text-small text-on-hatch/80">
        <span className="min-w-0 truncate">{bot.name}</span>
        <span className="font-mono text-label text-on-hatch/60">{kind === "console" ? t("Console") : t("Desktop")}</span>
        <span className="ml-auto flex items-center gap-1">{actions}</span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-on-hatch">
        <Icon size={28} className="opacity-60" />
        <p className="max-w-[26rem] text-body-lg">
          {kind === "console"
            ? "In a real Silo this is a shell inside the Bot's own container."
            : "In a real Silo this is the Bot's own desktop, live: you watch it work in Chromium and can take the mouse over."}
        </p>
        <p className="font-mono text-meta text-on-hatch/60">Not part of this demo: there is no machine behind it.</p>
      </div>
    </div>
  );
}
