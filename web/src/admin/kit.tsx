import { Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

// The parts every admin page is made of, so they all read alike: a head (the
// page's name and one sentence), then groups. A group is its title and note
// over one card of rows (SettingRow, or a row of the page's own); nothing an
// admin page shows sits outside a group.

/** AdminHead opens an admin page: its name, one sentence, and at the end a figure or the page's action. */
export function AdminHead({ title, note, aside, children }: { title: string; note?: ReactNode; aside?: ReactNode; children?: ReactNode }) {
  return (
    <header className="mb-7">
      <div className="flex items-start gap-6">
        <div className="min-w-0 flex-1">
          <h2 className="text-title text-ink">{title}</h2>
          {note ? <p className="mt-1.5 max-w-[68ch] text-row leading-5 text-ink-2">{note}</p> : null}
        </div>
        {aside ? <div className="shrink-0">{aside}</div> : null}
      </div>
      {children}
    </header>
  );
}

/** Figure is a number in a page's head: what it counts above, the count under it. */
export function Figure({ label, warn, children }: { label: string; warn?: boolean; children: ReactNode }) {
  return (
    <div className="flex flex-col items-end gap-1 max-sm:hidden">
      <span className="text-section text-ink-3">{label}</span>
      <span className={`font-mono text-heading leading-6 tabular-nums ${warn ? "text-vermilion" : "text-ink"}`}>{children}</span>
    </div>
  );
}

/** Groups stacks a page's groups at one distance. */
export function Groups({ children }: { children: ReactNode }) {
  return <div className="grid gap-8">{children}</div>;
}

/**
 * Group is one block of an admin page: a title and note over a card whose
 * children are rows, a hairline between each. `action` sits at the end of
 * the title line (a check, an add).
 */
export function Group({
  title,
  note,
  action,
  tone = "default",
  id,
  children,
}: {
  title?: string;
  note?: ReactNode;
  action?: ReactNode;
  tone?: "default" | "danger";
  id?: string;
  children: ReactNode;
}) {
  return (
    <section id={id} data-tone={tone} className="grid min-w-0 scroll-mt-6 gap-3">
      {title ? (
        <header className="flex items-end justify-between gap-4">
          <div className="min-w-0">
            <h3 className={`text-card-title leading-5 ${tone === "danger" ? "text-vermilion" : "text-ink"}`}>{title}</h3>
            {note ? <p className="mt-1 max-w-[72ch] text-meta leading-[18px] text-ink-2">{note}</p> : null}
          </div>
          {action ? <div className="flex shrink-0 items-center gap-2">{action}</div> : null}
        </header>
      ) : null}
      <div className="min-w-0 divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">{children}</div>
    </section>
  );
}

/** Block is a row of a group that is not a setting: free content at the rows' own padding. */
export function Block({ className = "", children }: { className?: string; children: ReactNode }) {
  return <div className={`px-5 py-4 ${className}`}>{children}</div>;
}

/** Band is a group's foot on `well`: the add row of a list, a key. */
export function Band({ className = "", children }: { className?: string; children: ReactNode }) {
  return <div className={`bg-well px-5 py-3.5 ${className}`}>{children}</div>;
}

/** Facts is a row of read-only values, each under its label. */
export function Facts({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-2 gap-x-6 gap-y-4 px-5 py-4 @2xl/page:grid-cols-3">{children}</dl>;
}

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-label leading-4 text-ink-3">{label}</dt>
      <dd className="mt-1 min-w-0 text-small leading-5 break-words text-ink">{children}</dd>
    </div>
  );
}

/** Line is one read-only fact as a row: the label in a column, the value beside it. */
export function Line({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-x-6 gap-y-0.5 px-5 py-3 text-small leading-5 @2xl/page:grid-cols-[180px_minmax(0,1fr)]">
      <span className="text-ink-3">{label}</span>
      <span className="min-w-0 break-words text-ink">{children}</span>
    </div>
  );
}

/** Notice is a sentence the reader should not miss, in a group: a warning (vermilion glyph) or a note. */
export function Notice({ tone = "note", children }: { tone?: "note" | "warn"; children: ReactNode }) {
  const Icon = tone === "warn" ? TriangleAlert : Info;
  return (
    <div className="flex items-start gap-2.5 bg-well px-5 py-3 text-small leading-5 text-ink-2">
      <Icon size={15} className={`mt-0.5 shrink-0 ${tone === "warn" ? "text-vermilion" : "text-ink-3"}`} />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/** Empty is a group with nothing in it yet: one sentence where its rows would be. */
export function Empty({ children }: { children: ReactNode }) {
  return <p className="px-5 py-4 text-small leading-5 text-ink-2">{children}</p>;
}
