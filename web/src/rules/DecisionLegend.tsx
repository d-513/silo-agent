const entries = [
  { dot: "bg-lamp", title: "Allow", text: "Runs immediately without asking" },
  { dot: "bg-ink-2", title: "Auto", text: "The approval model decides from your policy" },
  { dot: "bg-line", title: "Ask", text: "Pauses run and opens an approval slip" },
  { dot: "bg-vermilion", title: "Deny", text: "Refuses execution automatically" },
];

// What each of the four decisions does, above the rules.
export function DecisionLegend() {
  return (
    <div className="mb-5 rounded-card bg-well p-3.5">
      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-4">
        {entries.map((e) => (
          <div key={e.title} className="flex items-start gap-2.5">
            <span className={`mt-1 flex h-2 w-2 shrink-0 rounded-full ${e.dot}`} />
            <div>
              <div className="text-[13px] font-medium text-ink">{e.title}</div>
              <div className="text-[12px] leading-snug text-ink-3">{e.text}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
