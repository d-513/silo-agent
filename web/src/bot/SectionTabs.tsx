import { Book, Box, HardDrive, Key, ListChecks, Plug, Radio, SlidersHorizontal, Waypoints, type LucideIcon } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Link, Outlet } from "@tanstack/react-router";
import { useBotPage } from "./context";
import { FadeScroll } from "./FadeScroll";
import type { CustomizeTab, SettingsTab } from "./tabs";

// One 2px cobalt underline that slides to the active tab. It measures the tab
// marked data-tab-on in the strip it sits in, and re-measures on resize and
// once fonts settle. The strip is found from the mark itself: a parent's ref is
// not attached yet when a child's layout effect first runs.
function TabUnderline({ tab }: { tab: string }) {
  const mark = useRef<HTMLSpanElement>(null);
  const [box, setBox] = useState<{ x: number; w: number } | null>(null);
  const [live, setLive] = useState(false);
  useLayoutEffect(() => {
    const el = mark.current?.parentElement;
    if (!el) return;
    const measure = () => {
      const on = el.querySelector<HTMLElement>("[data-tab-on]");
      if (!on) {
        setBox(null);
        return;
      }
      setBox((cur) => (cur && cur.x === on.offsetLeft && cur.w === on.offsetWidth ? cur : { x: on.offsetLeft, w: on.offsetWidth }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    for (const c of Array.from(el.children)) ro.observe(c);
    let dead = false;
    void document.fonts?.ready.then(() => {
      if (!dead) measure();
    });
    return () => {
      dead = true;
      ro.disconnect();
    };
  }, [tab]);
  useEffect(() => {
    // Skip the slide on first placement; animate every move after.
    if (!box || live) return;
    const r = requestAnimationFrame(() => setLive(true));
    return () => cancelAnimationFrame(r);
  }, [box, live]);
  return (
    <span
      ref={mark}
      aria-hidden
      className={`pointer-events-none absolute bottom-0 left-0 h-[2px] rounded-full bg-cobalt ${box ? "" : "invisible"} ${
        live ? "transition-[transform,width] duration-[280ms] ease-quiet motion-reduce:transition-none" : ""
      }`}
      style={{ width: box?.w ?? 0, transform: `translateX(${box?.x ?? 0}px)` }}
    />
  );
}

function TabStrip({ children, tab }: { children: ReactNode; tab: string }) {
  return (
    <FadeScroll className="min-h-0 flex-1 self-stretch" innerClass="flex h-full items-stretch gap-0.5">
      {children}
      <TabUnderline tab={tab} />
    </FadeScroll>
  );
}

type SectionTab = CustomizeTab | SettingsTab;
type Item = { [T in SectionTab]: { tab: T; to: `/bots/$botId/${T}`; label: string; icon: LucideIcon } }[SectionTab];

// A section of a Bot with a few pages of its own: its name, one tab per page,
// and the open page under them.
function Section({ title, items }: { title: string; items: readonly Item[] }) {
  const { id, tab } = useBotPage();
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:px-5">
        <h1 className="hidden shrink-0 text-card-title leading-5 wide:block">{title}</h1>
        <TabStrip tab={tab}>
          {items.map(({ tab: t, to, label, icon: Icon }) => {
            const on = tab === t;
            return (
              <Link
                key={t}
                to={to}
                params={{ botId: id }}
                data-tab-on={on || undefined}
                className={`flex h-8 shrink-0 items-center gap-1.5 self-center whitespace-nowrap rounded-sm px-2.5 text-[13px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
                  on ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"
                }`}
              >
                <Icon size={15} />
                {label}
              </Link>
            );
          })}
        </TabStrip>
      </header>
      <div className="flex min-h-0 flex-1">
        <Outlet />
      </div>
    </section>
  );
}

const customize: readonly Item[] = [
  { tab: "connectors", to: "/bots/$botId/connectors", label: "Connectors", icon: Plug },
  { tab: "skills", to: "/bots/$botId/skills", label: "Skills", icon: Book },
  { tab: "drives", to: "/bots/$botId/drives", label: "Drives", icon: HardDrive },
  { tab: "channels", to: "/bots/$botId/channels", label: "Channels", icon: Radio },
];

const settings: readonly Item[] = [
  { tab: "settings", to: "/bots/$botId/settings", label: "General", icon: SlidersHorizontal },
  { tab: "rules", to: "/bots/$botId/rules", label: "Rules", icon: ListChecks },
  { tab: "secrets", to: "/bots/$botId/secrets", label: "Secrets", icon: Key },
  { tab: "tunnels", to: "/bots/$botId/tunnels", label: "Tunnels", icon: Waypoints },
  { tab: "container", to: "/bots/$botId/container", label: "Containers", icon: Box },
];

/** Customize: what the Bot can reach and use. */
export function CustomizeLayout() {
  return <Section title="Customize" items={customize} />;
}

/** Settings: the Bot itself, what it may do, and what it runs on. */
export function SettingsLayout() {
  return <Section title="Settings" items={settings} />;
}
