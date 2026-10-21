import { t } from "./i18n.ts";

// sourceLabel names where the Bot made something (a Feed post, a question, a
// live run) in one translated phrase: "Automation “Morning brief”", the chat's
// title, "Main chat". An unknown kind has no name ("").
export function sourceLabel(kind: string, name: string, untitled: string = t("Chat")): string {
  switch (kind) {
    case "main":
      return t("Main chat");
    case "automation":
      return t("Automation “{{name}}”", { name });
    case "channel":
      return t("Channel “{{name}}”", { name });
    case "chat":
      return name || untitled;
    default:
      return "";
  }
}
