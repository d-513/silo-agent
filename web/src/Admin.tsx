import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import type { AuditRow } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function AdminLayout() {
  return (
    <div className="mx-auto w-[760px] p-7">
      <h1 className="text-[22px] font-medium tracking-tight">Admin</h1>
      <nav className="mb-6 mt-4 flex gap-1 border-b border-thread-2">
        <NavLink
          to="/admin/settings"
          className={({ isActive }) =>
            `border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Settings
        </NavLink>
        <NavLink
          to="/admin/connectors"
          className={({ isActive }) =>
            `border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Connectors Library
        </NavLink>
      </nav>
      <Outlet />
    </div>
  );
}

export function AdminSettings() {
  const [model, setModel] = useState("");
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const [saved, setSaved] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    ui.getSettings({})
      .then((x) => setModel(x.model))
      .catch((e) => setErr(fail(e)));
    ui.listAudit({})
      .then((x) => setAudit(x.rows))
      .catch((e) => setErr(fail(e)));
  }, []);
  async function save() {
    setErr("");
    try {
      await ui.putSettings({ model });
      setSaved(true);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  return (
    <div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      <div className="mb-2 text-[11px] font-medium tracking-wide text-stone">Model</div>
      <input className="mb-4 h-9 w-full rounded border border-thread bg-folio px-3" value={model} onChange={(e) => setModel(e.target.value)} />
      <Btn kind="primary" onClick={() => void save()}>
        Save
      </Btn>
      {saved && <span className="ml-3 text-stone">Saved</span>}
      <h2 className="mt-10 mb-3 text-[22px] font-medium">Audit</h2>
      {audit.length === 0 ? (
        <p className="text-stone">No decisions yet.</p>
      ) : (
        <table className="w-full text-left text-[13px]">
          <thead className="bg-cloth text-stone">
            <tr>
              <th className="p-2">When</th>
              <th className="p-2">Bot</th>
              <th className="p-2">Actor</th>
              <th className="p-2">Action</th>
              <th className="p-2">Decision</th>
            </tr>
          </thead>
          <tbody>
            {audit.map((r) => (
              <tr key={r.id} className="border-b border-thread-2">
                <td className="p-2">{r.at}</td>
                <td className="flex items-center gap-2 p-2">
                  <Crest index={r.crest} size={20} />
                  {r.botName}
                </td>
                <td className="p-2">{r.actor}</td>
                <td className="p-2 font-mono">{r.action}</td>
                <td className="p-2">{r.decision}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

export function AccountPage({ email }: { email: string }) {
  return (
    <div className="mx-auto w-[560px] p-7">
      <h1 className="mb-2 text-[22px] font-medium tracking-tight">Account</h1>
      <p className="mb-6 text-stone">Your sign-in. More settings later.</p>
      <div className="mb-1 text-[12px] font-medium text-stone">Email</div>
      <div className="rounded border border-thread bg-folio px-3 py-2">{email}</div>
    </div>
  );
}
