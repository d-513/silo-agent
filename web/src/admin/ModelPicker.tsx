import { Check, CornerDownRight, Search } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell, inputClass } from "../Field";
import { FilterChip } from "../FilterChip";
import { locale, t } from "../i18n.ts";
import { Modal } from "../Modal";
import { LiveChip } from "./LiveChip";
import { customChoice, matches, type Choice } from "./modelChoices";
import { ProviderMark } from "./ProviderMap";
import { effective, jobName, kindsOf, providerOf, type JobDef } from "./providerJobs";
import { fmtContext, kindOf, kindWord } from "./providerModels";
import { useProviderLists } from "./useProviderLists";
import { useSettingsForm } from "./useAdminSettings";

// What the list draws: a group's name, or a row the arrow keys can stop on.
type Item = { head: string } | { row: Choice | "unset"; at: number };

// ModelPicker picks the model for one job, in one place: the models already
// allowed, then everything the providers that are set up can serve, then any
// id typed by hand. A press on a row is the choice. A text or decision model
// that is not allowed yet is allowed by the same press, so there is no list
// to fill in first; the choice itself is saved with the form.
export function ModelPicker({ job, onClose }: { job: JobDef; onClose: () => void }) {
  const form = useSettingsForm();
  const { values, setValue, setModel, allowedOf, providers } = form;
  const kinds = kindsOf(job.takes);
  const lists = useProviderLists(kinds);
  const [query, setQuery] = useState("");
  // "" is every provider.
  const [only, setOnly] = useState("");
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const box = useRef<HTMLDivElement>(null);

  const own = (values[job.key] ?? "").trim();
  const unsetValue = job.unsetValue ?? "";
  const isUnset = own === "" || own.toLowerCase() === unsetValue;
  const follows = effective(values, job.key);
  const nameOf = (id: string) => providers.find((p) => p.id === id)?.name ?? id;

  // What is allowed already (with what its provider says of it), then what
  // the providers list beyond that.
  const told = new Map(lists.rows.map((m) => [m.id, m]));
  const allowed: Choice[] = kinds.flatMap((k) =>
    allowedOf(k).map((m) => {
      const p = told.get(m.id);
      return { id: m.id, name: m.label && m.label !== m.id ? m.label : (p?.name ?? ""), kind: k, provider: providerOf(m.id), live: m.live, words: m.words, endOfTurn: m.endOfTurn, context: p?.contextWindow ?? 0, voices: p?.voices.length ?? 0, allowed: true };
    }),
  );
  const have = new Set(allowed.map((m) => m.id));
  const listed: Choice[] = lists.rows
    .filter((m) => !have.has(m.id))
    .map((m) => ({
      id: m.id,
      name: m.fastOf ? t("Fast mode: faster output at the provider's premium price") : m.name,
      kind: kindOf(m),
      provider: m.provider.id,
      live: m.live,
      words: m.words,
      endOfTurn: m.endOfTurn,
      context: m.contextWindow,
      voices: m.voices.length,
      allowed: false,
    }));

  const typed = query.trim();
  const show = (rows: Choice[]) => rows.filter((m) => (!only || m.provider === only) && matches(m, query));
  const custom = customChoice(query, kinds, providers, [...allowed, ...listed]);
  const items: Item[] = [];
  let count = 0;
  const add = (head: string, rows: (Choice | "unset")[]) => {
    if (rows.length === 0) return;
    if (head) items.push({ head });
    for (const row of rows) items.push({ row, at: count++ });
  };
  // The empty choice heads the list until something is typed.
  add("", typed ? [] : ["unset"]);
  add(t("Allowed already"), show(allowed));
  add(t("From your providers"), show(listed));
  add(t("Not on a provider's list"), custom ? [custom] : []);
  const at = Math.min(active, Math.max(0, count - 1));

  useEffect(() => setActive(0), [query, only]);
  useEffect(() => {
    box.current?.querySelector(`[data-row="${at}"]`)?.scrollIntoView({ block: "nearest" });
  }, [at]);

  async function choose(row: Choice | "unset") {
    setErr("");
    if (row === "unset") {
      setValue(job.key, unsetValue);
      onClose();
      return;
    }
    // A chat's model and a decision model must be on their list before a job
    // may name them: the press that picks one allows it.
    if ((row.kind === "text" || row.kind === "decision") && !row.allowed) {
      setBusy(row.id);
      const ok = await setModel(row.id, true, row.kind);
      setBusy("");
      if (!ok) {
        setErr(t("{{id}} could not be allowed, so it was not picked.", { id: row.id }));
        return;
      }
    }
    setValue(job.key, row.id);
    onClose();
  }

  function onKey(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(at + 1, count - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(at - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const hit = items.find((x) => "row" in x && x.at === at);
      if (hit && "row" in hit && !busy) void choose(hit.row);
    }
  }

  const filterable = [...new Set([...allowed, ...listed].map((m) => m.provider))].filter(Boolean);
  const nothing = lists.asked.length === 0 && allowed.length === 0;

  return (
    <Modal onClose={onClose} size="wide" rise dismissOnBackdrop label={t(job.label)}>
      <div className="mb-4 pr-8">
        <h2 className="text-heading text-ink">{t(job.label)}</h2>
        <p className="mt-1 max-w-[62ch] text-meta leading-[18px] text-ink-2">{t(job.hint)}</p>
      </div>
      <label className="relative mb-3 block">
        <Search size={14} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-3" />
        <input
          autoFocus
          className={`${inputClass} w-full pl-8`}
          placeholder={t("Search models, or type provider/model")}
          aria-label={t("Search models, or type provider/model")}
          autoComplete="off"
          spellCheck={false}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKey}
        />
      </label>
      {filterable.length > 1 ? (
        <div className="mb-3 flex flex-wrap gap-1.5">
          <FilterChip on={only === ""} onClick={() => setOnly("")}>
            {t("All providers")}
          </FilterChip>
          {filterable.map((p) => (
            <FilterChip key={p} on={only === p} onClick={() => setOnly(only === p ? "" : p)}>
              {nameOf(p)}
            </FilterChip>
          ))}
        </div>
      ) : null}
      {err ? <ErrorWell className="mb-3">{form.err || err}</ErrorWell> : null}
      {lists.failed.map((f) => (
        <ErrorWell key={f.provider.id} className="mb-3">
          {f.provider.name}: {fail(f.error)}
        </ErrorWell>
      ))}

      <div ref={box} role="listbox" aria-label={t(job.label)} className="max-h-[min(54vh,520px)] divide-y divide-line overflow-y-auto overscroll-contain rounded-control bg-surface shadow-card">
        {items.map((x) => {
          if ("head" in x) {
            return (
              <div key={x.head} className="sticky top-0 z-[1] bg-well px-3 py-1.5 text-section text-ink-3">
                {x.head}
              </div>
            );
          }
          const lit = x.at === at;
          const hover = () => setActive(x.at);
          if (x.row === "unset") {
            return (
              <Row key="unset" at={x.at} lit={lit} current={isUnset} busy={!!busy} onHover={hover} onPick={() => void choose("unset")}>
                <span className="flex h-6 w-6 shrink-0 items-center justify-center text-ink-3">
                  <CornerDownRight size={14} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate leading-5 text-ink">{t(job.unset)}</span>
                  {follows.from && follows.model ? (
                    <span className="block truncate text-label leading-4 text-ink-3">
                      <span className="font-mono">{follows.model}</span>
                      {" · "}
                      {t("from {{job}}", { job: t(jobName(follows.from)) })}
                    </span>
                  ) : null}
                </span>
              </Row>
            );
          }
          const m = x.row;
          const context = m.kind === "speech" ? (m.voices ? t("{{count}} voices", { count: m.voices }) : "") : fmtContext(m.context);
          return (
            <Row key={m.id} at={x.at} lit={lit} current={!isUnset && m.id === own} busy={!!busy} spinning={busy === m.id} onHover={hover} onPick={() => void choose(m)}>
              <ProviderMark id={m.provider} name={nameOf(m.provider)} small />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-mono leading-5 text-ink">{m.id}</span>
                {m.name ? <span className="block truncate text-label leading-4 text-ink-3">{m.name}</span> : null}
              </span>
              {job.takes === "either" ? <span className="shrink-0 text-label text-ink-3">{kindWord(m.kind)}</span> : null}
              <LiveChip kind={m.kind} live={m.live} words={m.words} endOfTurn={m.endOfTurn} />
              {context ? (
                <span className="shrink-0 font-mono text-label text-ink-3 tabular-nums max-sm:hidden" title={m.kind === "speech" ? undefined : t("{{tokens}} tokens of context", { tokens: m.context.toLocaleString(locale()) })}>
                  {context}
                </span>
              ) : null}
            </Row>
          );
        })}
        {lists.pending ? (
          <div className="flex h-11 items-center gap-2 pl-3 text-meta text-ink-3">
            <Spinner size={13} /> {t("Still asking {{count}} providers", { count: lists.pending })}
          </div>
        ) : null}
        {!lists.pending && count === 0 ? <p className="px-3 py-3 text-small leading-5 text-ink-2">{t("No model matches “{{search}}”. Type its full id, provider/model, to use it anyway.", { search: typed })}</p> : null}
      </div>

      <p className="mt-3 text-meta leading-[18px] text-ink-3">
        {nothing ? (
          <>
            {t("No provider that serves these models is set up yet.")}{" "}
            <Link to="/admin/settings/models/$page" params={{ page: "providers" }} className="text-cobalt hover:text-cobalt-deep" onClick={onClose}>
              {t("Set up a provider")}
            </Link>
          </>
        ) : kinds.includes("text") || kinds.includes("decision") ? (
          t("A model that is not allowed yet joins the allowed list when you pick it. The choice is saved with the page.")
        ) : (
          t("The choice is saved with the page.")
        )}
      </p>
    </Modal>
  );
}

// Row is one choice: the row under the pointer or the arrow keys is toned,
// the current choice is marked the way "you are here" is everywhere.
function Row({ at, lit, current, busy, spinning, onHover, onPick, children }: { at: number; lit: boolean; current: boolean; busy: boolean; spinning?: boolean; onHover: () => void; onPick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={current}
      data-row={at}
      disabled={busy}
      onMouseMove={onHover}
      onClick={onPick}
      className={`flex min-h-11 w-full items-center gap-3 px-3 py-1.5 text-left text-small outline-none ${current ? "bg-cobalt-pale" : lit ? "bg-well" : ""}`}
    >
      {children}
      <span className="flex w-5 shrink-0 justify-center">{spinning ? <Spinner size={13} /> : current ? <Check size={15} className="text-cobalt" /> : null}</span>
    </button>
  );
}
