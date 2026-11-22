import { useQuery } from "@connectrpc/connect-query";
import { Download } from "lucide-react";
import { Btn } from "../Btn";
import { Panel } from "../Field";
import { Status } from "../Lamp";
import { UI } from "../gen/silo/v1/ui_pb";
import type { ProxyStatus } from "../gen/silo/v1/siloos_pb";
import { t } from "../i18n.ts";
import { proxyLook } from "../statusLook";

// saveRoot hands the viewer Caddy's root certificate, to install on their
// devices so Silo's own certificates are trusted.
function saveRoot(pem: string) {
  const url = URL.createObjectURL(new Blob([pem], { type: "application/x-x509-ca-cert" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = "silo-root-ca.crt";
  a.click();
  URL.revokeObjectURL(url);
}

function Line({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[140px_minmax(0,1fr)] gap-3 py-1.5 text-small">
      <span className="text-ink-3">{label}</span>
      <span className="min-w-0 break-words text-ink">{children}</span>
    </div>
  );
}

// ProxyLines says what Caddy serves and where each certificate comes from.
export function ProxyLines({ proxy }: { proxy: ProxyStatus }) {
  const issuer = proxy.issuer === "internal" ? t("Silo's own certificate authority") : proxy.dnsProvider ? t("Let's Encrypt, through {{provider}}", { provider: proxy.dnsProvider }) : t("Let's Encrypt, on port 80");
  return (
    <div>
      <Line label={t("State")}>
        <Status look={proxyLook(proxy.state)} />
      </Line>
      {proxy.error ? <Line label={t("Error")}><span className="text-vermilion">{proxy.error}</span></Line> : null}
      <Line label={t("Silo's address")}>
        {proxy.host ? (
          <>
            <span className="font-mono">{proxy.host}</span> · {issuer}
          </>
        ) : (
          <span className="text-ink-3">{proxy.note || t("Not served.")}</span>
        )}
      </Line>
      {proxy.contentHost ? (
        <Line label={t("Content host")}>
          <span className="font-mono">*.{proxy.contentHost}</span> · {proxy.issuer === "acme" && !proxy.dnsProvider ? t("a certificate per name, at its first visit") : t("one wildcard certificate")}
        </Line>
      ) : null}
      {proxy.local.length ? (
        <Line label={t("This machine")}>
          <span className="font-mono">{proxy.local.join(", ")}</span> · {t("Silo's own certificate authority")}
        </Line>
      ) : null}
    </div>
  );
}

// RootButton downloads Caddy's root certificate while it certifies anything.
export function RootButton({ proxy }: { proxy: ProxyStatus }) {
  if (!proxy.rootCa) return null;
  return (
    <Btn size="sm" icon={<Download size={12} />} onClick={() => saveRoot(proxy.rootCa)}>
      {t("Root certificate")}
    </Btn>
  );
}

// CaddyHealth is Admin → Settings → HTTPS's view of Caddy as the control
// plane last left it (GetSiloOS's proxy, which every admin may read).
export function CaddyHealth() {
  const proxy = useQuery(UI.method.getSiloOS, {}, { refetchInterval: 15_000 }).data?.proxy;
  if (!proxy?.configured) return null;
  return (
    <Panel
      title={t("What Caddy serves")}
      note={t("Silo compares this with Caddy every 15 seconds and loads it again when they differ. Install the root certificate on a device to trust Silo's own certificates there.")}
      action={<RootButton proxy={proxy} />}
    >
      <ProxyLines proxy={proxy} />
    </Panel>
  );
}
