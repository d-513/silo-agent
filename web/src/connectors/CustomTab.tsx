import { Btn } from "../Btn";
import { ConnectorFields, emptyDraft, type ConnectorDraft } from "../ConnectorForm";

// The form for a connector that is not in the library: an HTTP MCP server, or a
// STDIO one in its own sidecar.
export function CustomTab({
  draft,
  setDraft,
  onSubmit,
  onCancel,
}: {
  draft: ConnectorDraft;
  setDraft: (d: ConnectorDraft) => void;
  onSubmit: (e: React.FormEvent) => void;
  onCancel: () => void;
}) {
  return (
    <div className="max-w-2xl space-y-6">
      <div className="rounded-card shadow-card bg-surface p-6">
        <div className="mb-5 border-b border-line pb-4">
          <h2 className="text-base font-semibold text-ink">Connect Custom MCP Server</h2>
          <p className="mt-1 text-xs text-ink-2">
            Attach your own MCP integration. Choose HTTP for remote servers or STDIO for an isolated Docker sidecar container.
          </p>
        </div>

        <form onSubmit={onSubmit}>
          <ConnectorFields value={draft} onChange={setDraft} />
          <div className="mt-6 flex items-center gap-2.5">
            <Btn kind="primary" type="submit">
              Add connector
            </Btn>
            <Btn kind="ghost" type="button" onClick={() => setDraft(emptyDraft())}>
              Reset
            </Btn>
            <Btn kind="ghost" type="button" onClick={onCancel}>
              Cancel
            </Btn>
          </div>
        </form>
      </div>
    </div>
  );
}
