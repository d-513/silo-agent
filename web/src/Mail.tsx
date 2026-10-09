import { ChevronRight, Download, Mail as MailIcon, MessageCircle, Paperclip, RefreshCw, ShieldAlert, ShieldCheck, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "./api";
import { useAuth } from "./auth";
import { btnClass } from "./Btn";
import { fail } from "./errors";
import { ArmedButton, CopyButton, SaveButton, useSave } from "./Feedback";
import { ErrorWell, Field, Panel, SkeletonRows, textareaClass } from "./Field";
import { feedStamp, fmtBytes } from "./format";
import { UI, type Bot, type Mail, type Mailbox } from "./gen/silo/v1/ui_pb";
import { chatLink } from "./links";
import { mailNotice, rawMailHref, senderName, wakeListError } from "./mailbox";
import { patch, reloadBot } from "./query";
import { ToggleRow } from "./Switch";

// MailPane is the Bot's receive-only mailbox: its address, what arrived, and
// whether mail from chosen senders starts a chat. The Bot reads the same
// messages with list_mail and read_mail; nothing can be sent from here.
export function MailPane({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const { admin } = useAuth();
  const botId = bot.id;
  // Mail arrives on its own, so the list looks again while the page is open.
  const q = useQuery(UI.method.listMail, { botId }, { refetchInterval: 10000 });
  const box = q.data?.mailbox ?? null;
  const rows = q.data?.messages ?? [];
  const [open, setOpen] = useState("");

  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);
  useEffect(() => setOpen(""), [botId]);

  const setBox = (m: Mailbox) => patch(UI.method.listMail, { botId }, (d) => ({ ...d, mailbox: m }));

  async function rotate() {
    onError("");
    try {
      setBox(await ui.rotateMailbox({ botId }));
      reloadBot(botId);
    } catch (e) {
      onError(fail(e));
    }
  }

  async function remove(id: string) {
    onError("");
    try {
      await ui.deleteMail({ botId, id });
      patch(UI.method.listMail, { botId }, (d) => ({ ...d, messages: d.messages.filter((m) => m.id !== id) }));
      if (open === id) setOpen("");
    } catch (e) {
      onError(fail(e));
    }
  }

  const notice = box ? mailNotice(box.state, admin) : null;

  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">Mail</h2>
      <p className="mb-5 mt-1 text-[13.5px] leading-[21px] text-ink-2">
        This Bot's own address. It only receives: give it to a sign-up, a newsletter or a report, and the Bot reads what arrives. Nothing can be sent from it.
      </p>
      {notice ? (
        <div className="mb-5 rounded-card bg-well px-5 py-4 text-[13.5px] leading-[21px] text-ink-2">
          {notice.text}{" "}
          {notice.admin ? (
            <Link to="/admin/settings/$section" params={{ section: "mail" }} className="font-medium text-cobalt hover:underline">
              Open settings
            </Link>
          ) : null}
        </div>
      ) : null}
      {box?.problem ? <ErrorWell className="mb-5">The mail listener is not running: {box.problem}</ErrorWell> : null}

      {box === null ? (
        <SkeletonRows rows={3} height={72} />
      ) : (
        <div className="grid gap-5">
          {box.address ? <AddressCard address={box.address} onRotate={() => void rotate()} /> : null}
          {box.address ? <WakePanel botId={botId} box={box} onSaved={setBox} /> : null}
          {rows.length === 0 ? (
            <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
              <MailIcon size={20} className="text-ink-3" />
              <p className="text-ink-2">No mail yet.</p>
              {box.address ? <p className="max-w-sm text-[12.5px] text-ink-3">Anything sent to the address above lands here, usually within a minute.</p> : null}
            </div>
          ) : (
            <section>
              <ul className="overflow-hidden rounded-card bg-surface shadow-card">
                {rows.map((m, i) => (
                  <MailRow key={m.id} botId={botId} mail={m} first={i === 0} open={open === m.id} onToggle={() => setOpen(open === m.id ? "" : m.id)} onDelete={() => void remove(m.id)} />
                ))}
              </ul>
              <p className="mt-2 px-1 text-[12.5px] text-ink-3">
                The newest {box.keep} messages are kept. A dot marks one the Bot has not read yet.
              </p>
            </section>
          )}
        </div>
      )}
    </div>
  );
}

function AddressCard({ address, onRotate }: { address: string; onRotate: () => void }) {
  return (
    <section className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-card bg-surface p-4 shadow-card">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
        <MailIcon size={18} />
      </div>
      <div className="min-w-0 flex-1 basis-60">
        <p className="truncate font-mono text-[13.5px] font-medium text-ink">{address}</p>
        <p className="mt-0.5 text-[12.5px] text-ink-2">Anyone who knows it can write to it. Adding +anything before the @ reaches the same mailbox.</p>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <CopyButton text={address} title="Copy address" />
        <ArmedButton kind="ghost" size="sm" iconOnly title="New address" armedLabel="Click again: the old address stops working" icon={<RefreshCw size={13} />} onConfirm={onRotate}>
          New address
        </ArmedButton>
      </div>
    </section>
  );
}

// WakePanel keeps only what the owner changed on top of the saved mailbox.
function WakePanel({ botId, box, onSaved }: { botId: string; box: Mailbox; onSaved: (m: Mailbox) => void }) {
  const [edits, setEdits] = useState<{ wake?: boolean; wakeFrom?: string }>({});
  const [err, setErr] = useState("");
  const saver = useSave();
  const wake = edits.wake ?? box.wake;
  const wakeFrom = edits.wakeFrom ?? box.wakeFrom;
  const dirty = wake !== box.wake || wakeFrom !== box.wakeFrom;
  useEffect(() => {
    setEdits({});
    setErr("");
  }, [botId]);

  async function save() {
    const bad = wakeListError(wake, wakeFrom);
    if (bad) {
      setErr(bad);
      return;
    }
    setErr("");
    try {
      await saver.run(async () => {
        onSaved(await ui.updateMailbox({ botId, wake, wakeFrom }));
        setEdits({});
      });
    } catch (e) {
      setErr(fail(e));
    }
  }

  return (
    <Panel>
      <ToggleRow
        label="Start a chat when mail arrives from someone I trust"
        hint="Off, mail waits in the inbox until the Bot is asked to look. On, a message from a sender below opens a new chat and the Bot acts on it."
        on={wake}
        onChange={(v) => {
          setEdits((e) => ({ ...e, wake: v }));
          setErr("");
        }}
      />
      {wake || wakeFrom ? (
        <Field
          className="mt-4"
          label="Senders"
          error={err}
          hint="One per line: an address (ada@example.com) or a whole domain (@example.com). Only mail the sender's domain vouches for counts, so a forged From address wakes nobody."
        >
          <textarea
            className={`${textareaClass} font-mono text-[13px]`}
            rows={3}
            spellCheck={false}
            placeholder="you@example.com"
            value={wakeFrom}
            aria-invalid={err ? true : undefined}
            onChange={(e) => {
              setEdits((x) => ({ ...x, wakeFrom: e.target.value }));
              setErr("");
            }}
          />
        </Field>
      ) : err ? (
        <p role="alert" className="mt-2 text-[12px] text-vermilion">
          {err}
        </p>
      ) : null}
      {dirty ? (
        <div className="mt-4 flex items-center gap-2">
          <SaveButton state={saver.state} onClick={() => void save()}>
            Save
          </SaveButton>
          <button type="button" className={btnClass("ghost")} onClick={() => (setEdits({}), setErr(""))}>
            Discard
          </button>
        </div>
      ) : null}
    </Panel>
  );
}

function MailRow({ botId, mail: m, first, open, onToggle, onDelete }: { botId: string; mail: Mail; first: boolean; open: boolean; onToggle: () => void; onDelete: () => void }) {
  return (
    <li className={first ? "" : "shadow-[inset_0_1px_0_var(--color-line)]"}>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-start gap-3 px-4 py-3 text-left transition-[background-color] duration-[160ms] ease-quiet hover:bg-well outline-offset-[-2px]"
      >
        <ChevronRight size={14} className={`mt-[3px] shrink-0 text-ink-3 transition-transform duration-200 ease-quiet ${open ? "rotate-90" : ""}`} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            {m.read ? null : <span aria-label="Not read by the Bot yet" title="Not read by the Bot yet" className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" />}
            <span className="truncate text-[13.5px] font-medium text-ink">{senderName(m.from, m.fromAddress)}</span>
            {m.verified ? null : (
              <span title="Sender not verified: the From address may be forged" className="flex shrink-0 items-center text-ink-3">
                <ShieldAlert size={13} aria-label="Sender not verified" />
              </span>
            )}
            {m.attachments.length > 0 ? <Paperclip size={12} className="shrink-0 text-ink-3" aria-label={`${m.attachments.length} attached`} /> : null}
            <span className="ml-auto shrink-0 font-mono text-[12px] text-ink-3">{feedStamp(m.receivedAt)}</span>
          </span>
          <span className="mt-0.5 block truncate text-[13.5px] text-ink">{m.subject || "(no subject)"}</span>
          {open ? null : <span className="mt-0.5 block truncate text-[12.5px] text-ink-3">{m.preview}</span>}
        </span>
      </button>
      {open ? <MailBody botId={botId} mail={m} onDelete={onDelete} /> : null}
    </li>
  );
}

function MailBody({ botId, mail: m, onDelete }: { botId: string; mail: Mail; onDelete: () => void }) {
  const q = useQuery(UI.method.getMail, { botId, id: m.id });
  const full = q.data ?? null;
  const Shield = m.verified ? ShieldCheck : ShieldAlert;
  return (
    <div className="px-4 pb-4 pl-[42px]">
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[12.5px] leading-[18px]">
        <dt className="text-ink-3">From</dt>
        <dd className="min-w-0 break-words text-ink-2">{m.from || "Unknown"}</dd>
        <dt className="text-ink-3">To</dt>
        <dd className="min-w-0 break-words text-ink-2">{m.to || "Not named"}</dd>
      </dl>
      <p className={`mt-2 flex items-start gap-1.5 text-[12.5px] leading-[18px] ${m.verified ? "text-ink-2" : "text-vermilion"}`} title={m.authDetail}>
        <Shield size={13} className={`mt-[2.5px] shrink-0 ${m.verified ? "text-emerald" : ""}`} />
        <span>{m.verified ? "Sender verified: their domain vouched for this message." : "Sender not verified: the From address may be forged."}</span>
      </p>
      {q.error ? (
        <ErrorWell className="mt-3">{fail(q.error)}</ErrorWell>
      ) : full === null ? (
        <SkeletonRows rows={1} height={64} className="mt-3" />
      ) : (
        <pre className="mt-3 max-h-[480px] overflow-auto whitespace-pre-wrap break-words rounded-sm bg-well px-3 py-2.5 font-sans text-[13.5px] leading-[21px] text-ink">
          {full.text || "(no text)"}
        </pre>
      )}
      {m.attachments.length > 0 ? (
        <ul className="mt-3 flex flex-wrap gap-1.5">
          {m.attachments.map((a) => (
            <li key={a.index} className="flex max-w-full items-center gap-1.5 rounded-sm bg-well px-2 py-1 text-[12.5px] text-ink-2">
              <Paperclip size={12} className="shrink-0 text-ink-3" />
              <span className="truncate">{a.name}</span>
              <span className="shrink-0 font-mono text-[11.5px] text-ink-3">{fmtBytes(a.size)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        {m.chatId ? (
          <Link {...chatLink(botId, m.chatId)} className={btnClass("secondary", "", "sm")}>
            <MessageCircle size={13} />
            <span>Open the chat it started</span>
          </Link>
        ) : null}
        <a href={rawMailHref(botId, m.id)} download className={btnClass("ghost", "", "sm")}>
          <Download size={13} />
          <span>{m.attachments.length > 0 ? "Download original with attachments" : "Download original"}</span>
        </a>
        <ArmedButton kind="ghost" size="sm" iconOnly title="Delete message" icon={<Trash2 size={13} />} onConfirm={onDelete}>
          Delete
        </ArmedButton>
      </div>
    </div>
  );
}
