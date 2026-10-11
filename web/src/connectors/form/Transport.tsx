import { ArgsEditor } from "./ListEditors";
import type { ConnectorDraft } from "./draft";
import { Segmented } from "./atoms";
import { TextRow } from "./TextRow";
import { t } from "../../i18n.ts";
import { Trans } from "../../Trans.tsx";

type Edit = <K extends keyof ConnectorDraft>(k: K, v: ConnectorDraft[K]) => void;

// How an MCP connector is reached: HTTP (URL and auth) or STDIO (command,
// arguments and an optional image).
export function TransportFields({
  value,
  set,
  existing,
  fromCatalog,
  allowStdioImage,
}: {
  value: ConnectorDraft;
  set: Edit;
  existing?: boolean;
  fromCatalog?: boolean;
  allowStdioImage?: boolean;
}) {
  return (
    <>
      <div className="mb-1 text-[12px] font-medium text-ink-3">{t("Transport")}</div>
      <div className="mb-3">
        <Segmented
          value={value.transport}
          onChange={(v) => set("transport", v)}
          options={[
            { id: "http", label: "HTTP" },
            { id: "stdio", label: "STDIO" },
          ]}
        />
      </div>
      <p className="mb-3 rounded-sm bg-well px-3 py-2 text-[12px] text-ink-3">
        <Trans
          i18nKey="You can reference operator connector variables as <mono>{{name}}</mono> in the URL, headers, OAuth fields, command, arguments, and env values. Set them under Admin → Settings."
          values={{ name: "${NAME}" }}
          components={{ mono: <span className="font-mono" /> }}
        />
      </p>
      {value.transport === "stdio" ? (
        <>
          <TextRow label={t("Command")} mono value={value.stdioCommand} onChange={(e) => set("stdioCommand", e.target.value)} placeholder={"npx"} required={!fromCatalog} />
          <ArgsEditor value={value} set={set} />
          {allowStdioImage && (
            <TextRow label={t("Image", { context: "container" })} mono value={value.stdioImage} onChange={(e) => set("stdioImage", e.target.value)} placeholder={"localhost/silo-mcp-stdio:v1"} />
          )}
        </>
      ) : (
        <HttpFields value={value} set={set} existing={existing} fromCatalog={fromCatalog} />
      )}
    </>
  );
}

function HttpFields({ value, set, fromCatalog, existing }: { value: ConnectorDraft; set: Edit; fromCatalog?: boolean; existing?: boolean }) {
  return (
    <>
      <TextRow label={t("URL")} mono value={value.httpUrl} onChange={(e) => set("httpUrl", e.target.value)} placeholder={"https://…"} required={!fromCatalog} />
      <div className="mb-1 text-[12px] font-medium text-ink-3">{t("Auth")}</div>
      <div className="mb-3">
        <Segmented
          value={value.auth}
          onChange={(v) => set("auth", v)}
          options={[
            { id: "none", label: t("None") },
            { id: "oauth", label: "OAuth" },
          ]}
        />
      </div>
      {value.auth === "oauth" && (
        <>
          <p className="mb-2 text-ink-2">
            <Trans i18nKey="Leave blank when the server registers clients itself. GitHub and similar need a pre-registered OAuth App whose callback is <mono>/oauth/callback</mono> on this Silo." components={{ mono: <span className="font-mono" /> }} />
          </p>
          <TextRow label={t("OAuth Client ID")} mono value={value.oauthClientId} onChange={(e) => set("oauthClientId", e.target.value)} autoComplete="off" />
          <label className="mb-1 block text-[12px] font-medium text-ink-3">{t("OAuth Client Secret")}</label>
          <input
            className="mb-3 h-9 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 font-mono"
            type="password"
            value={value.oauthClientSecret}
            onChange={(e) => set("oauthClientSecret", e.target.value)}
            placeholder={existing && value.hasOauthClientSecret ? t("unchanged") : ""}
            autoComplete="new-password"
          />
        </>
      )}
    </>
  );
}
