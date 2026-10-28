import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { Pencil, Plug, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { ui } from "../api";
import { Btn, btnClass } from "../Btn";
import { useBots } from "../bots";
import { startConnectorAuth } from "../connectorAuth";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { UI, type Connector } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Modal } from "../Modal";
import { widePage } from "../PageHead";
import { reload } from "../query";
import { BotsStep, ChooseBots, useManagedBots } from "./BotPick";
import { ConnectorToolbar } from "./ConnectorToolbar";
import { CustomTab } from "./CustomTab";
import { ConnectorFields, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "./form";
import { LibraryTab } from "./LibraryTab";
import { categoriesOf, FEATURED, filterCatalog, groupByCategory, nextCopyName } from "./model";
import { SpaceCard } from "./SpaceCard";
import { LendChip, SignInChoice } from "../space/Lend";
import { useSpaces } from "../space/spaces";

type HubMode = "space" | "library" | "custom";

const noRows: Connector[] = [];

// SpaceConnectors is the Workspace's Connectors tab in Customize: every
// connection kept once here, which Bots use each, and adding new ones from the
// library or a custom server.
export function SpaceConnectors() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/connectors" });
  const spaceQ = useQuery(UI.method.listSpaceConnectors, { spaceId }, { refetchInterval: 3000 });
  const catalogQ = useQuery(UI.method.listConnectors, {}, { refetchInterval: 10_000 });
  const rows = spaceQ.data?.connectors ?? noRows;
  const catalog = catalogQ.data?.connectors ?? noRows;
  const managed = useManagedBots(spaceId) ?? [];
  const space = useSpaces().spaces?.find((x) => x.id === spaceId);
  const solo = (space?.people ?? 1) <= 1;
  const { bots } = useBots();
  const [actErr, setErr] = useState("");
  const loadErr = (!spaceQ.data && spaceQ.error) || (!catalogQ.data && catalogQ.error);
  const err = actErr || (loadErr ? fail(loadErr) : "");
  const [tab, setTab] = useState<HubMode>("space");
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("all");
  const [libraryModal, setLibraryModal] = useState<Connector | null>(null);
  const [libraryDraft, setLibraryDraft] = useState<ConnectorDraft>(emptyDraft());
  const [customDraft, setCustomDraft] = useState<ConnectorDraft>(emptyDraft());
  // Which Bots a new connection goes to: every Bot the user manages, unless
  // they untick some.
  const [chosen, setChosen] = useState<string[] | null>(null);
  const picked = chosen ?? managed.map((b) => b.id);
  const [edit, setEdit] = useState<Connector | null>(null);
  const [editDraft, setEditDraft] = useState<ConnectorDraft>(emptyDraft());

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return q ? rows.filter((c) => c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q)) : rows;
  }, [rows, search]);
  const filteredCatalog = useMemo(() => filterCatalog(catalog, search, category), [catalog, search, category]);
  const featured = useMemo(() => FEATURED.flatMap((key) => catalog.filter((c) => c.builtin === key)), [catalog]);
  const byCategory = useMemo(() => groupByCategory(filteredCatalog), [filteredCatalog]);
  const nameOf = (id: string) => bots?.find((b) => b.id === id)?.name ?? "";
  const load = () => Promise.all([reload(UI.method.listSpaceConnectors, { spaceId }), reload(UI.method.listBotConnectors)]);

  async function act(fn: () => Promise<unknown>) {
    setErr("");
    try {
      await fn();
      await load();
      return true;
    } catch (ex) {
      setErr(fail(ex));
      return false;
    }
  }

  async function add(e: React.FormEvent, draft: ConnectorDraft, sourceId: string) {
    e.preventDefault();
    const ok = await act(() => ui.createBotConnector({ spaceId, botIds: picked, ...specOf(draft), sourceId }));
    if (!ok) return;
    setLibraryModal(null);
    setCustomDraft(emptyDraft());
    setChosen(null);
    setTab("space");
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    if (!edit) return;
    if (await act(() => ui.updateConnector({ id: edit.id, ...specOf(editDraft) }))) setEdit(null);
  }

  const used = (c: Connector) => c.usedBy.map(nameOf).filter(Boolean);

  return (
    <div className={widePage}>
      <div className="mb-6 space-y-1.5">
        <h1 className="text-title text-ink">{t("Connectors")}</h1>
        <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">
          {t("Connect a service once for the whole workspace, then choose which Bots can use it. Each Bot keeps its own session and its own rules for it.")}
        </p>
      </div>

      {err ? (
        <div className="mb-4 flex items-center justify-between rounded-sm bg-vermilion-pale px-3.5 py-2.5 text-[13px] text-vermilion">
          <span>{err}</span>
          <button type="button" onClick={() => setErr("")} className="text-vermilion hover:opacity-80" aria-label={t("Close")}>
            <X size={14} />
          </button>
        </div>
      ) : null}

      <ConnectorToolbar
        tabs={[
          { mode: "space", label: t("In this workspace"), count: rows.length },
          { mode: "library", label: t("Library"), count: catalog.length },
          { mode: "custom", label: t("Custom"), add: true },
        ]}
        tab={tab}
        setTab={setTab}
        search={search}
        setSearch={setSearch}
        onAdd={() => setTab("library")}
      />

      {tab === "space" ? (
        rows.length === 0 ? (
          <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
              <Plug size={28} />
            </div>
            <h3 className="text-base font-semibold text-ink">{t("No connectors in this workspace yet")}</h3>
            <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">{t("Add one from the library or connect your own MCP server, then choose which Bots can use it.")}</p>
            <div className="mt-6 flex items-center justify-center gap-3">
              <button type="button" className={btnClass("primary")} onClick={() => setTab("library")}>
                {t("Browse Library")}
              </button>
              <button type="button" className={btnClass("secondary")} onClick={() => setTab("custom")}>
                {t("Add Custom Connector")}
              </button>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {shown.map((c) => (
              <SpaceCard
                key={c.id}
                c={c}
                actions={
                  <>
                    {c.auth === "oauth" && !c.signedIn ? (
                      <SignInChoice solo={solo} space={space?.name ?? ""} onSignIn={(shared) => void act(() => startConnectorAuth({ connectorId: c.id, shared }))} />
                    ) : null}
                    <LendChip lentBy={c.lentBy} shared={c.shared} solo={solo} onShare={async (shared) => void (await act(() => ui.setLendShared({ connectorId: c.id, shared })))} />
                    <ChooseBots
                      bots={managed}
                      granted={c.usedBy}
                      onToggle={async (botId, on) => {
                        await act(() => (on ? ui.grantConnector({ botId, connectorId: c.id }) : ui.revokeConnector({ botId, connectorId: c.id })));
                      }}
                    />
                    {c.canEdit ? (
                      <>
                        <Btn
                          kind="ghost"
                          onClick={() => {
                            setEdit(c);
                            setEditDraft(draftFrom(c));
                          }}
                        >
                          <Pencil size={13} />
                          <span>{t("Edit")}</span>
                        </Btn>
                        <ArmedButton
                          kind="ghost"
                          armedLabel={used(c).length ? t("Remove from {{bots}} too", { bots: used(c).join(", ") }) : t("Click again to delete")}
                          icon={<Trash2 size={13} />}
                          onConfirm={() => void act(() => ui.deleteConnector({ id: c.id }))}
                        >
                          {t("Delete")}
                        </ArmedButton>
                      </>
                    ) : null}
                  </>
                }
              />
            ))}
          </div>
        )
      ) : null}

      {tab === "library" ? (
        <LibraryTab
          catalog={catalog}
          filtered={filteredCatalog}
          categories={categoriesOf(catalog)}
          byCategory={byCategory}
          featured={featured}
          selectedCategory={category}
          setSelectedCategory={setCategory}
          search={search}
          setSearch={setSearch}
          countFor={(id) => rows.filter((c) => c.sourceId === id).length}
          onAdd={(c) => {
            setErr("");
            setChosen(null);
            setLibraryModal(c);
            setLibraryDraft({ ...draftFrom(c), name: nextCopyName(c.name, rows.map((x) => x.name)) });
          }}
          onCustom={() => setTab("custom")}
        />
      ) : null}

      {tab === "custom" ? (
        <CustomTab
          draft={customDraft}
          setDraft={setCustomDraft}
          onSubmit={(e) => void add(e, customDraft, "")}
          onCancel={() => setTab("space")}
          extra={<BotsStep bots={managed} value={picked} onChange={setChosen} />}
        />
      ) : null}

      {libraryModal ? (
        <Modal rise onClose={() => setLibraryModal(null)}>
          <form onSubmit={(e) => void add(e, libraryDraft, libraryModal.id)}>
            <ConnectorFields value={libraryDraft} onChange={setLibraryDraft} fromCatalog={true} catalogGuide={libraryModal.catalogGuide} />
            <BotsStep bots={managed} value={picked} onChange={setChosen} />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit">
                {t("Add to workspace")}
              </Btn>
              <Btn kind="ghost" type="button" onClick={() => setLibraryModal(null)}>
                {t("Cancel")}
              </Btn>
            </div>
          </form>
        </Modal>
      ) : null}

      {edit ? (
        <Modal onClose={() => setEdit(null)}>
          <h3 className="mb-1 text-base font-semibold text-ink">{t("Edit connector")}</h3>
          {used(edit).length ? <p className="mb-4 text-[12.5px] text-ink-2">{t("Changes apply to every Bot that uses it: {{bots}}.", { bots: used(edit).join(", ") })}</p> : <div className="mb-4" />}
          <form onSubmit={(e) => void saveEdit(e)}>
            <ConnectorFields value={editDraft} onChange={setEditDraft} existing />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit">
                {t("Save changes")}
              </Btn>
              <Btn kind="ghost" type="button" onClick={() => setEdit(null)}>
                {t("Cancel")}
              </Btn>
            </div>
          </form>
        </Modal>
      ) : null}
    </div>
  );
}
