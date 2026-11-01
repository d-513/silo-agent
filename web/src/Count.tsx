// How many there are, beside a title or on a tab: a quiet number, never a pill.
export function Count({ n }: { n: number }) {
  return <span className="font-mono text-label leading-4 font-normal tabular-nums text-ink-3">{n}</span>;
}
