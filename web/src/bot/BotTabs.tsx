import { Book, Box, ChevronDown, Folder, HardDrive, Key, ListChecks, MessageCircle, Monitor, Plug, Radio, SlidersHorizontal, SquareTerminal } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Link, NavLink } from "react-router-dom";
import { FadeScroll } from "./FadeScroll";
import { onChatSide, tabs, type NavTab, type Tab } from "./tabs";

const tabMeta: Record<NavTab, { label: string; icon: typeof MessageCircle }> = {
  run: { label: "Chat", icon: MessageCircle },
  desktop: { label: "Desktop", icon: Monitor },
  files: { label: "Files", icon: Folder },
  drives: { label: "Drives", icon: HardDrive },
  connectors: { label: "Connectors", icon: Plug },
  channels: { label: "Channels", icon: Radio },
  skills: { label: "Skills", icon: Book },
  secrets: { label: "Secrets", icon: Key },
  rules: { label: "Rules", icon: ListChecks },
  container: { label: "Containers", icon: Box },
  settings: { label: "Settings", icon: SlidersHorizontal },
};

// Under 1280px the header's Start/Stop shows only the power glyph.
export const machineBtnCompact =
  "@max-[1280px]:w-10 @max-[1280px]:justify-center @max-[1280px]:gap-0 @max-[1280px]:px-0 @max-[1280px]:pr-0 @max-[1280px]:[&>span:last-child]:bg-transparent";

function tabClass(on: boolean, compact?: boolean) {
  return `flex h-8 shrink-0 items-center gap-1.5 self-center whitespace-nowrap rounded-sm text-[13px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
    compact && !on ? "min-w-10 justify-center px-2" : "px-2.5"
  } ${on ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"}`;
}

function TabLink({
  to,
  on,
  icon: Icon,
  label,
  compact,
}: {
  to: string;
  on: boolean;
  icon: typeof MessageCircle;
  label: string;
  compact?: boolean;
}) {
  return (
    <NavLink to={to} title={label} data-tab-on={on || undefined} className={tabClass(on, compact)}>
      <Icon size={15} />
      {(!compact || on) && label}
    </NavLink>
  );
}

// One 2px cobalt underline that slides to the active tab. It measures the tab
// marked data-tab-on, and re-measures on resize and once fonts settle.
function TabUnderline({ strip, tab }: { strip: RefObject<HTMLDivElement | null>; tab: string }) {
  const [box, setBox] = useState<{ x: number; w: number } | null>(null);
  const [live, setLive] = useState(false);
  useLayoutEffect(() => {
    const el = strip.current;
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
  }, [strip, tab]);
  useEffect(() => {
    // Skip the slide on first placement; animate every move after.
    if (!box || live) return;
    const r = requestAnimationFrame(() => setLive(true));
    return () => cancelAnimationFrame(r);
  }, [box, live]);
  if (!box) return null;
  return (
    <span
      aria-hidden
      className={`pointer-events-none absolute bottom-0 left-0 h-[2px] rounded-full bg-cobalt ${ live ? "transition-[transform,width] duration-[280ms] ease-quiet motion-reduce:transition-none" : ""
      }`}
      style={{ width: box.w, transform: `translateX(${box.x}px)` }}
    />
  );
}

export function TabStrip({ className, innerClass, children, tab }: { className: string; innerClass: string; children: ReactNode; tab: string }) {
  const strip = useRef<HTMLDivElement>(null);
  return (
    <FadeScroll className={className} innerClass={innerClass} innerRef={strip}>
      {children}
      <TabUnderline strip={strip} tab={tab} />
    </FadeScroll>
  );
}

export function BotTabs({
  id,
  tab,
  chatId,
  splitMachine,
  compact,
}: {
  id: string;
  tab: Tab;
  chatId?: string;
  splitMachine?: boolean;
  compact?: boolean;
}) {
  return (
    <>
      {tabs.map((t) => {
        if (t === "desktop") {
          if (splitMachine) {
            return (
              <span key="machine" className="contents">
                <TabLink to={`/bots/${id}/desktop`} on={tab === "desktop"} icon={Monitor} label="Desktop" compact={compact} />
                <TabLink to={`/bots/${id}/console`} on={tab === "console"} icon={SquareTerminal} label="Console" compact={compact} />
              </span>
            );
          }
          return <MachineNav key="desktop" id={id} tab={tab} />;
        }
        const { label, icon } = tabMeta[t];
        return (
          <TabLink
            key={t}
            to={t === "run" && chatId ? `/bots/${id}/run/${chatId}` : `/bots/${id}/${t}`}
            on={t === "run" ? onChatSide(tab) : tab === t}
            icon={icon}
            label={label}
            compact={compact}
          />
        );
      })}
    </>
  );
}

function MachineNav({ id, tab }: { id: string; tab: Tab }) {
  const wrap = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [box, setBox] = useState({ top: 0, left: 0 });
  const onMachine = tab === "desktop" || tab === "console";
  const onConsole = tab === "console";
  const label = onConsole ? "Console" : "Desktop";
  const Icon = onConsole ? SquareTerminal : Monitor;
  const href = onConsole ? `/bots/${id}/console` : `/bots/${id}/desktop`;
  const other = onConsole ? `/bots/${id}/desktop` : `/bots/${id}/console`;
  const otherLabel = onConsole ? "Desktop" : "Console";
  const OtherIcon = onConsole ? Monitor : SquareTerminal;
  useEffect(() => {
    if (!open) return;
    const r = wrap.current?.getBoundingClientRect();
    if (r) setBox({ top: r.bottom + 6, left: r.left });
    const close = () => setOpen(false);
    const onDoc = (e: MouseEvent) => {
      const t = e.target;
      if (!(t instanceof Node)) return;
      if (wrap.current?.contains(t) || menu.current?.contains(t)) return;
      close();
    };
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    document.addEventListener("mousedown", onDoc);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      document.removeEventListener("mousedown", onDoc);
    };
  }, [open]);
  return (
    <div
      ref={wrap}
      data-tab-on={onMachine || undefined}
      className={`group/machine flex h-8 shrink-0 items-stretch self-center rounded-sm transition-colors duration-[160ms] ease-quiet ${ onMachine ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"
      }`}
    >
      <NavLink to={href} className="flex shrink-0 items-center gap-1.5 whitespace-nowrap pr-1 pl-2.5 text-[13px] font-medium">
        <Icon size={15} />
        {label}
      </NavLink>
      <button
        type="button"
        title={otherLabel}
        aria-expanded={open}
        className="flex items-center rounded-sm pr-2 pl-0.5"
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronDown size={12} className={`transition-transform duration-[200ms] ease-quiet ${open ? "rotate-180" : ""}`} />
      </button>
      {open &&
        createPortal(
          <div
            ref={menu}
            className="rise z-50 w-44 rounded-control bg-surface p-1 shadow-slip"
            style={{ position: "fixed", top: box.top, left: box.left }}
          >
            <Link
              to={other}
              onClick={() => setOpen(false)}
              className="flex h-8 items-center gap-2 rounded-sm px-2.5 text-[13px] font-medium text-ink transition-colors duration-[160ms] hover:bg-well"
            >
              <OtherIcon size={15} />
              {otherLabel}
            </Link>
          </div>,
          document.body,
        )}
    </div>
  );
}
