import { Copy, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { startConnectorAuth } from "../connectorAuth";
import { ConnectorFields, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "../ConnectorForm";
import { fail } from "../errors";
import type { BotConnector, Connector } from "../gen/silo/v1/ui_pb";
import { widePage } from "../PageHead";
import { ConnectorModal } from "./ConnectorModal";
import { ConnectorToolbar } from "./ConnectorToolbar";
import { CustomTab } from "./CustomTab";
import { InUseTab } from "./InUseTab";
import { LibraryTab } from "./LibraryTab";
import { attachedCount, categoriesOf, FEATURED, filterAttached, filterCatalog, groupByCategory, nextCopyName, type TabMode } from "./model";
import { useBotConnectors } from "./useBotConnectors";

export function BotConnectors({
  botId,
  onNeedAuth,
}: {
  botId: string;
  onNeedAuth: (row: BotConnector | null) => void;
}) {
  const [err, setErr] = useState("");
  const { attached, catalog, load } = useBotConnectors(botId, setErr);
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

  const searchInputRef = useRef<HTMLInputElement>(null);

  // Keyboard shortcut ⌘K / Ctrl+K for search
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

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
      await startConnectorAuth(botId, row.id);
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
        <h1 className="text-title text-ink">Connectors</h1>
        <p className="max-w-3xl text-[13px] leading-relaxed text-ink-2">
          Connectors let this Bot work with outside services like GitHub, Slack, Notion, or your custom MCP servers. Search the library to add verified integrations or link bespoke tools directly into your bot runtime.
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
        tab={tab}
        setTab={setTab}
        attachedCount={attached.length}
        catalogCount={catalog.length}
        search={search}
        setSearch={setSearch}
        searchRef={searchInputRef}
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
        <ConnectorModal rise onClose={closeLibraryModal}>
          <form onSubmit={submitAddLibrary}>
            <ConnectorFields value={libraryDraft} onChange={setLibraryDraft} fromCatalog={true} catalogGuide={libraryModal.catalogGuide} />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit">
                Add to Bot
              </Btn>
              <Btn kind="ghost" type="button" onClick={closeLibraryModal}>
                Cancel
              </Btn>
            </div>
          </form>
        </ConnectorModal>
      )}

      {/* POPUP: the full error of one connector */}
      {errView && (
        <ConnectorModal wide dismissOnBackdrop onClose={() => setErrView(null)}>
          <h3 className="mb-1 text-base font-semibold text-ink">{errView.connector?.name} error</h3>
          <p className="mb-3 text-[12px] text-ink-3">{errView.statusDetail}</p>
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-well p-3 font-mono text-[12px] leading-relaxed text-ink">
            {errView.lastError || errView.statusDetail}
          </pre>
          <div className="mt-4 flex items-center gap-2">
            <Btn kind="ghost" onClick={() => void navigator.clipboard?.writeText(errView.lastError || errView.statusDetail)}>
              <Copy size={13} />
              <span>Copy</span>
            </Btn>
            <Btn kind="ghost" onClick={() => setErrView(null)}>
              Close
            </Btn>
          </div>
        </ConnectorModal>
      )}

      {/* POPUP: Edit Attached Connector Modal */}
      {edit && (
        <ConnectorModal onClose={() => setEdit(null)}>
          <h3 className="mb-4 text-base font-semibold text-ink">Edit connector</h3>
          <form onSubmit={saveEdit}>
            <ConnectorFields value={editDraft} onChange={setEditDraft} existing />
            <div className="mt-6 flex items-center gap-2">
              <Btn kind="primary" type="submit">
                Save changes
              </Btn>
              <Btn kind="ghost" type="button" onClick={() => setEdit(null)}>
                Cancel
              </Btn>
            </div>
          </form>
        </ConnectorModal>
      )}
    </div>
  );
}
