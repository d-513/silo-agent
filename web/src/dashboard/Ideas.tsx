import { useQuery } from "@connectrpc/connect-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowUpRight } from "lucide-react";
import { useContext } from "react";
import { BotPageCtx } from "../bot/context";
import { UI } from "../gen/silo/v1/ui_pb";
import { chatLink } from "../links";
import { msg, t } from "../i18n.ts";
import { items, text } from "./widgets";

export type Idea = { title: string; prompt: string };

// ideasOf reads an ideas widget's data: {items: [{title, prompt?}]}.
export function ideasOf(data: Record<string, unknown>): Idea[] {
  return items(data.items)
    .map((x) => ({ title: text(x.title), prompt: text(x.prompt) || text(x.title) }))
    .filter((x) => x.title);
}

// useOpenIdea puts an idea in the main chat's composer and goes there; it is
// never sent for the owner. A draft the owner already started is kept: the
// idea goes in below it. With the chat list not in yet, the chat page itself
// opens the main chat.
export function useOpenIdea() {
  const page = useContext(BotPageCtx);
  const navigate = useNavigate();
  return (prompt: string) => {
    if (!page) return;
    const main = page.chats.find((c) => c.main)?.id;
    page.draft.setText((cur) => (!cur.trim() ? prompt : cur.includes(prompt) ? cur : `${cur.trimEnd()}\n\n${prompt}`));
    void navigate(chatLink(page.id, main));
  };
}

// IdeaList is a column of ideas, each a press away from the composer.
export function IdeaList({ ideas }: { ideas: Idea[] }) {
  const open = useOpenIdea();
  return (
    <ul className="space-y-1">
      {ideas.map((idea, i) => (
        <li key={i}>
          <button
            type="button"
            onClick={() => open(idea.prompt)}
            className="group/idea flex w-full items-center gap-2 rounded-control px-2.5 py-2 text-left text-[13.5px] leading-5 text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
          >
            <span className="min-w-0 flex-1">{idea.title}</span>
            <ArrowUpRight size={13} className="shrink-0 text-ink-3 opacity-0 transition-opacity duration-[160ms] group-hover/idea:opacity-100" />
          </button>
        </li>
      ))}
    </ul>
  );
}

const general = [
  msg("Research a question for me and compare the top three options"),
  msg("Turn my notes into a one-page PDF I can share"),
  msg("Every morning, brief me on what matters today"),
  msg("Watch a web page and tell me when it changes"),
];

// StarterIdeas are what a Bot can do for its owner before it knows them:
// a few things to ask, shaped by what is connected. No model writes them; the
// Bot replaces them with its own (an ideas widget) once it has some.
export function StarterIdeas({ botId, name }: { botId: string; name: string }) {
  const attached = useQuery(UI.method.listBotConnectors, { botId }).data?.connectors ?? [];
  const has = (...keys: string[]) => attached.some((r) => keys.includes(r.connector?.builtin ?? "") || keys.includes(r.connector?.identifier ?? ""));
  const ideas: Idea[] = [];
  const say = (text: string) => ideas.push({ title: text, prompt: text });
  if (has("email", "gmail")) say(t("Go through my unread mail and tell me what needs an answer"));
  if (has("calendar", "google_calendar")) say(t("Plan my week from my calendar"));
  for (const g of general) say(t(g));
  return (
    <section className="rounded-card bg-surface px-3 py-3 shadow-card">
      <h3 className="mb-1 px-2.5 text-label-caps leading-4 text-ink-3 uppercase">{t("Things to ask {{name}}", { name })}</h3>
      <IdeaList ideas={ideas.slice(0, 5)} />
    </section>
  );
}
