import { BookOpen, ChevronDown } from "lucide-react";
import Markdown from "react-markdown";
import { useSyncExternalStore } from "react";
import remarkGfm from "remark-gfm";

// Tailwind's `lg`. Media queries read rem against the browser's own 16px, not
// the app's 14px root, so this is 1024px like the `lg:` classes beside it.
const lg = "(min-width: 64rem)";
const subscribe = (cb: () => void) => {
  const m = window.matchMedia(lg);
  m.addEventListener("change", cb);
  return () => m.removeEventListener("change", cb);
};
const useWide = () => useSyncExternalStore(subscribe, () => window.matchMedia(lg).matches, () => true);

// An adapter's setup guide. Wide: a panel beside the form that stays in view
// while you fill it in and scrolls on its own. Narrow: a fold above the form,
// shut, so it does not push the form down the page.
export function GuideCard({ guide, title = "Setup guide" }: { guide: string; title?: string }) {
  const wide = useWide();
  const body = (
    <div className="silo-md text-[13px] leading-relaxed text-ink">
      <Markdown remarkPlugins={[remarkGfm]}>{guide}</Markdown>
    </div>
  );
  if (!wide) {
    return (
      <details className="group rounded-card bg-well">
        <summary className="flex cursor-pointer select-none items-center gap-2 px-4 py-3 text-[13px] font-medium text-ink">
          <BookOpen size={15} className="text-ink-2" />
          {title}
          <ChevronDown size={15} className="ml-auto text-ink-3 transition-transform duration-[200ms] ease-quiet group-open:rotate-180" />
        </summary>
        <div className="border-t border-line px-4 pb-4 pt-3">{body}</div>
      </details>
    );
  }
  return (
    <aside className="sticky top-6 flex max-h-[calc(100dvh-9rem)] flex-col overflow-hidden rounded-card bg-well">
      <header className="flex shrink-0 items-center gap-2 px-5 pb-3 pt-4 text-label-caps uppercase text-ink-3">
        <BookOpen size={14} />
        {title}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">{body}</div>
    </aside>
  );
}
