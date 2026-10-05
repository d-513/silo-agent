import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { ui } from "../api";
import { useSave } from "../Feedback";
import { UI, type Drive, type DriveOption, type DriveTemplate } from "../gen/silo/v1/ui_pb";
import { fail } from "../errors";
import { NAME_RE, uniqueName, visible } from "./driveModel";

// useDriveForm is the add/edit form's state and its three jobs: keep a draft
// drive in step with what is on screen, sign in through the provider's popup,
// and test the connection (again on its own once a connection field changes).
export function useDriveForm({
  botId,
  t,
  drive,
  taken,
  onDone,
}: {
  botId: string;
  t: DriveTemplate;
  drive?: Drive;
  taken: Set<string>;
  onDone: () => void;
}) {
  const editing = !!drive;
  const [draft, setDraft] = useState<Drive | undefined>(drive);
  const [values, setValues] = useState<Record<string, string>>(() => ({ ...(drive?.options ?? {}) }));
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [name, setName] = useState(drive?.name ?? uniqueName(t.title, taken));
  const [readOnly, setReadOnly] = useState(drive?.readOnly ?? false);
  const [err, setErr] = useState("");
  const [authBusy, setAuthBusy] = useState(false);
  const [authErr, setAuthErr] = useState("");
  const [test, setTest] = useState<{ state: "idle" | "busy" | "ok" | "fail"; msg: string }>({ state: "idle", msg: "" });
  const saver = useSave();
  const draftRef = useRef<Drive | undefined>(drive);
  draftRef.current = draft;

  const oauth = t.authKind === "oauth2";
  const userVars = t.vars.filter((v) => v.kind === "user" && v.type !== "hidden" && visible(v, values, t));
  const connectVars = userVars.filter((v) => !v.advanced && v.type !== "pick" && v.type !== "folder");
  const mountVars = userVars.filter((v) => !v.advanced && (v.type === "pick" || v.type === "folder"));
  const advancedVars = userVars.filter((v) => v.advanced);
  const connected = oauth ? !!draft?.connected : editing || test.state === "ok";
  const requiredMissing = connectVars.filter((v) => v.required && !(values[v.key] || secrets[v.key] || draft?.secretsSet.includes(v.key) || v.defaultValue));

  // Only connection fields change what a test proves. Choosing a folder or a
  // pick never does, so it must not send the owner back to Test connection.
  const connectionField = (k: string) => {
    const v = t.vars.find((x) => x.kind === "user" && x.key === k);
    return !!v && v.type !== "folder" && v.type !== "pick";
  };
  const set = (k: string, val: string) => setValues((m) => ({ ...m, [k]: val }));
  const setSecret = (k: string, val: string) => setSecrets((m) => ({ ...m, [k]: val }));

  function options() {
    const out: Record<string, string> = {};
    for (const v of t.vars) {
      if (v.kind !== "user") continue;
      if (v.secret) {
        if (secrets[v.key]) out[v.key] = secrets[v.key];
      } else if (values[v.key] !== undefined && values[v.key] !== "") out[v.key] = values[v.key];
    }
    return out;
  }

  // Sync the draft with what is on screen. Editing a saved drive never writes
  // here: its changes apply only on Save.
  const sync = useCallback(async (): Promise<Drive> => {
    const cur = draftRef.current;
    if (cur && !cur.draft) return cur;
    const d = await ui.saveDrive({ botId, id: cur?.id ?? "", template: t.key, draft: true, readOnly, options: options() });
    setDraft(d);
    draftRef.current = d;
    return d;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [botId, t.key, readOnly, values, secrets]);

  const reloadDraft = useCallback(async () => {
    const cur = draftRef.current;
    if (!cur) return;
    const r = await ui.listDrives({ botId, draftId: cur.draft ? cur.id : "" });
    const fresh = r.drives.find((x) => x.id === cur.id);
    if (fresh) {
      setDraft(fresh);
      setValues((m) => ({ ...fresh.options, ...m, ...Object.fromEntries(Object.entries(fresh.options).filter(([k]) => !m[k])) }));
    }
  }, [botId]);

  // The sign-in popup posts back when it is done; polling covers a lost message.
  const polled = useQuery(UI.method.listDrives, authBusy && draft ? { botId, draftId: draft.draft ? draft.id : "" } : skipToken, {
    refetchInterval: 2000,
    // The sign-in happens in another window, so keep asking while this one is behind it.
    refetchIntervalInBackground: true,
  });
  useEffect(() => {
    const cur = draftRef.current;
    const fresh = cur && polled.data?.drives.find((x) => x.id === cur.id);
    if (!cur || !fresh || !authBusy) return;
    if (fresh.connected && fresh.account !== cur.account) {
      setAuthBusy(false);
      setDraft(fresh);
      setValues((m) => ({ ...fresh.options, ...m }));
    }
  }, [polled.data, authBusy]);
  useEffect(() => {
    if (!authBusy) return;
    const onMsg = (e: MessageEvent) => {
      const m = e.data as { silo?: string; ok?: boolean; message?: string };
      if (m?.silo !== "drive-auth") return;
      setAuthBusy(false);
      if (!m.ok) setAuthErr(m.message || "Sign-in did not finish.");
      void reloadDraft();
    };
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [authBusy, botId, reloadDraft]);

  async function signIn() {
    setAuthErr("");
    // Open the window inside the click so popup blockers allow it, then point
    // it at the provider once the server has the URL.
    const w = window.open("about:blank", "silo-drive-auth", "width=520,height=720");
    try {
      const d = await sync();
      const r = await ui.beginDriveAuth({ id: d.id });
      if (w) {
        w.location.href = r.url;
        setAuthBusy(true);
      } else {
        setAuthErr("Your browser blocked the sign-in window. Allow pop-ups for Silo and try again.");
      }
    } catch (e) {
      w?.close();
      setAuthErr(fail(e));
    }
  }

  async function runTest() {
    testedSig.current = connSig;
    setTest({ state: "busy", msg: "" });
    try {
      const d = await sync();
      const r = await ui.browseDrive({ id: d.id, path: "" });
      const n = r.dirs.length;
      setTest({ state: "ok", msg: n === 0 ? "Connected. The drive is empty at the top level." : `Connected. Found ${n} folder${n === 1 ? "" : "s"}.` });
    } catch (e) {
      setTest({ state: "fail", msg: fail(e) });
    }
  }

  // Once the owner has tested, editing a connection field re-tests on its
  // own after a pause instead of asking for another click.
  const connSig = JSON.stringify([
    Object.entries(values).filter(([k]) => connectionField(k)).sort(),
    Object.entries(secrets).filter(([k]) => connectionField(k)).sort(),
  ]);
  const testedSig = useRef("");
  useEffect(() => {
    if (oauth || editing || test.state === "busy") return;
    if (test.state === "idle" || connSig === testedSig.current) return;
    if (requiredMissing.length > 0) {
      setTest({ state: "idle", msg: "" });
      return;
    }
    const timer = setTimeout(() => void runTest(), 900);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connSig]);

  const browse = useCallback(
    async (path: string) => {
      const d = await sync();
      return (await ui.browseDrive({ id: d.id, path })).dirs;
    },
    [sync],
  );

  const loaders = useRef<Record<string, () => Promise<DriveOption[]>>>({});
  function pickLoader(key: string) {
    if (!loaders.current[key]) {
      loaders.current[key] = async () => {
        const d = await sync();
        return (await ui.pickDriveOptions({ id: d.id, key })).options;
      };
    }
    return loaders.current[key];
  }
  // A new sign-in means new options.
  useEffect(() => {
    loaders.current = {};
  }, [draft?.account]);

  const nameOk = NAME_RE.test(name) && (!taken.has(name) || name === drive?.name);
  const canSave = connected && nameOk && saver.state !== "saving" && (oauth || requiredMissing.length === 0);

  async function save() {
    setErr("");
    try {
      await saver.run(async () => {
        const cur = draftRef.current;
        await ui.saveDrive({
          botId,
          id: cur?.id ?? "",
          template: t.key,
          name,
          readOnly,
          draft: false,
          options: options(),
        });
      });
      onDone();
    } catch (e) {
      setErr(fail(e));
    }
  }

  const pathPreview = `/workspace/drives/${name || "…"}`;

  return {
    editing, draft, values, setValues, secrets, name, setName, readOnly, setReadOnly, err, authBusy, authErr, test, saver,
    oauth, connectVars, mountVars, advancedVars, connected, requiredMissing, set, setSecret, signIn, runTest, browse, pickLoader,
    nameOk, canSave, save, pathPreview,
  };
}
