import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import { i18n, storedLanguage, t } from "./i18n.ts";
import { botLanguages, flagOf, languageName, nativeName, uiLanguages } from "./languages.ts";
import de from "./locales/de.ts";
import en from "./locales/en.ts";
import pl from "./locales/pl.ts";
import { eq } from "./testing.ts";

// The catalogs are held to the source: every message the code names has a
// Polish and a German translation, with the same placeholders and tags, and a
// catalog holds nothing the code no longer says.
//
//   node --experimental-strip-types src/i18n_test.ts          the check
//   node --experimental-strip-types src/i18n_test.ts --dump   every message, with where it is first used
//   node --experimental-strip-types src/i18n_test.ts --source the messages alone, before translating

// key is what a catalog files the message under: the English text, or that
// text with "_<context>" where one English word has two meanings.
type Use = { key: string; text: string; file: string; line: number; counted: boolean; rich: boolean; given: string[] | null };

const root = path.dirname(new URL(import.meta.url).pathname);

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name !== "gen" && e.name !== "locales") sourceFiles(p, out);
    } else if (/\.tsx?$/.test(e.name) && !/_test\.ts$/.test(e.name) && !e.name.endsWith(".d.ts")) {
      out.push(p);
    }
  }
  return out;
}

// The literals an expression can come to: "a", c ? "a" : "b", x ?? msg("a").
function literals(n: ts.Node, out: ts.StringLiteralLike[] = []): ts.StringLiteralLike[] {
  if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) out.push(n);
  else if (ts.isConditionalExpression(n)) {
    literals(n.whenTrue, out);
    literals(n.whenFalse, out);
  } else if (ts.isParenthesizedExpression(n) || ts.isJsxExpression(n)) {
    if (n.expression) literals(n.expression, out);
  }
  return out;
}

// Every string given to `t` or `msg`, and every `i18nKey` of a <Trans>.
function messages(): Use[] {
  const uses: Use[] = [];
  for (const file of sourceFiles(root)) {
    const sf = ts.createSourceFile(file, fs.readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    // The names of the values a call hands over; null when they cannot be read off the source.
    const names = (o: ts.Node | undefined): string[] | null => {
      if (!o) return [];
      if (!ts.isObjectLiteralExpression(o) || o.properties.some((p) => !p.name)) return null;
      return o.properties.map((p) => p.name!.getText());
    };
    const add = (n: ts.StringLiteralLike, given: string[] | null, rich: boolean, context = "") =>
      uses.push({
        key: context ? `${n.text}_${context}` : n.text,
        text: n.text,
        file: path.relative(root, file),
        line: sf.getLineAndCharacterOfPosition(n.getStart()).line + 1,
        counted: !!given?.includes("count"),
        rich,
        given,
      });
    // t("Used", { context: "tool" }): the same English, filed apart.
    const contextOf = (o: ts.Node | undefined): string => {
      if (!o || !ts.isObjectLiteralExpression(o)) return "";
      const p = o.properties.find((x) => x.name?.getText() === "context");
      return p && ts.isPropertyAssignment(p) && ts.isStringLiteral(p.initializer) ? p.initializer.text : "";
    };
    const visit = (n: ts.Node) => {
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && (n.expression.text === "t" || n.expression.text === "msg") && n.arguments.length > 0) {
        // A message marked with msg gets its values where it is translated.
        const given = n.expression.text === "msg" ? null : names(n.arguments[1]);
        for (const lit of literals(n.arguments[0])) add(lit, given, false, contextOf(n.arguments[1]));
      } else if (ts.isJsxAttribute(n) && n.name.getText() === "i18nKey" && n.initializer) {
        const attr = (name: string) => n.parent.properties.find((p): p is ts.JsxAttribute => ts.isJsxAttribute(p) && p.name.getText() === name)?.initializer;
        const obj = (init: ts.JsxAttributeValue | undefined) => (init && ts.isJsxExpression(init) ? init.expression : undefined);
        const given = [...(names(obj(attr("values"))) ?? []), ...(names(obj(attr("components"))) ?? [])];
        for (const lit of literals(n.initializer)) add(lit, given, true);
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  }
  return uses;
}

const uses = messages();
const first = new Map<string, Use>();
const counted = new Set<string>();
const rich = new Set<string>();
// The English a catalog key stands for: itself, unless it carries a context.
const english = new Map<string, string>();
for (const u of uses) {
  english.set(u.key, u.text);
  if (!first.has(u.key)) first.set(u.key, u);
  if (u.counted) counted.add(u.key);
  if (u.rich) rich.add(u.key);
}

const problems: string[] = [];

if (process.argv.includes("--dump")) {
  for (const [key, u] of first) console.log(`${u.file}:${u.line}\t${counted.has(key) ? "#" : ""}\t${JSON.stringify(key)}`);
  process.exit(0);
}

const where = (key: string) => {
  const u = first.get(key);
  return u ? `${u.file}:${u.line}` : "";
};

// A message is written the way a translator can carry it over.
for (const key of first.keys()) {
  if (key !== key.trim()) problems.push(`${where(key)}: ${JSON.stringify(key)} starts or ends with a space; put the space in the markup`);
  if (!/\p{L}{2}/u.test(key)) problems.push(`${where(key)}: ${JSON.stringify(key)} has no words to translate`);
}

// The marks a translation must carry over unchanged: its {{values}}, and in a
// <Trans> message its <tags> too (anywhere else "<name>" is just text).
function marks(s: string, key: string): string {
  const pattern = rich.has(key) ? /\{\{\s*(\w+)\s*\}\}|<\/?(\w+)\s*\/?>/g : /\{\{\s*(\w+)\s*\}\}/g;
  return [...s.matchAll(pattern)]
    .map((m) => m[0].replace(/\s/g, ""))
    .sort()
    .join(" ");
}

// Every value and tag a message names is handed to it where it is used.
for (const u of uses) {
  if (!u.given) continue;
  for (const m of u.text.matchAll(u.rich ? /\{\{\s*(\w+)\s*\}\}|<(\w+)\s*\/?>/g : /\{\{\s*(\w+)\s*\}\}/g)) {
    const name = m[1] ?? m[2];
    if (!u.given.includes(name)) problems.push(`${u.file}:${u.line}: ${JSON.stringify(u.text)} is not given ${name}`);
  }
}

// The plural forms each language has (CLDR), as i18next suffixes them.
const forms: Record<string, string[]> = { en: ["one", "other"], de: ["one", "other"], pl: ["one", "few", "many", "other"] };

function check(lang: string, catalog: Record<string, string>, source: boolean) {
  const used = new Set<string>();
  for (const key of first.keys()) {
    const text = english.get(key)!;
    const plural = forms[lang].map((f) => `${key}_${f}`);
    const hasPlural = plural.some((k) => k in catalog);
    if (hasPlural) {
      if (!counted.has(key)) problems.push(`${lang}: ${JSON.stringify(key)} has plural forms but is never used with a count (${where(key)})`);
      for (const k of plural) {
        used.add(k);
        if (!(k in catalog)) problems.push(`${lang}: missing plural form ${JSON.stringify(k)}`);
        // A form may leave the number out ("every hour"), nothing else.
        else if (marks(catalog[k], key).replace("{{count}}", "").trim() !== marks(text, key).replace("{{count}}", "").trim())
          problems.push(`${lang}: ${JSON.stringify(k)} does not carry the same values and tags as its message`);
      }
      if (key in catalog) problems.push(`${lang}: ${JSON.stringify(key)} has plural forms and a plain entry; keep one`);
      continue;
    }
    // English is the key itself: only what is counted needs writing out. (A
    // key with a context falls back to its plain English too.)
    if (source) continue;
    used.add(key);
    if (!(key in catalog)) problems.push(`${lang}: no translation for ${JSON.stringify(key)} (${where(key)})`);
    else if (!catalog[key].trim()) problems.push(`${lang}: empty translation for ${JSON.stringify(key)}`);
    else if (marks(catalog[key], key) !== marks(text, key)) problems.push(`${lang}: ${JSON.stringify(key)} does not carry the same values and tags as its message`);
  }
  for (const k of Object.keys(catalog)) {
    if (!used.has(k)) problems.push(`${lang}: ${JSON.stringify(k)} is not a message the code uses`);
  }
}

check("en", en, true);
// --source checks the messages alone, before they are translated.
if (!process.argv.includes("--source")) {
  check("pl", pl, false);
  check("de", de, false);
}

if (problems.length) {
  for (const p of problems.slice(0, 80)) console.error(p);
  throw new Error(`${problems.length} i18n problems${problems.length > 80 ? " (first 80 shown)" : ""}`);
}

// English reads as written, counts pick their form, and a value is put in.
eq(t("Save"), "Save", "a message is its own English");
eq(t("{{count}} files", { count: 1 }), "1 file", "one file");
eq(t("{{count}} files", { count: 12 }), "12 files", "many files");

i18n.addResourceBundle("pl", "translation", pl);
i18n.addResourceBundle("de", "translation", de);
await i18n.changeLanguage("pl");
eq(
  [1, 2, 5, 22, 25].map((count) => t("{{count}} files", { count })),
  ["1 plik", "2 pliki", "5 plików", "22 pliki", "25 plików"],
  "Polish counts files in three forms",
);
await i18n.changeLanguage("de");
eq([1, 3].map((count) => t("{{count}} files", { count })), ["1 Datei", "3 Dateien"], "German counts files in two");
await i18n.changeLanguage("en");

// Only a language the console is translated into is read back from storage.
eq([storedLanguage("pl"), storedLanguage("de"), storedLanguage("en")], ["pl", "de", "en"], "stored languages");
eq([storedLanguage("fr"), storedLanguage(""), storedLanguage(null), storedLanguage("mixed")], [null, null, null, null], "anything else is no pick");

// Every language is named in its own words and in the reader's.
eq(uiLanguages.map(nativeName), ["English", "Polski", "Deutsch"], "display languages in their own words");
eq([languageName("de", "en"), languageName("de", "pl"), languageName("pl", "de")], ["German", "Niemiecki", "Polnisch"], "a language named for the reader");
for (const code of botLanguages) {
  if (languageName(code, "en") === code) throw new Error(`no name for Bot language ${code}`);
}

eq([flagOf("pl"), flagOf("pt-BR"), flagOf("mixed")], ["🇵🇱", "🇧🇷", "🌐"], "flags");
for (const code of botLanguages) if (flagOf(code) === flagOf("mixed")) throw new Error(`no flag for ${code}`);

console.log(`i18n ok: ${first.size} messages, pl ${Object.keys(pl).length}, de ${Object.keys(de).length}`);
