import { Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ConnectorMark } from "../connectors/form";
import { SaveButton, useSave } from "../Feedback";
import { Btn } from "../Btn";
import { ConfigSource, UI, type AutoenableConnectors } from "../gen/silo/v1/ui_pb";
import { Select } from "../Select";
import { t } from "../i18n.ts";
import { Trans } from "../Trans.tsx";
import { Band, Empty, Group, Notice } from "./kit";

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
    <Group
      title={t("On every new bot")}
      note={t("Library connectors every new Bot starts with, on top of the ones switched on in the Connectors Library. Removing one from a Bot does not re-add it.")}
      action={
        locked ? null : (
          <SaveButton size="sm" state={saver.state} disabled={edit === null && saver.state !== "saved"} onClick={save}>
            {t("Save list")}
          </SaveButton>
        )
      }
    >
      {locked ? (
        <Notice tone="warn">
          <Trans i18nKey="Set by <mono>{{envName}}</mono>; change it there." values={{ envName: list?.envName }} components={{ mono: <span className="font-mono" /> }} />
        </Notice>
      ) : null}
      {ids.length === 0 ? <Empty>{t("No connectors listed.")}</Empty> : null}
      {ids.map((id) => {
        const c = byIdent.get(id);
        return (
          <div key={id} className="flex items-center gap-3 px-5 py-2.5">
            <ConnectorMark id={c?.id} hasImage={!!c?.hasImage} size={28} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-row leading-5 font-medium text-ink">{c ? c.name : <span className="font-mono text-small font-normal">{id}</span>}</div>
              {c ? (
                <div className="truncate font-mono text-label leading-4 text-ink-3">{id}</div>
              ) : lib.isPending ? null : (
                <div className="text-label leading-4 text-vermilion">{t("Not in the library, so nothing is added.")}</div>
              )}
            </div>
            {locked ? null : (
              <Btn kind="ghost" size="sm" iconOnly title={t("Remove")} aria-label={t("Remove")} className="hover:text-vermilion" onClick={() => setEdit(ids.filter((x) => x !== id))}>
                <Trash2 size={14} />
              </Btn>
            )}
          </div>
        );
      })}
      {locked ? null : (
        <Band>
          <Select
            value=""
            onChange={(id) => setEdit([...ids, id])}
            options={addable.map((c) => ({ value: c.identifier, label: c.name, hint: c.identifier }))}
            placeholder={t("Add a connector…")}
            emptyLabel={t("Every library connector is listed")}
            ariaLabel={t("Add a connector to every new bot")}
            className="max-w-[300px]"
          />
        </Band>
      )}
    </Group>
  );
}
