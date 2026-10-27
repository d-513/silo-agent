import { useQuery } from "@connectrpc/connect-query";
import { Copy, X } from "lucide-react";
import { useMemo, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { startConnectorAuth } from "../connectorAuth";
import { ConnectorFields, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "./form";
import { fail } from "../errors";
import { UI, type BotConnector, type Connector } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { widePage } from "../PageHead";
import { Modal } from "../Modal";
import { ConnectorToolbar } from "./ConnectorToolbar";
import { CustomTab } from "./CustomTab";
import { InUseTab } from "./InUseTab";
import { LibraryTab } from "./LibraryTab";
import { attachedCount, categoriesOf, FEATURED, filterAttached, filterCatalog, groupByCategory, nextCopyName, type TabMode } from "./model";
import { SpaceCard } from "./SpaceCard";
import { useBotConnectors } from "./useBotConnectors";
import { t } from "../i18n.ts";

export function BotConnectors({
  botId,
  spaceId,
  onNeedAuth,
}: {
  botId: string;
  spaceId: string;
  onNeedAuth: (row: BotConnector | null) => void;
}) {
  const [err, setErr] = useState("");
  const { attached, catalog, load: loadBot } = useBotConnectors(botId, setErr);
  // The Workspace's other connections: one press gives one to this Bot.
  const spaceQ = useQuery(UI.method.listSpaceConnectors, { spaceId }, { refetchInterval: 5000 });
  const others = useMemo(() => (spaceQ.data?.connectors ?? []).filter((c) => !attached.some((r) => r.connector?.id === c.id)), [spaceQ.data, attached]);
  const load = () => Promise.all([loadBot(), reload(UI.method.listSpaceConnectors, { spaceId })]);
  const [tab, setTab] = useState<TabMode>("in_use");
  const [search, setSearch] = useState("");
  const [selectedCategory, setSelectedCategory] = useState("all");

  // Library modal popup state
  const [libraryModal, setLibraryModal] = useState<Connector | null>(null);
  const [libraryDraft, setLibraryDraft] = useState<ConnectorDraft>(emptyDraft());

  // Custom tab draft state
  const [customDraft, setCustomDraft] = useState<ConnectorDraft>(emptyDraft());

  // Edit attached connector state
  const [edit, setEdit] = useState<BotConnector | null>(null);
  const [editDraft, setEditDraft] = useState<ConnectorDraft>(emptyDraft());

  // Full status/error text of one connector (the status line truncates it)
  const [errView, setErrView] = useState<BotConnector | null>(null);

  // Which connector is being refreshed
  const [busy, setBusy] = useState("");

  const categories = useMemo(() => categoriesOf(catalog), [catalog]);
  const filteredCatalog = useMemo(() => filterCatalog(catalog, search, selectedCategory), [catalog, search, selectedCategory]);
  const featured = useMemo(() => FEATURED.flatMap((key) => catalog.filter((c) => c.builtin === key)), [catalog]);
  const catalogByCategory = useMemo(() => groupByCategory(filteredCatalog), [filteredCatalog]);
  const filteredAttached = useMemo(() => filterAttached(attached, search), [attached, search]);

  function openLibraryModal(c: Connector) {
    setErr("");
    setLibraryModal(c);
    setLibraryDraft({
      ...draftFrom(c),
      name: nextCopyName(c.name, attached.map((x) => x.connector?.name ?? "")),
    });
  }

  function closeLibraryModal() {
    setLibraryModal(null);
    setLibraryDraft(emptyDraft());
  }

  async function submitAddLibrary(e: React.FormEvent) {
    e.preventDefault();
    if (!libraryModal) return;
    setErr("");
    try {
      const row = await ui.createBotConnector({
        botId,
        ...specOf(libraryDraft),
        sourceId: libraryModal.id,
      });
      closeLibraryModal();
      await load();
      setTab("in_use");
      if (row.authStatus === "needs_auth" && row.connector?.auth === "oauth") onNeedAuth(row);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function submitAddCustom(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const row = await ui.createBotConnector({
        botId,
        ...specOf(customDraft),
        sourceId: "",
      });
      setCustomDraft(emptyDraft());
      await load();
      setTab("in_use");
      if (row.authStatus === "needs_auth" && row.connector?.auth === "oauth") onNeedAuth(row);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    const c = edit?.connector;
    if (!c) return;
    setErr("");
    try {
      await ui.updateConnector({ id: c.id, ...specOf(editDraft) });
      setEdit(null);
      await load();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function authorize(row: BotConnector) {
    setErr("");
    try {
      await startConnectorAuth({ botId, id: row.id });
      onNeedAuth(null);
    } catch (e) {
      setErr(fail(e));
    }
  }

  async function refresh(id: string) {
    setErr("");
    setBusy(id);
    try {
      await ui.refreshBotConnector({ botId, id });
      await load();
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy("");
    }
  }

  async function use(c: Connector) {
    setErr("");
    try {
      const row = await ui.grantConnector({ botId, connectorId: c.id });
      await load();
      setTab("in_use");
      if (row.authStatus === "needs_auth" && row.connector?.auth === "oauth") onNeedAuth(row);
    } catch (e) {
      setErr(fail(e));
    }
  }

  async function handleDetach(id: string) {
    setErr("");
    try {
      await ui.detachConnector({ botId, id });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }

  function startEdit(row: BotConnector) {
    setEdit(row);
    if (row.connector) setEditDraft(draftFrom(row.connector));
  }

  return (
    <div className={widePage}>
      {/* Header & Page Description */}
      <div className="mb-6 space-y-1.5">
        <h1 className="text-title text-ink">{t("Connectors")}</h1>
        <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">
          {t("Connectors let this Bot work with outside services like GitHub, Slack, Notion, or your custom MCP servers. Search the library to add verified integrations or link bespoke tools directly into your bot runtime.")}
        </p>
      </div>

      {err && (
        <div className="mb-4 flex items-center justify-between rounded-sm bg-vermilion-pale px-3.5 py-2.5 text-[13px] text-vermilion">
          <span>{err}</span>
          <button type="button" onClick={() => setErr("")} className="text-vermilion hover:opacity-80">
            <X size={14} />
          </button>
        </div>
      )}

      <ConnectorToolbar
        tabs={[
          { mode: "in_use", label: t("In Use"), count: attached.length },
          { mode: "workspace", label: t("Workspace"), count: others.length },
          { mode: "library", label: t("Library"), count: catalog.length },
          { mode: "custom", label: t("Custom"), add: true },
        ]}
        tab={tab}
        setTab={setTab}
        search={search}
        setSearch={setSearch}
        onAdd={() => setTab(others.length ? "workspace" : "library")}
      />

      {tab === "in_use" && (
        <InUseTab
          attached={attached}
          filtered={filteredAttached}
          search={search}
          setSearch={setSearch}
          setTab={setTab}
          busy={busy}
          onEdit={startEdit}
          onAuthorize={(row) => void authorize(row)}
          onRefresh={(id) => void refresh(id)}
          onDetach={(id) => void handleDetach(id)}
          onViewError={setErrView}
        />
      )}

      {tab === "workspace" &&
        (others.length === 0 ? (
          <div className="rounded-card bg-well p-6 text-[13px] text-ink-2">{t("This Bot already uses every connector in its workspace. Add a new one from the Library.")}</div>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {others.map((c) => (
              <SpaceCard
                key={c.id}
                c={c}
                actions={
                  <Btn kind="primary" onClick={() => void use(c)}>
                    {t("Use in this Bot")}
                  </Btn>
                }
              />
            ))}
          </div>
        ))}

      {tab === "library" && (
        <LibraryTab
          catalog={catalog}
          filtered={filteredCatalog}
          categories={categories}
          byCategory={catalogByCategory}
          featured={featured}
          selectedCategory={selectedCategory}
          setSelectedCategory={setSelectedCategory}
          search={search}
          setSearch={setSearch}
          countFor={(id) => attachedCount(attached, id)}
          onAdd={openLibraryModal}
          onCustom={() => setTab("custom")}
        />
      )}

      {tab === "custom" && <CustomTab draft={customDraft} setDraft={setCustomDraft} onSubmit={submitAddCustom} onCancel={() => setTab("library")} />}

      {/* POPUP: Add From Library Modal */}
      {libraryModal && (
        <Modal rise onClose={closeLibraryModal}>
          <form onSubmit={submitAddLibrary}>
            <ConnectorFields value={libraryDraft} onChange={setLibraryDraft} fromCatalog={true} catalogGuide={libraryModal.catalogGuide} />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit">
                {t("Add to Bot")}
              </Btn>
              <Btn kind="ghost" type="button" onClick={closeLibraryModal}>
                {t("Cancel")}
              </Btn>
            </div>
          </form>
        </Modal>
      )}

      {/* POPUP: the full error of one connector */}
      {errView && (
        <Modal size="wide" dismissOnBackdrop onClose={() => setErrView(null)}>
          <h3 className="mb-1 text-base font-semibold text-ink">{t("{{name}} error", { name: errView.connector?.name ?? "" })}</h3>
          <p className="mb-3 text-[12px] text-ink-3">{errView.statusDetail}</p>
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-well p-3 font-mono text-[12px] leading-relaxed text-ink">
            {errView.lastError || errView.statusDetail}
          </pre>
          <div className="mt-4 flex items-center gap-2">
            <Btn kind="ghost" onClick={() => void navigator.clipboard?.writeText(errView.lastError || errView.statusDetail)}>
              <Copy size={13} />
              <span>{t("Copy")}</span>
            </Btn>
            <Btn kind="ghost" onClick={() => setErrView(null)}>
              {t("Close")}
            </Btn>
          </div>
        </Modal>
      )}

      {/* POPUP: Edit Attached Connector Modal */}
      {edit && (
        <Modal onClose={() => setEdit(null)}>
          <h3 className="mb-1 text-base font-semibold text-ink">{t("Edit connector")}</h3>
          {/* The connection is the Workspace's: an edit reaches every Bot that uses it. */}
          <p className="mb-4 text-[12.5px] text-ink-2">
            {!edit.connector?.canEdit
              ? t("Only whoever added this connector, or an admin of the workspace, can change it.")
              : edit.alsoIn.length || edit.alsoInHidden
                ? t("Changes apply to every Bot that uses it: {{bots}}.", { bots: [t("this Bot"), ...edit.alsoIn].join(", ") })
                : ""}
          </p>
          <form onSubmit={saveEdit}>
            <ConnectorFields value={editDraft} onChange={setEditDraft} existing />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit" disabled={!edit.connector?.canEdit}>
                {t("Save changes")}
              </Btn>
              <Btn kind="ghost" type="button" onClick={() => setEdit(null)}>
                {t("Cancel")}
              </Btn>
            </div>
          </form>
        </Modal>
      )}
    </div>
  );
}
