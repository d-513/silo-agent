import { useQuery } from "@connectrpc/connect-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import { ui } from "../api";
import { useBots } from "../bots";
import { Btn, btnClass } from "../Btn";
import { Crest } from "../Crest";
import { fail } from "../errors";
import { inputClass } from "../Field";
import { UI } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload } from "../query";
import { Initials } from "../space/People";
import { eligible } from "./roomText.ts";

// NewRoomPage makes a room: a name, the Bots in it (ones the caller manages)
// and who else talks there (people who are on every one of those Bots).
export function NewRoomPage() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/rooms/new" });
  const nav = useNavigate();
  const { bots } = useBots();
  const people = useQuery(UI.method.listSpacePeople, { spaceId }).data?.people ?? [];
  const me = people.find((p) => p.you)?.userId ?? "";
  const mine = (bots ?? []).filter((b) => b.spaceId === spaceId && b.role === "manage");
  const [title, setTitle] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [users, setUsers] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const chosen = mine.filter((b) => picked.includes(b.id)).map((b) => ({ id: b.id, people: b.people.map((p) => p.userId) }));
  const names = Object.fromEntries(mine.map((b) => [b.id, b.name]));
  const others = eligible(
    people.filter((p) => !p.you).map((p) => p.userId),
    chosen,
    me,
    names,
  );
  // Someone picked who is not on a Bot picked after them is left out.
  const going = users.filter((u) => others.some((o) => o.userId === u && !o.why));

  async function create(e: FormEvent) {
    e.preventDefault();
    if (picked.length === 0) return;
    setBusy(true);
    setErr("");
    try {
      const r = await ui.createRoom({ spaceId, title: title.trim(), botIds: picked, userIds: going });
      void reload(UI.method.listRooms);
      void nav({ to: "/rooms/$roomId", params: { roomId: r.id } });
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  const flip = (xs: string[], x: string) => (xs.includes(x) ? xs.filter((y) => y !== x) : [...xs, x]);
  return (
    <div className="silo-page silo-page-sm rise">
      <h1 className="mb-2 text-title">{t("New room")}</h1>
      <p className="mb-6 text-body text-ink-2">{t("A conversation you share with several Bots and people. Every Bot hears what is said there; each keeps its own work, tools and rules.")}</p>
      <form onSubmit={create}>
        <label htmlFor="room-name" className="mb-1.5 block text-label font-medium text-ink-3">
          {t("Name")}
        </label>
        <input id="room-name" className={`${inputClass} mb-6`} placeholder={t("Launch plan")} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
        <section className="mb-6">
          <h2 className="mb-1 text-row font-medium text-ink">{t("Bots")}</h2>
          <p className="mb-3 text-meta text-ink-2">{t("The Bots you manage in this workspace.")}</p>
          {mine.length === 0 ? (
            <p className="text-meta text-ink-3">{t("You manage no Bot here yet.")}</p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {mine.map((b) => (
                <Pick key={b.id} on={picked.includes(b.id)} why="" onClick={() => setPicked(flip(picked, b.id))}>
                  <Crest index={b.crest} size={18} />
                  {b.name}
                </Pick>
              ))}
            </div>
          )}
        </section>
        {others.length > 0 ? (
          <section className="mb-6">
            <h2 className="mb-1 text-row font-medium text-ink">{t("People")}</h2>
            <p className="mb-3 text-meta text-ink-2">{t("Everyone in a room is on every Bot in it: someone who is not on one of yours cannot join.")}</p>
            <div className="flex flex-wrap gap-2">
              {others.map((o) => {
                const p = people.find((x) => x.userId === o.userId)!;
                const why = o.why ? t("Not on {{bot}}", { bot: o.why }) : "";
                return (
                  <Pick key={o.userId} on={going.includes(o.userId)} why={why} onClick={() => setUsers(flip(users, o.userId))}>
                    <Initials email={p.email} size={18} />
                    {p.email}
                  </Pick>
                );
              })}
            </div>
          </section>
        ) : null}
        {err ? (
          <p role="alert" className="mb-3 text-small text-vermilion">
            {err}
          </p>
        ) : null}
        <div className="flex gap-2">
          <Btn kind="primary" type="submit" disabled={busy || picked.length === 0}>
            {busy ? t("Creating…") : t("Create room")}
          </Btn>
          <Link to="/" className={btnClass("secondary")}>
            {t("Cancel")}
          </Link>
        </div>
      </form>
    </div>
  );
}

function Pick({ on, why, onClick, children }: { on: boolean; why: string; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      aria-disabled={!!why || undefined}
      title={why || undefined}
      onClick={() => !why && onClick()}
      className={`flex h-8 items-center gap-1.5 rounded-control px-2.5 text-meta font-medium transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
        why ? "cursor-default bg-well text-ink-3 line-through" : on ? "bg-surface text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "bg-well text-ink-2 hover:text-ink"
      }`}
    >
      {children}
      {on && !why ? <Check size={12} className="text-cobalt" /> : null}
    </button>
  );
}
