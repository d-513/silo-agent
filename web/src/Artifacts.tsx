import { Package } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "./api";
import { ArtifactCard, itemToBlock, type Artifact } from "./Artifact";
import { fail } from "./errors";
import { feedStamp } from "./format";
import { UI, type ArtifactItem } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";
import { chatLink } from "./links";
import { patch, recheck, reload } from "./query";

function sourceLabel(item: ArtifactItem) {
  switch (item.sourceKind) {
    case "automation":
      return `${t("Automation")} \u00b7 ${item.sourceName}`;
    case "channel":
      return `${t("Channel")} \u00b7 ${item.sourceName}`;
    case "subagent":
      return item.sourceName || t("Subagent");
    case "chat":
      return item.sourceName || t("Chat");
    default:
      return "";
  }
}

// ArtifactsPane lists every artifact this Bot has handed over across its
// conversations, newest first, with version switching, full-screen inspection,
// public link sharing, skill saving, and deletion.
export function ArtifactsPane({
  botId,
  onInspectArtifact,
  onSaveSkill,
  onError,
}: {
  botId: string;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
  onError: (s: string) => void;
}) {
  const q = useQuery(UI.method.listArtifacts, { botId });
  const items = q.data?.artifacts ?? null;

  useEffect(() => {
    void recheck(UI.method.listArtifacts, { botId });
  }, [botId]);
  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteArtifact({ botId, id });
      patch(UI.method.listArtifacts, { botId }, (r) => ({ ...r, artifacts: r.artifacts.filter((a) => a.id !== id) }));
      void reload(UI.method.listArtifactShares, { botId });
    } catch (e) {
      onError(fail(e));
    }
  }

  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">{t("Artifacts")}</h2>
      <p className="mb-6 text-ink-2">{t("Files, skills, and apps this Bot has handed you across conversations, newest first.")}</p>
      {items === null ? (
        <p className="text-ink-3">{t("Loading\u2026")}</p>
      ) : items.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Package size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("No artifacts yet.")}</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">{t("When the Bot hands you a file, a skill, or an app with the artifact tool, it is kept here across versions.")}</p>
        </div>
      ) : (
        <div className="space-y-4">
          {items.map((item) => {
            const label = sourceLabel(item);
            return (
              <div key={item.id} className="space-y-1.5">
                <p className="flex min-w-0 items-center gap-1.5 px-1 text-[12px] leading-4 text-ink-3">
                  <span className="shrink-0 font-mono">{feedStamp(item.updatedAt || item.createdAt)}</span>
                  {label ? (
                    <>
                      <span aria-hidden>{"\u00b7"}</span>
                      {item.sourceKind === "chat" && item.chatId ? (
                        <Link {...chatLink(botId, item.chatId)} className="truncate hover:text-ink hover:underline">
                          {label}
                        </Link>
                      ) : item.sourceKind === "automation" && item.automationId ? (
                        <Link
                          to="/bots/$botId/automations/$automationId"
                          params={{ botId, automationId: item.automationId }}
                          className="truncate hover:text-ink hover:underline"
                        >
                          {label}
                        </Link>
                      ) : (
                        <span className="truncate">{label}</span>
                      )}
                    </>
                  ) : null}
                  <span aria-hidden>{"\u00b7"}</span>
                  <span className="truncate font-mono">{item.path}</span>
                </p>
                <ArtifactCard
                  botId={botId}
                  block={itemToBlock(item)}
                  fullWidth
                  onOpen={onInspectArtifact}
                  onSave={onSaveSkill}
                  onDelete={() => void remove(item.id)}
                />
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
