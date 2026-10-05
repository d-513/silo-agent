import { eq } from "../testing.ts";
import { adapterPoints, adapterTraits, filterAdapters } from "./model.ts";

const ad = (slug: string, extra: object = {}) =>
  ({ slug, name: slug[0].toUpperCase() + slug.slice(1), description: `${slug} adapter`, fields: [], actions: [], requiresTarget: false, ...extra }) as any;
const telegram = ad("telegram", {
  requiresTarget: true,
  fields: [{ key: "bot_token", secret: true, required: true }, { key: "reply_mode", secret: false }],
  actions: [{ key: "list_dialogs", kind: "pick" }],
});
const whatsapp = ad("whatsapp", { requiresTarget: true, actions: [{ key: "link", kind: "qr" }] });
const plain = ad("plain");

eq(adapterTraits(telegram), ["Bot token", "One chat"], "token adapters name the token and the single chat");
eq(adapterTraits(whatsapp), ["QR login", "One chat"], "a QR action wins over a token");
eq(adapterTraits(plain), [], "nothing to say");

eq(adapterPoints("telegram").length > 0, true, "known adapters have selling points");
eq(adapterPoints("nope"), [], "unknown adapters have none");

const all = [telegram, whatsapp, plain];
eq(filterAdapters(all, "").length, 3, "no search keeps all");
eq(filterAdapters(all, "WHATS").map((a) => a.slug), ["whatsapp"], "name, case-insensitive");
eq(filterAdapters(all, "adapter").length, 3, "description");
eq(filterAdapters(all, "zzz"), [], "no match");
