import { useQuery } from "@connectrpc/connect-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowUp, Plus, Square, Users, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { ui } from "../api";
import { useBots } from "../bots";
import { Btn, btnClass } from "../Btn";
import { ChipRadio } from "../ChipRadio";
import { Crest } from "../Crest";
import { fail } from "../errors";
import { inputClass } from "../Field";
import { UI, type Room, type RoomBot, type RoomMessage } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { Md } from "../Md";
import { NavButton } from "../nav/NavSheet";
import { reload } from "../query";
import { Initials } from "../space/People";
import { useWide } from "../useWide";
import { anyWorking, eligible, mention, parts, type BotPeople, type RoomBotRef } from "./roomText.ts";
import { useRoomStream } from "./useRoomStream";

// RoomPage is a room: the conversation several Bots and people share. The
// thread is the room's own transcript; each Bot's work stays in its seat, which
// only its managers open. Beside it (or over it, narrow) is who is in the room.
export function RoomPage() {
  const { roomId } = useParams({ from: "/_authed/rooms/$roomId" });
  const { room, setRoom, messages, earlier, gone } = useRoomStream(roomId);
  const wide = useWide();
  // Narrow, the panel takes the page and starts shut; wide, it sits beside
  // the thread and stays as the reader last left it.
  const [narrowPanel, setNarrowPanel] = useState(false);
  const [widePanel, setWidePanel] = useState(readPanel);
  const panel = wide ? widePanel : narrowPanel;
  const togglePanel = () => {
    if (!wide) return setNarrowPanel((p) => !p);
    setWidePanel((p) => {
      writePanel(!p);
      return !p;
    });
  };
  const refs = useMemo<RoomBotRef[]>(() => (room?.bots ?? []).map((b) => ({ id: b.botId, name: b.name })), [room?.bots]);

  useEffect(() => {
    // The sidebar's unread counts follow what this page has shown.
    if (messages.length) void reload(UI.method.listRooms);
  }, [messages.length]);

  if (gone) {
    return (
      <div className="silo-page silo-page-sm rise">
        <h1 className="mb-2 text-title">{t("This room is not open to you")}</h1>
        <p className="mb-5 text-body text-ink-2">{gone}</p>
        <Link to="/" className={btnClass("secondary")}>
          {t("Back to Bots")}
        </Link>
      </div>
    );
  }
  const showPanel = room && panel;
  return (
    <div className="flex h-full min-h-0 flex-col bg-canvas">
      <header className="flex h-[calc(3.5rem+env(safe-area-inset-top))] shrink-0 items-center gap-2 bg-canvas pt-[env(safe-area-inset-top)] pr-3 pl-1 shadow-[inset_0_-1px_0_var(--color-line)] wide:pl-5">
        {wide ? null : <NavButton />}
        <h1 className="min-w-0 flex-1 truncate text-card-title text-ink">{room?.title ?? ""}</h1>
        {room ? <Faces bots={room.bots} /> : null}
        {room && anyWorking(room.bots) ? (
          <Btn kind="secondary" size="sm" onClick={() => void ui.stopRoom({ roomId }).catch(() => undefined)}>
            <Square size={12} />
            {t("Stop all")}
          </Btn>
        ) : null}
        <button
          type="button"
          onClick={togglePanel}
          aria-label={panel ? t("Hide who is in this room") : t("Show who is in this room")}
          title={panel ? t("Hide who is in this room") : t("Show who is in this room")}
          aria-pressed={panel}
          data-tab-on={panel || undefined}
          className={btnClass("ghost", panel ? "bg-well text-ink" : "", "sm", true)}
        >
          <Users size={16} />
        </button>
      </header>
      <div className="flex min-h-0 flex-1">
        <div className={`flex min-w-0 flex-1 flex-col ${showPanel && !wide ? "hidden" : ""}`}>
          <Thread messages={messages} earlier={earlier} bots={room?.bots ?? []} refs={refs} />
          {room ? <Working bots={room.bots} /> : null}
          {room ? <RoomComposer room={room} refs={refs} /> : null}
        </div>
        {showPanel ? <RoomPanel room={room} onRoom={setRoom} className={wide ? "w-[300px] shadow-[inset_1px_0_0_var(--color-line)]" : "flex-1"} /> : null}
      </div>
    </div>
  );
}

// The wide panel's last state, per browser: a convenience, so storage that
// is off or full only means it opens again.
const panelKey = "silo.roomPanel";
function readPanel(): boolean {
  try {
    return localStorage.getItem(panelKey) !== "0";
  } catch {
    return true;
  }
}
function writePanel(on: boolean) {
  try {
    localStorage.setItem(panelKey, on ? "1" : "0");
  } catch {
    // Storage off: the panel opens next time.
  }
}

// Faces are the room's Bots as small crests, each lit while it works.
function Faces({ bots }: { bots: RoomBot[] }) {
  return (
    <span className="flex shrink-0 items-center -space-x-1.5 max-wide:hidden" aria-hidden>
      {bots.map((b) => (
        <span key={b.botId} className="relative rounded-full ring-2 ring-canvas" title={b.name}>
          <Crest index={b.crest} size={24} state={b.working ? "working" : "idle"} />
        </span>
      ))}
    </span>
  );
}

function Thread({ messages, earlier, bots, refs }: { messages: RoomMessage[]; earlier: boolean; bots: RoomBot[]; refs: RoomBotRef[] }) {
  const box = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const crests = useMemo(() => new Map(bots.map((b) => [b.botId, b])), [bots]);
  useLayoutEffect(() => {
    const el = box.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [messages.length]);
  return (
    <div
      ref={box}
      onScroll={(e) => {
        const el = e.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
      }}
      className="min-h-0 flex-1 overflow-auto"
    >
      <div className="mx-auto flex w-full max-w-[760px] flex-col gap-4 px-4 py-6">
        {earlier ? <p className="text-center text-meta text-ink-3">{t("Older messages are not shown.")}</p> : null}
        {messages.length === 0 ? <Empty bots={bots} /> : null}
        {messages.map((m, i) => (
          <Message key={m.id} m={m} bot={crests.get(m.botId)} refs={refs} same={sameRun(messages[i - 1], m)} />
        ))}
      </div>
    </div>
  );
}

// sameRun: the writer goes on from their previous message, so the second is
// not headed again.
function sameRun(prev: RoomMessage | undefined, m: RoomMessage) {
  if (!prev || prev.kind !== "message" || m.kind !== "message") return false;
  return prev.userId === m.userId && prev.botId === m.botId && Date.parse(m.createdAt) - Date.parse(prev.createdAt) < 5 * 60_000;
}

function Empty({ bots }: { bots: RoomBot[] }) {
  return (
    <div className="rise flex flex-col items-center gap-3 py-16 text-center">
      <span className="flex -space-x-3">
        {bots.slice(0, 4).map((b) => (
          <span key={b.botId} className="rounded-full ring-4 ring-canvas">
            <Crest index={b.crest} size={56} />
          </span>
        ))}
      </span>
      <p className="max-w-[420px] text-body text-ink-2">
        {t("Every Bot here hears what you write. Name one with @ to have it answer; the others speak up only when they have something to add.")}
      </p>
    </div>
  );
}

function when(iso: string) {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" });
}

function Message({ m, bot, refs, same }: { m: RoomMessage; bot: RoomBot | undefined; refs: RoomBotRef[]; same: boolean }) {
  if (m.kind === "notice") {
    return <p className="rise text-center text-meta leading-[18px] text-ink-3">{m.body}</p>;
  }
  if (m.botId) {
    return (
      <div className="rise flex gap-3">
        <span className="w-7 shrink-0 pt-0.5">{same ? null : <Crest index={bot?.crest ?? 0} size={28} />}</span>
        <div className="min-w-0 flex-1">
          {same ? null : (
            <div className="mb-1 flex items-baseline gap-2">
              <span className="text-row font-medium text-ink">{m.author}</span>
              <span className="text-micro text-ink-3 tabular-nums">{when(m.createdAt)}</span>
            </div>
          )}
          <div className="silo-reply">
            <Md text={m.body} />
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className={`rise flex flex-col ${m.mine ? "items-end" : "items-start"}`}>
      {same ? null : (
        <span className="mb-1 flex items-baseline gap-2 px-1">
          {m.mine ? null : <span className="text-meta font-medium text-ink-2">{m.author}</span>}
          <span className="text-micro text-ink-3 tabular-nums">{when(m.createdAt)}</span>
        </span>
      )}
      <div className={`max-w-[88%] rounded-bubble bg-well px-4 py-2.5 text-body-lg leading-6 whitespace-pre-wrap text-ink sm:max-w-[80%] ${m.mine ? "rounded-br-[6px]" : "rounded-bl-[6px]"}`}>
        {parts(m.body, refs).map((p, i) =>
          p.mention ? (
            <span key={i} className="font-medium text-cobalt">
              {p.text}
            </span>
          ) : (
            <span key={i}>{p.text}</span>
          ),
        )}
      </div>
    </div>
  );
}

// Working says which Bots are at it now, and on what; one that waits on an
// approval this person may answer says so and leads to it.
function Working({ bots }: { bots: RoomBot[] }) {
  const busy = bots.filter((b) => b.working);
  if (busy.length === 0) return null;
  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-1 px-4 pb-2">
      {busy.map((b) => (
        <div key={b.botId} className="flex min-w-0 items-center gap-2 text-meta leading-[18px]">
          <Lamp status={b.needsYou ? "needs_you" : "working"} />
          <span className="font-medium text-ink">{b.name}</span>
          <span className={`min-w-0 truncate ${b.needsYou ? "text-vermilion" : "text-ink-3"}`}>
            {b.needsYou ? t("waits for your approval") : b.step ? t("is using {{tool}}", { tool: b.step }) : t("is thinking…")}
          </span>
          {b.chatId ? (
            <Link to="/bots/$botId/run/$chatId" params={{ botId: b.botId, chatId: b.chatId }} className="shrink-0 text-cobalt hover:text-cobalt-deep">
              {t("See its work")}
            </Link>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function RoomComposer({ room, refs }: { room: Room; refs: RoomBotRef[] }) {
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const area = useRef<HTMLTextAreaElement>(null);
  async function send(e?: FormEvent) {
    e?.preventDefault();
    const text = draft.trim();
    if (!text || busy) return;
    setBusy(true);
    setErr("");
    try {
      await ui.postRoomMessage({ roomId: room.id, text });
      setDraft("");
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
      area.current?.focus();
    }
  }
  function key(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  }
  const canSend = draft.trim() !== "" && !busy;
  return (
    <form onSubmit={send} className="mx-auto w-full max-w-[760px] px-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] wide:px-4">
      {err ? (
        <p role="alert" className="mb-2 px-1 text-small text-vermilion">
          {err}
        </p>
      ) : null}
      <div className="rounded-[18px] bg-surface px-3 pt-2.5 pb-2 shadow-float transition-shadow duration-[160ms] ease-quiet focus-within:shadow-float-focus">
        <textarea
          ref={area}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={key}
          rows={1}
          placeholder={t("Message the room…")}
          aria-label={t("Message the room")}
          className="block max-h-[168px] min-h-[26px] w-full resize-none bg-transparent px-1 text-body-lg leading-[26px] text-ink outline-none [field-sizing:content] placeholder:text-ink-3"
        />
        <div className="mt-2 flex items-center gap-1.5">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
            {refs.map((b) => (
              <button
                key={b.id}
                type="button"
                onClick={() => {
                  setDraft((d) => mention(d, b.name, refs));
                  area.current?.focus();
                }}
                title={t("Ask {{name}}", { name: b.name })}
                className={btnClass("ghost", "h-7 px-2 text-meta", "sm")}
              >
                @{b.name}
              </button>
            ))}
          </div>
          <button
            type="submit"
            disabled={!canSend}
            title={t("Enter to send")}
            aria-label={t("Send message")}
            className={`grid h-[34px] w-[34px] shrink-0 place-items-center rounded-[11px] transition-[background-color,transform] duration-[240ms] ease-quiet active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:active:scale-100 ${canSend ? "bg-ink hover:bg-ink-deep" : "bg-pressed"}`}
          >
            <ArrowUp size={17} strokeWidth={2.25} className={canSend ? "text-on-ink" : "text-ink-3"} />
          </button>
        </div>
      </div>
      {room.replyMode === "mentions" ? <p className="mt-1.5 px-1 text-label text-ink-3">{t("Only the Bots you @mention answer in this room.")}</p> : null}
    </form>
  );
}

// RoomPanel is who is in the room and what may be changed about it: its name,
// who answers, the Bots and people in it.
function RoomPanel({ room, onRoom, className }: { room: Room; onRoom: (r: Room) => void; className: string }) {
  const nav = useNavigate();
  const { bots } = useBots();
  const people = useQuery(UI.method.listSpacePeople, { spaceId: room.spaceId }).data?.people ?? [];
  const me = people.find((p) => p.you)?.userId ?? room.people.find((p) => p.me)?.userId ?? "";
  const [title, setTitle] = useState(room.title);
  const [err, setErr] = useState("");
  const [armed, setArmed] = useState(false);
  useEffect(() => setTitle(room.title), [room.title]);

  const inRoom = new Set(room.bots.map((b) => b.botId));
  const roomPeople = room.people.map((p) => p.userId);
  const mine: BotPeople[] = (bots ?? []).map((b) => ({ id: b.id, people: b.people.map((p) => p.userId) }));
  const names = Object.fromEntries((bots ?? []).map((b) => [b.id, b.name]));
  // A Bot may come in when its manager adds it and everyone here is on it.
  const addBots = (bots ?? [])
    .filter((b) => b.spaceId === room.spaceId && b.role === "manage" && !inRoom.has(b.id))
    .map((b) => {
      const off = roomPeople.find((u) => !(b.people.length === 0 ? u === me : b.people.some((p) => p.userId === u)));
      return { bot: b, why: off ? t("{{who}} is not on it", { who: room.people.find((p) => p.userId === off)?.email ?? "" }) : "" };
    });
  const roomBots = mine.filter((b) => inRoom.has(b.id));
  const addPeople = eligible(
    people.filter((p) => !roomPeople.includes(p.userId)).map((p) => p.userId),
    roomBots,
    me,
    names,
  ).map((e) => ({ person: people.find((p) => p.userId === e.userId)!, why: e.why ? t("Not on {{bot}}", { bot: e.why }) : "" }));

  async function act(f: () => Promise<Room | undefined>) {
    setErr("");
    try {
      const r = await f();
      if (r) onRoom(r);
      void reload(UI.method.listRooms);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  async function rename() {
    const v = title.trim();
    if (!v || v === room.title) return setTitle(room.title);
    await act(() => ui.updateRoom({ roomId: room.id, title: v }));
  }
  async function leave() {
    try {
      await ui.removeRoomPerson({ roomId: room.id, userId: me });
      void reload(UI.method.listRooms);
      void nav({ to: "/" });
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  async function remove() {
    if (!armed) return setArmed(true);
    try {
      await ui.deleteRoom({ roomId: room.id });
      void reload(UI.method.listRooms);
      void nav({ to: "/" });
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  const head = "mb-2 text-section text-ink-3";
  return (
    <aside className={`min-h-0 overflow-auto bg-well px-4 py-5 ${className}`} aria-label={t("Who is in this room")}>
      <label htmlFor="room-title" className={head + " block"}>
        {t("Name")}
      </label>
      <input
        id="room-title"
        className={`${inputClass} mb-5`}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onBlur={() => void rename()}
        onKeyDown={(e) => e.key === "Enter" && (e.currentTarget as HTMLInputElement).blur()}
      />
      <h2 className={head}>{t("Who answers")}</h2>
      <ChipRadio
        className="mb-1 w-full"
        chip="flex-1 px-2"
        size="sm"
        label={t("Who answers")}
        value={room.replyMode}
        onChange={(m) => void act(() => ui.updateRoom({ roomId: room.id, replyMode: m }))}
        options={[
          { value: "decide", label: t("Every Bot decides") },
          { value: "mentions", label: t("Only who is named") },
        ]}
      />
      <p className="mb-5 text-label leading-4 text-ink-3">
        {room.replyMode === "mentions"
          ? t("A message wakes only the Bots it @mentions.")
          : t("Every Bot hears each message, one after another, and answers only when it has something to add.")}
      </p>

      <h2 className={head}>{t("Bots")}</h2>
      <ul className="mb-2 space-y-1">
        {room.bots.map((b) => (
          <li key={b.botId} className="flex min-w-0 items-center gap-2 rounded-control bg-surface px-2 py-1.5 shadow-card">
            <Crest index={b.crest} size={24} state={b.working ? "working" : "idle"} />
            {b.chatId ? (
              <Link to="/bots/$botId/run/$chatId" params={{ botId: b.botId, chatId: b.chatId }} className="min-w-0 flex-1 truncate text-row font-medium text-ink hover:text-cobalt" title={t("Open its seat: the work behind its messages")}>
                {b.name}
              </Link>
            ) : (
              <span className="min-w-0 flex-1 truncate text-row font-medium text-ink">{b.name}</span>
            )}
            <button type="button" onClick={() => void act(() => ui.removeRoomBot({ roomId: room.id, botId: b.botId }))} aria-label={t("Take {{name}} out", { name: b.name })} title={t("Take {{name}} out", { name: b.name })} className={btnClass("ghost", "h-6 w-6", "sm", true)}>
              <X size={13} />
            </button>
          </li>
        ))}
      </ul>
      <Candidates
        items={addBots.map(({ bot, why }) => ({ key: bot.id, label: bot.name, why, onAdd: () => act(() => ui.addRoomBot({ roomId: room.id, botId: bot.id })) }))}
      />

      <h2 className={`${head} mt-5`}>{t("People")}</h2>
      <ul className="mb-2 space-y-1">
        {room.people.map((p) => (
          <li key={p.userId} className="flex min-w-0 items-center gap-2 rounded-control bg-surface px-2 py-1.5 shadow-card">
            <Initials email={p.email} size={24} />
            <span className="min-w-0 flex-1 truncate text-row text-ink">{p.me ? t("{{who}} (you)", { who: p.email }) : p.email}</span>
            {!p.me && room.canDelete ? (
              <button
                type="button"
                onClick={() => void act(async () => (await ui.removeRoomPerson({ roomId: room.id, userId: p.userId })).room)}
                aria-label={t("Take {{name}} out", { name: p.email })}
                title={t("Take {{name}} out", { name: p.email })}
                className={btnClass("ghost", "h-6 w-6", "sm", true)}
              >
                <X size={13} />
              </button>
            ) : null}
          </li>
        ))}
      </ul>
      <Candidates
        items={addPeople.map(({ person, why }) => ({ key: person.userId, label: person.email, why, onAdd: () => act(() => ui.addRoomPerson({ roomId: room.id, userId: person.userId })) }))}
      />

      {err ? (
        <p role="alert" className="mt-4 text-small text-vermilion">
          {err}
        </p>
      ) : null}
      <div className="mt-6 flex flex-wrap gap-2">
        <Btn kind="secondary" size="sm" onClick={() => void leave()}>
          {t("Leave the room")}
        </Btn>
        {room.canDelete ? (
          <Btn kind={armed ? "deny" : "ghost"} size="sm" onClick={() => void remove()} onBlur={() => setArmed(false)}>
            {armed ? t("Click again to delete") : t("Delete the room")}
          </Btn>
        ) : null}
      </div>
    </aside>
  );
}

// Candidates are what may be added, each a press away; one that may not says
// why and is struck through.
function Candidates({ items }: { items: { key: string; label: string; why: string; onAdd: () => Promise<void> }[] }) {
  if (items.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {items.map((c) => (
        <button
          key={c.key}
          type="button"
          aria-disabled={!!c.why || undefined}
          title={c.why || t("Add {{name}}", { name: c.label })}
          onClick={() => !c.why && void c.onAdd()}
          className={`flex h-7 max-w-full items-center gap-1 rounded-control px-2 text-meta font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
            c.why ? "cursor-default text-ink-3 line-through" : "text-ink-2 shadow-[inset_0_0_0_1px_var(--color-line-strong)] hover:bg-surface hover:text-ink"
          }`}
        >
          {c.why ? null : <Plus size={12} />}
          <span className="truncate">{c.label}</span>
        </button>
      ))}
    </div>
  );
}
