import { Globe, Lock } from "lucide-react";
import type { KeyboardEvent } from "react";
import { useArmed } from "../Feedback";
import { accessOf } from "../tunnels";

const options = [
  { pub: false, icon: Lock, tone: "text-emerald" },
  { pub: true, icon: Globe, tone: "text-vermilion" },
] as const;

// TunnelAccess is the Private | Public choice for one tunnel: both options are
// always visible and the current one is the raised chip (the same control as a
// rule's Allow / Ask / Deny). With `confirm`, going Public takes a second click,
// because that exposes the service to anyone with the link; going Private never
// does.
export function TunnelAccess({
  isPublic,
  onChange,
  confirm = false,
  disabled,
}: {
  isPublic: boolean;
  onChange: (isPublic: boolean) => void;
  confirm?: boolean;
  disabled?: boolean;
}) {
  const { armed, fire, disarm } = useArmed(3000);

  const choose = (pub: boolean) => {
    if (pub === isPublic) return;
    if (pub && confirm) {
      fire(() => onChange(true));
      return;
    }
    disarm();
    onChange(pub);
  };

  // Arrow keys move between the two, like any radio group.
  const keys = (e: KeyboardEvent) => {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    e.preventDefault();
    choose(!isPublic);
  };

  return (
    <div role="radiogroup" aria-label="Who can open this tunnel" onKeyDown={keys} className="flex w-fit select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
      {options.map((o) => {
        const active = o.pub === isPublic;
        const arming = o.pub && armed && !active;
        const Icon = o.icon;
        return (
          <button
            key={String(o.pub)}
            type="button"
            role="radio"
            aria-checked={active}
            tabIndex={active ? 0 : -1}
            disabled={disabled}
            title={arming ? "Click again to make it public" : accessOf(o.pub).caption}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              choose(o.pub);
            }}
            className={`flex h-8 w-[104px] items-center justify-center gap-1.5 rounded-sm px-2.5 text-[12.5px] font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
              active ? `bg-surface shadow-card ${o.tone}` : arming ? "bg-vermilion text-on-accent" : "text-ink-3 hover:bg-pressed hover:text-ink"
            } ${disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}
          >
            <Icon size={12} />
            <span>{arming ? "Click again" : accessOf(o.pub).label}</span>
          </button>
        );
      })}
    </div>
  );
}
