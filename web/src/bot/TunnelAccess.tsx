import { Globe, Lock } from "lucide-react";
import { ChipRadio } from "../ChipRadio";
import { useArmed } from "../Feedback";
import { accessOf } from "../tunnels";
import { t } from "../i18n.ts";

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

  return (
    <ChipRadio
      label={t("Who can open this tunnel")}
      disabled={disabled}
      value={isPublic}
      onChange={choose}
      chip="w-[104px] gap-1.5 px-2.5"
      options={options.map((o) => {
        const arming = o.pub && armed && o.pub !== isPublic;
        const Icon = o.icon;
        return {
          value: o.pub,
          icon: <Icon size={12} />,
          label: <span>{arming ? t("Click again") : accessOf(o.pub).label}</span>,
          tone: o.tone,
          title: arming ? t("Click again to make it public") : accessOf(o.pub).caption,
          look: arming ? "bg-vermilion text-on-accent" : undefined,
        };
      })}
    />
  );
}
