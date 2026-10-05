import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { COLOR_COUNT, Crest, CrestPicker, packCrest, SHAPE_COUNT } from "./Crest";
import { fail } from "./errors";
import { UI } from "./gen/silo/v1/ui_pb";
import { reload } from "./query";
import { inputClass, textareaClass } from "./Field";

function randomCrest() {
  return packCrest(Math.floor(Math.random() * SHAPE_COUNT), Math.floor(Math.random() * COLOR_COUNT));
}

export function NewBotPage() {
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [crest, setCrest] = useState(randomCrest);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  async function create(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const b = await ui.createBot({ name: name.trim(), crest, description: description.trim() });
      void reload(UI.method.listBots);
      nav(`/bots/${b.id}/run`);
    } catch (ex) {
      setErr(fail(ex));
      setBusy(false);
    }
  }
  return (
    <div className="silo-page silo-page-sm rise">
      <h1 className="mb-6 text-title">New Bot</h1>
      <form onSubmit={create}>
        <div className="mb-5 flex flex-col items-center gap-4">
          <Crest index={crest} size={88} />
          <span className="text-[12px] text-ink-3">Pick a crest for this machine</span>
        </div>
        <div className="mb-6">
          <CrestPicker value={crest} onChange={setCrest} />
        </div>
        <label htmlFor="bot-name" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Name
        </label>
        <input
          id="bot-name"
          className={`${inputClass} mb-4`}
          placeholder="Scout"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
        />
        <label htmlFor="bot-desc" className="mb-1.5 block text-[12px] font-medium text-ink-3">
          Description
        </label>
        <textarea
          id="bot-desc"
          className={`${textareaClass} mb-3 min-h-[72px]`}
          placeholder="What this machine is for"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <p className="mb-6 text-[13px] text-ink-2">A Bot is its own machine. It does not share files with the others.</p>
        {err && (
          <p role="alert" className="mb-3 text-[13px] text-vermilion">
            {err}
          </p>
        )}
        <div className="flex gap-2">
          <Btn kind="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Creating…" : "Create Bot"}
          </Btn>
          <Link to="/" className={btnClass("secondary")}>
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}
