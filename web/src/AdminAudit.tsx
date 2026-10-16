import { AuditPane } from "./AuditPane";
import { t } from "./i18n.ts";

// Admin → Audit: every Bot's timeline, and where each Bot went. The attend line
// is the point: what the network filter blocked and what a rule refused, with
// the Bot beside it. Opening a Bot's own Audit tab clears only that Bot's badge.
export function AdminAudit() {
  return (
    <div>
      <p className="mb-5 max-w-[640px] text-[13.5px] leading-[21px] text-ink-2">
        {t("What every Bot did: calls and who allowed them, results the output filter did not pass, and what the network filter blocked or flagged. The traffic view sums what each Bot's machine talked to, host by host.")}
      </p>
      <AuditPane botId="" embedded />
    </div>
  );
}
