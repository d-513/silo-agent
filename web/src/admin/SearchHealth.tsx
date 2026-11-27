import { useQuery } from "@connectrpc/connect-query";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { inputClass } from "../Field";
import { UI, type SearchSource, type SearchTest } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { reload } from "../query";
import { Band, Group } from "./kit";
import { sourceLook, testLine } from "./sourceHealth";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

const noSources: SearchSource[] = [];

// What the server searches with now: its saved engine, and that engine's
// sources where it has any (only the Built-in Scraper does). Both parts of
// the page read the one answer.
function useSearchHealth() {
  const { fields, values, engines } = useSettingsForm();
  const q = useQuery(UI.method.checkSearch, {}, { refetchInterval: 15000 });
  const mine = fields.filter((f) => f.key.startsWith("search."));
  // The server's view follows the saved settings: ask again once they change.
  const saved = mine.map((f) => `${f.key}=${f.value}`).join("\n");
  useEffect(() => {
    void reload(UI.method.checkSearch);
  }, [saved]);
  const savedEngine = q.data?.engine ?? "";
  const picked = values["search.engine"] || engines[0]?.id || "";
  return {
    pending: q.isPending,
    sources: q.data?.sources ?? noSources,
    // The engine a test search asks, by its name.
    engine: engines.find((e) => e.id === savedEngine)?.name ?? savedEngine,
    // The engine picked on the page is not the one saved yet.
    switching: !!savedEngine && picked !== savedEngine,
    // A search setting was changed and not saved: a test would not use it.
    unsaved: mine.some((f) => (values[f.key] ?? "") !== f.value),
  };
}

// SearchSources is what the Built-in Scraper's sources are doing, as the
// server has seen them since it started. It is there only while that engine
// is both saved and picked: another engine has no sources, and one only
// picked has not asked anything yet.
export function SearchSources() {
  const { pending, sources, switching } = useSearchHealth();
  if (pending || switching || sources.length === 0) return null;
  return (
    <Group title={t("Sources")} note={t("The sites the Built-in Scraper asks, as this server has seen them since it started. One that refuses is left alone for a while and the others answer in its place.")}>
      {sources.map((s) => (
        <SourceRow key={s.name} source={s} />
      ))}
    </Group>
  );
}

// SearchTry is the foot of the Engine group: one real search with the saved
// settings, whichever engine they name, and what came back.
export function SearchTry() {
  const { engine, unsaved } = useSearchHealth();
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [test, setTest] = useState<SearchTest | null>(null);

  async function run(e: React.FormEvent) {
    e.preventDefault();
    if (!query.trim() || busy) return;
    setBusy(true);
    setErr("");
    setTest(null);
    try {
      const r = await ui.checkSearch({ query });
      setTest(r.test ?? null);
      void reload(UI.method.checkSearch);
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }

  const line = test ? testLine(test) : null;
  // On the group's well foot, not a row among the settings: it changes
  // nothing, it only asks.
  return (
    <Band className="grid gap-2">
      <form onSubmit={(e) => void run(e)} className="flex items-center gap-2">
        <label className="relative block min-w-0 flex-1">
          <Search size={14} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-3" />
          <input
            className={`${inputClass} w-full pl-8`}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={engine ? t("Try a search with {{engine}}", { engine }) : t("Try a search")}
            aria-label={t("Try a search")}
          />
        </label>
        <Btn type="submit" disabled={busy || !query.trim()}>
          {busy ? t("Searching…") : t("Search")}
        </Btn>
      </form>
      <p className="text-meta leading-[18px] text-ink-3">{unsaved ? t("This searches with the saved settings. Save first.") : t("One real search. It spends the engine's budget or quota like any other.")}</p>
      {err ? <p className="text-small leading-5 text-vermilion">{err}</p> : null}
      {test && line ? (
        <div role="status" className="mt-1 grid gap-2">
          <p className={`text-small leading-5 ${line.ok ? "text-ink-2" : "text-vermilion"}`}>{line.text}</p>
          {test.note ? <p className="text-small leading-5 text-ink-2">{test.note}</p> : null}
          {test.blocked.length > 0 ? <p className="text-meta text-ink-3">{t("Did not answer: {{names}}", { names: test.blocked.join(", ") })}</p> : null}
          {test.busy.length > 0 ? <p className="text-meta text-ink-3">{t("Budget spent for now: {{names}}", { names: test.busy.join(", ") })}</p> : null}
          {test.hits.length > 0 ? (
            <ol className="grid gap-2">
              {test.hits.map((h) => (
                <li key={h.url} className="min-w-0">
                  <div className="truncate text-small leading-5 text-ink">{h.title}</div>
                  <div className="truncate font-mono text-label leading-4 text-ink-3">{h.url}</div>
                </li>
              ))}
            </ol>
          ) : null}
        </div>
      ) : null}
    </Band>
  );
}

function SourceRow({ source }: { source: SearchSource }) {
  const look = sourceLook(source);
  return (
    <div className="flex items-center gap-3 px-5 py-3">
      <Lamp status={look.lamp} className="shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-small leading-5 text-ink">{source.name}</span>
          <span className="text-label text-ink-3">{source.api ? t("API") : t("Scraped")}</span>
        </div>
        <div className="truncate text-meta leading-[18px] text-ink-3" title={look.detail}>
          {look.detail}
        </div>
      </div>
      <span className={`shrink-0 text-meta ${look.tone}`}>{look.word}</span>
    </div>
  );
}
