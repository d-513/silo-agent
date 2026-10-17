// Reading a .pptx far enough to draw its slides: positions, text runs,
// pictures and tables, in slide units (EMU). What has no position of its own,
// nor in the slide's layout or master, is left unplaced for the outline.
import type JSZip from "jszip";

export type Run = { text: string; size?: number; bold?: boolean; italic?: boolean; color?: string };
export type Para = { runs: Run[]; align?: "left" | "center" | "right"; level: number; bullet: boolean };
export type Box = { x: number; y: number; w: number; h: number };
export type Shape = {
  kind: "text" | "image" | "table" | "chart";
  box?: Box;
  paras?: Para[];
  image?: string; // a path inside the zip
  rows?: string[][];
  fill?: string;
  title?: boolean;
};
export type Slide = { shapes: Shape[]; bg?: string; notes?: string };
export type Deck = { cx: number; cy: number; slides: Slide[] };

const parse = (xml: string) => new DOMParser().parseFromString(xml, "application/xml");
const kids = (el: Element | null | undefined, name: string) => (el ? Array.from(el.children).filter((c) => c.nodeName === name) : []);
const kid = (el: Element | null | undefined, name: string) => kids(el, name)[0];
const num = (v: string | null | undefined) => (v == null || v === "" ? undefined : Number(v));

function dirOf(path: string) {
  return path.slice(0, path.lastIndexOf("/") + 1);
}

// resolve a relationship target against the part that names it.
export function resolve(from: string, target: string): string {
  if (target.startsWith("/")) return target.slice(1);
  const parts = (dirOf(from) + target).split("/");
  const out: string[] = [];
  for (const p of parts) {
    if (p === "..") out.pop();
    else if (p && p !== ".") out.push(p);
  }
  return out.join("/");
}

async function rels(zip: JSZip, part: string): Promise<Record<string, { target: string; type: string }>> {
  const relPath = `${dirOf(part)}_rels/${part.slice(part.lastIndexOf("/") + 1)}.rels`;
  const raw = await zip.file(relPath)?.async("string");
  const out: Record<string, { target: string; type: string }> = {};
  if (!raw) return out;
  for (const r of Array.from(parse(raw).getElementsByTagName("Relationship"))) {
    out[r.getAttribute("Id") ?? ""] = { target: resolve(part, r.getAttribute("Target") ?? ""), type: r.getAttribute("Type") ?? "" };
  }
  return out;
}

function xfrm(el: Element | undefined): Box | undefined {
  if (!el) return undefined;
  const off = kid(el, "a:off");
  const ext = kid(el, "a:ext");
  const x = num(off?.getAttribute("x"));
  const y = num(off?.getAttribute("y"));
  const w = num(ext?.getAttribute("cx"));
  const h = num(ext?.getAttribute("cy"));
  if (x == null || y == null || w == null || h == null) return undefined;
  return { x, y, w, h };
}

function srgb(el: Element | undefined): string | undefined {
  const c = kid(kid(el, "a:solidFill"), "a:srgbClr")?.getAttribute("val");
  return c && /^[0-9a-f]{6}$/i.test(c) ? `#${c}` : undefined;
}

function paras(body: Element | undefined, title: boolean, placeholder: boolean): Para[] {
  return kids(body, "a:p").map((p) => {
    const pPr = kid(p, "a:pPr");
    const runs: Run[] = [];
    for (const r of Array.from(p.children)) {
      if (r.nodeName === "a:br") runs.push({ text: "\n" });
      if (r.nodeName !== "a:r" && r.nodeName !== "a:fld") continue;
      const rPr = kid(r, "a:rPr");
      const sz = num(rPr?.getAttribute("sz"));
      runs.push({
        text: kid(r, "a:t")?.textContent ?? "",
        size: sz ? sz / 100 : undefined,
        bold: rPr?.getAttribute("b") === "1",
        italic: rPr?.getAttribute("i") === "1",
        color: srgb(rPr),
      });
    }
    const algn = pPr?.getAttribute("algn");
    const noBullet = !!kid(pPr, "a:buNone");
    return {
      runs,
      align: algn === "ctr" ? "center" : algn === "r" ? "right" : undefined,
      level: num(pPr?.getAttribute("lvl")) ?? 0,
      bullet: !title && !noBullet && (placeholder || !!kid(pPr, "a:buChar") || !!kid(pPr, "a:buAutoNum")),
    };
  });
}

type Ph = { type: string; idx: string };
function phOf(sp: Element): Ph | undefined {
  const nv = kid(sp, "p:nvSpPr") ?? kid(sp, "p:nvPicPr") ?? kid(sp, "p:nvGraphicFramePr");
  const ph = kid(kid(nv, "p:nvPr"), "p:ph");
  if (!ph) return undefined;
  return { type: ph.getAttribute("type") ?? "body", idx: ph.getAttribute("idx") ?? "" };
}

// Where a placeholder sits by its layout or master: the shape with the same
// index, else the same type.
function placeholderBoxes(doc: Document | undefined): { byIdx: Record<string, Box>; byType: Record<string, Box> } {
  const out = { byIdx: {} as Record<string, Box>, byType: {} as Record<string, Box> };
  if (!doc) return out;
  for (const sp of Array.from(doc.getElementsByTagName("p:sp"))) {
    const ph = phOf(sp);
    const box = xfrm(kid(kid(sp, "p:spPr"), "a:xfrm"));
    if (!ph || !box) continue;
    if (ph.idx) out.byIdx[ph.idx] ??= box;
    out.byType[ph.type] ??= box;
  }
  return out;
}

type Frame = { off: [number, number]; chOff: [number, number]; scale: [number, number] };
const IDENT: Frame = { off: [0, 0], chOff: [0, 0], scale: [1, 1] };
const place = (f: Frame, b: Box): Box => ({
  x: f.off[0] + (b.x - f.chOff[0]) * f.scale[0],
  y: f.off[1] + (b.y - f.chOff[1]) * f.scale[1],
  w: b.w * f.scale[0],
  h: b.h * f.scale[1],
});

export async function readDeck(zip: JSZip): Promise<Deck> {
  const presPath = "ppt/presentation.xml";
  const pres = parse((await zip.file(presPath)?.async("string")) ?? "");
  const sz = pres.getElementsByTagName("p:sldSz")[0];
  const cx = num(sz?.getAttribute("cx")) ?? 12192000;
  const cy = num(sz?.getAttribute("cy")) ?? 6858000;
  const presRels = await rels(zip, presPath);
  const order = Array.from(pres.getElementsByTagName("p:sldId")).map((s) => presRels[s.getAttribute("r:id") ?? ""]?.target).filter(Boolean) as string[];
  const slides: Slide[] = [];
  for (const path of order) {
    const raw = await zip.file(path)?.async("string");
    if (!raw) continue;
    const doc = parse(raw);
    const r = await rels(zip, path);
    const layoutPath = Object.values(r).find((x) => x.type.endsWith("/slideLayout"))?.target;
    const layoutDoc = layoutPath ? parse((await zip.file(layoutPath)?.async("string")) ?? "") : undefined;
    const masterPath = layoutPath ? Object.values(await rels(zip, layoutPath)).find((x) => x.type.endsWith("/slideMaster"))?.target : undefined;
    const masterDoc = masterPath ? parse((await zip.file(masterPath)?.async("string")) ?? "") : undefined;
    const layout = placeholderBoxes(layoutDoc);
    const master = placeholderBoxes(masterDoc);
    const inherit = (ph: Ph | undefined) =>
      ph ? (layout.byIdx[ph.idx] ?? layout.byType[ph.type] ?? master.byIdx[ph.idx] ?? master.byType[ph.type] ?? (ph.type === "ctrTitle" ? layout.byType.title ?? master.byType.title : undefined)) : undefined;

    const shapes: Shape[] = [];
    const walk = (tree: Element | undefined, frame: Frame) => {
      for (const el of Array.from(tree?.children ?? [])) {
        if (el.nodeName === "p:grpSp") {
          const gx = kid(kid(el, "p:grpSpPr"), "a:xfrm");
          const box = xfrm(gx);
          const chOff = kid(gx, "a:chOff");
          const chExt = kid(gx, "a:chExt");
          if (box && chOff && chExt) {
            const outer = place(frame, box);
            const cw = num(chExt.getAttribute("cx")) || box.w;
            const ch = num(chExt.getAttribute("cy")) || box.h;
            walk(el, { off: [outer.x, outer.y], chOff: [num(chOff.getAttribute("x")) ?? 0, num(chOff.getAttribute("y")) ?? 0], scale: [outer.w / cw, outer.h / ch] });
          } else walk(el, frame);
          continue;
        }
        const ph = phOf(el);
        const own = (b: Box | undefined) => (b ? place(frame, b) : (() => {
          const inh = inherit(ph);
          return inh ? place(IDENT, inh) : undefined;
        })());
        if (el.nodeName === "p:sp") {
          const spPr = kid(el, "p:spPr");
          const title = ph?.type === "title" || ph?.type === "ctrTitle";
          const ps = paras(kid(el, "p:txBody"), title, !!ph && !title && ph.type !== "subTitle");
          const fill = srgb(spPr);
          if (!ps.some((p) => p.runs.some((x) => x.text.trim())) && !fill) continue;
          shapes.push({ kind: "text", box: own(xfrm(kid(spPr, "a:xfrm"))), paras: ps, fill, title });
        } else if (el.nodeName === "p:pic") {
          const embed = kid(kid(el, "p:blipFill"), "a:blip")?.getAttribute("r:embed");
          const target = embed ? r[embed]?.target : undefined;
          if (target) shapes.push({ kind: "image", box: own(xfrm(kid(kid(el, "p:spPr"), "a:xfrm"))), image: target });
        } else if (el.nodeName === "p:graphicFrame") {
          const box = own(xfrm(kid(el, "p:xfrm")));
          const tbl = el.getElementsByTagName("a:tbl")[0];
          if (tbl) {
            const rows = kids(tbl, "a:tr").map((tr) => kids(tr, "a:tc").map((tc) => tc.textContent?.trim() ?? ""));
            shapes.push({ kind: "table", box, rows });
          } else if ((el.getElementsByTagName("a:graphicData")[0]?.getAttribute("uri") ?? "").includes("chart")) {
            shapes.push({ kind: "chart", box });
          }
        }
      }
    };
    walk(doc.getElementsByTagName("p:spTree")[0], IDENT);
    const bgPr = doc.getElementsByTagName("p:bgPr")[0];
    slides.push({ shapes, bg: srgb(bgPr) });
  }
  return { cx, cy, slides };
}
