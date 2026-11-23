import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { Panel, SkeletonRows, inputClass } from "../Field";
import { UI, type SearchSource, type SearchTest } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { reload } from "../query";
import { sourceLook, testLine } from "./sourceHealth";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

const noSources: SearchSource[] = [];

// Admin → Settings → Search, under the form: what the Built-in Scraper's
// sources are doing (the server's own view, since it started) and one real
// search with the saved settings, for any engine.
export function SearchHealth() {
  const { dirty } = useSettingsForm();
  const q = useQuery(UI.method.checkSearch, {}, { refetchInterval: 15000 });
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

  if (q.isPending) return <SkeletonRows rows={2} height={44} />;
  const sources = q.data?.sources ?? noSources;
  const line = test ? testLine(test) : null;

  return (
    <>
      {sources.length > 0 ? (
        <Panel
          title={t("Sources")}
          note={t("The sites the Built-in Scraper asks, as this server has seen them since it started. One that refuses is left alone for a while and the others answer in its place.")}
          padded={false}
        >
          <ul>
            {sources.map((s) => (
              <SourceRow key={s.name} source={s} />
            ))}
          </ul>
        </Panel>
      ) : null}
      <Panel title={t("Try a search")} note={t("One real search with the saved settings. It spends the engine's budget or quota like any other.")}>
        <form onSubmit={(e) => void run(e)} className="flex flex-wrap items-center gap-2">
          <input
            className={`${inputClass} min-w-[180px] flex-1`}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("What to search for")}
            aria-label={t("What to search for")}
          />
          <Btn type="submit" disabled={busy || !query.trim()}>
            {busy ? t("Searching…") : t("Search")}
          </Btn>
        </form>
        {dirty > 0 ? <p className="mt-2 text-meta leading-[18px] text-ink-3">{t("This searches with the saved settings. Save first.")}</p> : null}
        {err ? <p className="mt-3 text-small text-vermilion">{err}</p> : null}
        {test && line ? (
          <div role="status" className="mt-3 grid gap-2">
            <p className={`text-small leading-5 ${line.ok ? "text-ink-2" : "text-vermilion"}`}>{line.text}</p>
            {test.note ? <p className="text-small leading-5 text-ink-2">{test.note}</p> : null}
            {test.blocked.length > 0 ? <p className="text-meta text-ink-3">{t("Did not answer: {{names}}", { names: test.blocked.join(", ") })}</p> : null}
            {test.busy.length > 0 ? <p className="text-meta text-ink-3">{t("Budget spent for now: {{names}}", { names: test.busy.join(", ") })}</p> : null}
            {test.hits.length > 0 ? (
              <ol className="grid gap-1.5">
                {test.hits.map((h) => (
                  <li key={h.url} className="min-w-0">
                    <div className="truncate text-small text-ink">{h.title}</div>
                    <div className="truncate font-mono text-label text-ink-3">{h.url}</div>
                  </li>
                ))}
              </ol>
            ) : null}
          </div>
        ) : null}
      </Panel>
    </>
  );
}

function SourceRow({ source }: { source: SearchSource }) {
  const look = sourceLook(source);
  return (
    <li className="flex items-center gap-3 border-t border-line px-5 py-3 first:border-t-0">
      <Lamp status={look.lamp} className="shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-small text-ink">{source.name}</span>
          <span className="text-label text-ink-3">{source.api ? t("API") : t("Scraped")}</span>
        </div>
        <div className="truncate text-meta leading-[18px] text-ink-3" title={look.detail}>
          {look.detail}
        </div>
      </div>
      <span className={`shrink-0 text-meta ${look.tone}`}>{look.word}</span>
    </li>
  );
}
