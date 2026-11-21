// What a room page works out with no React in it: who may join, how a
// message's mentions are drawn, and what the composer's mention chips type.
// The server decides mentions on its own (internal/app/room/text.go); this
// mirrors it only to draw them.

/** A Bot as the room page knows it. */
export type RoomBotRef = { id: string; name: string };

/** A Bot the caller is on, with who else is on it ([] when it is only theirs). */
export type BotPeople = { id: string; people: string[] };

/** Whether the person is on the Bot: a Bot that lists nobody is the caller's alone. */
export function onBot(bot: BotPeople, userId: string, me: string): boolean {
  return bot.people.length === 0 ? userId === me : bot.people.includes(userId);
}

/**
 * Who of `candidates` may be in a room with these Bots: everyone in a room is
 * on every Bot in it. The answer keeps the order given; `why` is the first
 * Bot each one is not on.
 */
export function eligible(candidates: string[], bots: BotPeople[], me: string, names: Record<string, string>): { userId: string; why: string }[] {
  return candidates.map((userId) => {
    const off = bots.find((b) => !onBot(b, userId, me));
    return { userId, why: off ? names[off.id] ?? "" : "" };
  });
}

const word = /[\p{L}\p{N}_]/u;
const everyone = ["all", "everyone", "bots"];

/** A piece of a message: plain text, or a mention of a Bot (or of them all). */
export type Part = { text: string; mention?: string };

/**
 * parts splits a message into plain text and mentions, for drawing: @Name
 * (any case, the name whole, the longest name first) and @all.
 */
export function parts(text: string, bots: RoomBotRef[]): Part[] {
  const byLen = [...bots].sort((a, b) => b.name.length - a.name.length);
  const out: Part[] = [];
  let plain = "";
  let i = 0;
  while (i < text.length) {
    const before = i > 0 ? text[i - 1] : "";
    if (text[i] === "@" && !word.test(before)) {
      const rest = text.slice(i + 1);
      const hit = byLen.find((b) => b.name && whole(rest, b.name)) ?? undefined;
      const all = hit ? undefined : everyone.find((w) => whole(rest, w));
      const len = hit ? hit.name.length : all ? all.length : 0;
      if (len) {
        if (plain) out.push({ text: plain });
        plain = "";
        out.push({ text: text.slice(i, i + 1 + len), mention: hit ? hit.id : "all" });
        i += 1 + len;
        continue;
      }
    }
    plain += text[i];
    i++;
  }
  if (plain) out.push({ text: plain });
  return out;
}

function whole(s: string, name: string): boolean {
  if (s.length < name.length || s.slice(0, name.length).toLowerCase() !== name.toLowerCase()) return false;
  const next = s[name.length];
  return next === undefined || !word.test(next);
}

/**
 * mention puts @Name into a draft: at the end, with a space before it when
 * the draft does not end in one, and one after it. A Bot already named stays
 * named once.
 */
export function mention(draft: string, name: string, bots: RoomBotRef[]): string {
  const id = bots.find((b) => b.name === name)?.id;
  if (id && parts(draft, bots).some((p) => p.mention === id)) return draft;
  const sep = draft === "" || /\s$/.test(draft) ? "" : " ";
  return `${draft}${sep}@${name} `;
}

const PASS = "<pass/>";

/** isPass: a Bot's reply in its room seat that says it has nothing to add (internal/app/room Pass). */
export function isPass(text: string): boolean {
  const s = text.trim();
  return s !== "" && s.replaceAll(PASS, "").trim() === "";
}

/** passSoFar: a reply still being written that is, so far, a pass. */
export function passSoFar(text: string): boolean {
  const s = text.trim();
  return isPass(s) || (s.length >= 2 && PASS.startsWith(s));
}

/** Whether any Bot in the room is at work. */
export function anyWorking(bots: { working: boolean }[]): boolean {
  return bots.some((b) => b.working);
}
