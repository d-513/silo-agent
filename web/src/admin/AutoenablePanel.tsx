import { Trash2, TriangleAlert } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ConnectorMark } from "../connectors/form";
import { SaveButton, useSave } from "../Feedback";
import { Panel } from "../Field";
import { ConfigSource, UI, type AutoenableConnectors } from "../gen/silo/v1/ui_pb";
import { Select } from "../Select";

const none: string[] = [];

// The autoenable_connectors list: library connectors, by identifier, that every
// new Bot gets. It is the file's way of saying what a connector's own "Add to
// new bots by default" switch says, so both end in the same copy on the Bot.
export function AutoenablePanel({ list, onSave }: { list?: AutoenableConnectors; onSave: (ids: string[]) => Promise<void> }) {
  const lib = useQuery(UI.method.listConnectors, {});
  const presets = lib.data?.connectors ?? [];
  // null until the human changes the list: it then shows what the server holds.
  const [edit, setEdit] = useState<string[] | null>(null);
  const saver = useSave();
  const ids = edit ?? list?.identifiers ?? none;
  const locked = list?.source === ConfigSource.ENV;
  const byIdent = new Map(presets.map((c) => [c.identifier, c]));
  const addable = presets.filter((c) => c.identifier && !ids.includes(c.identifier)).sort((a, b) => a.name.localeCompare(b.name));

  function save() {
    void saver
      .run(() => onSave(ids))
      .then(() => setEdit(null))
      .catch(() => {});
  }

  return (
    <Panel
      title="On every new bot"
      note="Library connectors every new Bot starts with, on top of the ones switched on in the Connectors Library. Removing one from a Bot does not re-add it."
    >
      {locked ? (
        <p className="mb-4 flex items-start gap-2 text-[13px] text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={16} />
          <span>
            Set by <span className="font-mono">{list?.envName}</span>; change it there.
          </span>
        </p>
      ) : null}
      {ids.length === 0 ? (
        <p className="mb-3 text-[13px] text-ink-2">No connectors listed.</p>
      ) : (
        <ul className="mb-3 grid gap-2">
          {ids.map((id) => {
            const c = byIdent.get(id);
            return (
              <li key={id} className="flex items-center gap-3">
                <ConnectorMark id={c?.id} hasImage={!!c?.hasImage} size={28} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[14px] text-ink">{c ? c.name : <span className="font-mono text-[13px]">{id}</span>}</div>
                  {c ? (
                    <div className="truncate font-mono text-[12px] text-ink-3">{id}</div>
                  ) : lib.isPending ? null : (
                    <div className="text-[12px] text-vermilion">Not in the library, so nothing is added.</div>
                  )}
                </div>
                {locked ? null : (
                  <button
                    type="button"
                    title="Remove"
                    className="rounded-sm p-1 text-ink-2 transition-colors hover:bg-pressed hover:text-vermilion"
                    onClick={() => setEdit(ids.filter((x) => x !== id))}
                  >
                    <Trash2 size={14} />
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {locked ? null : (
        <div className="flex flex-wrap items-center gap-3">
          <Select
            value=""
            onChange={(id) => setEdit([...ids, id])}
            options={addable.map((c) => ({ value: c.identifier, label: c.name, hint: c.identifier }))}
            placeholder="Add a connector…"
            emptyLabel="Every library connector is listed"
            ariaLabel="Add a connector to every new bot"
            className="max-w-[280px]"
          />
          <SaveButton state={saver.state} onClick={save}>
            Save list
          </SaveButton>
        </div>
      )}
    </Panel>
  );
}
