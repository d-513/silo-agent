import { useQuery } from "@connectrpc/connect-query";
import { Download } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell } from "../Field";
import { UI, type Provider, type ProviderProgram as Program } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { put, reload } from "../query";
import { ProviderAccount } from "./ProviderAccount";
import { installPercent, programLine } from "./providerProgram.ts";

// ProviderAccess is how a provider without a key is made usable: the program
// it runs on this server, when Silo does not ship it, and then the sign-in.
export function ProviderAccess({ provider }: { provider: Provider }) {
  const input = { provider: provider.id };
  const program = useQuery(UI.method.getProviderProgram, input, {
    enabled: provider.program,
    retry: false,
    // A download is followed while it runs.
    refetchInterval: (q) => (q.state.data?.installing ? 1000 : false),
  });
  const ready = !provider.program || program.data?.installed === true;
  return (
    <>
      {provider.program ? <ProgramPanel provider={provider} program={program.data} loading={program.isPending} loadError={program.error ? fail(program.error) : ""} /> : null}
      {provider.signIn && ready ? <ProviderAccount provider={provider} /> : null}
    </>
  );
}

function ProgramPanel({ provider, program, loading, loadError }: { provider: Provider; program?: Program; loading: boolean; loadError: string }) {
  const input = { provider: provider.id };
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // When a download ends, what the provider can do changed: who is signed
  // in, its lamp, the models it lists.
  const was = useRef(false);
  const installing = program?.installing === true;
  useEffect(() => {
    if (was.current && !installing) {
      void reload(UI.method.getProviderAccount, input);
      void reload(UI.method.getSettings);
      void reload(UI.method.listProviderModels, input);
    }
    was.current = installing;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [installing, provider.id]);

  if (loading) {
    return (
      <div className="flex h-9 items-center gap-2 text-small text-ink-3">
        <Spinner size={13} /> {t("Looking for the program…")}
      </div>
    );
  }
  if (loadError || !program) return <ErrorWell>{loadError || t("Could not read the program's state.")}</ErrorWell>;

  const install = async () => {
    setBusy(true);
    setError("");
    try {
      put(UI.method.getProviderProgram, input, await ui.installProviderProgram(input));
    } catch (e) {
      setError(fail(e));
    } finally {
      setBusy(false);
    }
  };
  const percent = installPercent(program);

  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className="flex min-w-0 flex-1 items-center gap-2 text-row text-ink">
          <span aria-hidden className={`h-[7px] w-[7px] shrink-0 rounded-full ${program.installed ? "bg-lamp" : "bg-canvas shadow-[inset_0_0_0_1.5px_var(--color-line-strong)]"}`} />
          <span className="min-w-0 truncate" title={program.path || undefined}>
            {programLine(program, t)}
          </span>
        </span>
        {program.canInstall ? (
          <Btn size="sm" kind={program.installed ? "secondary" : "primary"} disabled={busy || installing} onClick={() => void install()}>
            {busy || installing ? <Spinner size={13} tone={program.installed ? "ink" : "on-ink"} /> : <Download size={13} />}
            {installing ? (percent === null ? t("Downloading…") : t("Downloading… {{percent}}%", { percent })) : program.installed ? (program.managed ? t("Update") : t("Install Silo's own copy")) : t("Install")}
          </Btn>
        ) : null}
      </div>
      {program.canInstall && (!program.installed || installing) ? <p className="text-small leading-5 text-ink-2">{program.about}</p> : null}
      {program.problem && !installing ? <ErrorWell>{program.problem}</ErrorWell> : null}
      {error ? <ErrorWell>{error}</ErrorWell> : null}
    </div>
  );
}
