import { CalendarDays, GitCompare, Globe, Mail } from "lucide-react";
import { t } from "../i18n.ts";

// What an empty board offers to put in the ask bar: one press fills it in.
export function suggestions() {
  return [
    { icon: Mail, label: t("Mail that needs a reply") },
    { icon: CalendarDays, label: t("Today's calendar") },
    { icon: GitCompare, label: t("What changed in /workspace") },
    { icon: Globe, label: t("Is my site up") },
  ];
}
