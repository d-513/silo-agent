import { Check } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useBots } from "../bots";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Switch } from "../Switch";
import { useDismiss } from "../useMenu";

/** The Bots in a Workspace the user manages: the ones they can give a connector to. */
export function useManagedBots(spaceId: string): Bot[] | null {
  const { bots } = useBots();
  return bots ? bots.filter((b) => b.spaceId === spaceId && b.role === "manage") : null;
}

// BotsStep ends an add flow: which of the Workspace's Bots get the new
// connection. Every Bot the user manages starts ticked.
export function BotsStep({ bots, value, onChange }: { bots: Bot[]; value: string[]; onChange: (ids: string[]) => void }) {
  return (
    <section className="mt-6 rounded-card bg-well p-4">
      <h3 className="text-[13.5px] font-medium text-ink">{t("Which Bots can use it")}</h3>
      {bots.length === 0 ? (
        <p className="mt-1 text-[12.5px] text-ink-3">{t("No Bot can use it yet. Give it to one later from Choose Bots.")}</p>
      ) : (
        <div className="mt-2.5 flex flex-wrap gap-2">
          {bots.map((b) => {
            const on = value.includes(b.id);
            return (
              <button
                key={b.id}
                type="button"
                aria-pressed={on}
                onClick={() => onChange(on ? value.filter((x) => x !== b.id) : [...value, b.id])}
                className={`flex h-9 items-center gap-2 rounded-control px-2.5 text-[13px] font-medium transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
                  on ? "bg-surface text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "bg-surface/60 text-ink-2 hover:bg-surface"
                }`}
              >
                <Crest index={b.crest} size={20} />
                <span className="max-w-40 truncate">{b.name}</span>
                {on ? <Check size={13} className="text-cobalt" /> : null}
              </button>
            );
          })}
        </div>
      )}
    </section>
  );
}

// UsedBy is a connection's footer: the crests of the Bots it is granted to,
// five at most, then a count.
export function UsedBy({ ids, hidden = 0 }: { ids: string[]; hidden?: number }) {
  const { bots } = useBots();
  const shown = (bots ?? []).filter((b) => ids.includes(b.id));
  const more = Math.max(0, shown.length - 5) + hidden;
  if (shown.length === 0 && hidden === 0) return <span className="text-[12px] text-ink-3">{t("No Bot uses it")}</span>;
  return (
    <span className="flex items-center gap-1.5" title={shown.map((b) => b.name).join(", ")}>
      <span className="text-[11px] font-medium text-ink-3">{t("Used by")}</span>
      {shown.slice(0, 5).map((b) => (
        <Crest key={b.id} index={b.crest} size={20} />
      ))}
      {more > 0 ? <span className="text-[11px] font-medium text-ink-3">{`+${more}`}</span> : null}
    </span>
  );
}

// ChooseBots is a connection's Bot switches in a slip: each one grants or
// takes the connection away from that Bot at once.
export function ChooseBots({
  bots,
  granted,
  onToggle,
}: {
  bots: Bot[];
  granted: string[];
  onToggle: (botId: string, on: boolean) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<CSSProperties>({});
  useDismiss(open, trigger, menu, () => setOpen(false));
  useEffect(() => {
    if (!open) return;
    const r = trigger.current?.getBoundingClientRect();
    if (r) setBox({ left: Math.min(Math.max(8, r.right - 280), window.innerWidth - 288), top: Math.min(r.bottom + 6, window.innerHeight - 320) });
  }, [open]);
  return (
    <>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="h-8 rounded-control px-2.5 text-[12.5px] font-medium text-ink-2 transition-colors hover:bg-well hover:text-ink aria-expanded:bg-well"
      >
        {t("Choose Bots")}
      </button>
      {open
        ? createPortal(
            <div ref={menu} role="dialog" aria-label={t("Choose Bots")} className="fixed z-50 max-h-[300px] w-[280px] overflow-auto rounded-control bg-surface p-1 shadow-slip" style={box}>
              <div className="rise">
                {bots.length === 0 ? (
                  <p className="px-2.5 py-2 text-[12.5px] text-ink-3">{t("You manage no Bot in this workspace.")}</p>
                ) : (
                  bots.map((b) => {
                    const on = granted.includes(b.id);
                    return (
                      <div key={b.id} className="flex h-10 items-center gap-2.5 rounded-sm px-2">
                        <Crest index={b.crest} size={20} />
                        <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{b.name}</span>
                        <Switch
                          on={on}
                          disabled={busy === b.id}
                          title={on ? t("Take it away from {{name}}", { name: b.name }) : t("Give it to {{name}}", { name: b.name })}
                          onChange={async (v) => {
                            setBusy(b.id);
                            try {
                              await onToggle(b.id, v);
                            } finally {
                              setBusy("");
                            }
                          }}
                        />
                      </div>
                    );
                  })
                )}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
