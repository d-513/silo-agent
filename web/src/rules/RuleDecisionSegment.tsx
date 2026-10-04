import { Bot, Check, CircleHelp, X } from "lucide-react";

const modes = [
  { id: "allow", label: "Allow", icon: Check },
  { id: "auto", label: "Auto", icon: Bot },
  { id: "ask", label: "Ask", icon: CircleHelp },
  { id: "deny", label: "Deny", icon: X },
];

// The Allow / Auto / Ask / Deny switch for one rule, or for a whole section.
export function RuleDecisionSegment({
  value,
  onChange,
  disabled,
  size = "md",
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  size?: "sm" | "md";
}) {
  return (
    <div role="radiogroup" className="flex select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
      {modes.map((m) => {
        const active = value === m.id;
        const Icon = m.icon;
        // Selected segment is a surface chip; only the text carries the tone.
        const tone = m.id === "allow" ? "text-emerald" : m.id === "deny" ? "text-vermilion" : m.id === "auto" ? "text-ink-2" : "text-ink";
        return (
          <button
            key={m.id}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={`flex flex-1 items-center justify-center gap-1 rounded-sm font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
              size === "sm" ? "h-7 px-1.5 text-[11.5px]" : "h-8 px-2 text-[12.5px]"
            } ${active ? `bg-surface shadow-card ${tone}` : "text-ink-3 hover:bg-pressed hover:text-ink"} ${
              disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"
            }`}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChange(m.id);
            }}
          >
            <Icon size={size === "sm" ? 11 : 12} />
            <span>{m.label}</span>
          </button>
        );
      })}
    </div>
  );
}
