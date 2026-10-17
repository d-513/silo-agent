import { BarChart3 } from "lucide-react";
import { useEffect, useState } from "react";
import { t } from "../i18n.ts";
import { readDeck, type Deck, type Para, type Shape } from "./pptx";

const PAPER = "#ffffff";
const INK = "#1a1917";

// A slide deck drawn from its own parts, slide by slide. A shape the deck does
// not place (no position in it, its layout or its master) is listed under the
// slide instead of guessed at.
export default function Slides({ data }: { data: Uint8Array }) {
  const [deck, setDeck] = useState<Deck | null>(null);
  const [media, setMedia] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");
  useEffect(() => {
    let dead = false;
    const urls: string[] = [];
    (async () => {
      const { default: JSZip } = await import("jszip");
      const zip = await JSZip.loadAsync(data);
      const d = await readDeck(zip);
      const pics: Record<string, string> = {};
      for (const s of d.slides) {
        for (const sh of s.shapes) {
          if (sh.kind !== "image" || !sh.image || pics[sh.image]) continue;
          const bytes = await zip.file(sh.image)?.async("uint8array");
          if (!bytes) continue;
          const ext = sh.image.split(".").pop()?.toLowerCase();
          const type = ext === "png" ? "image/png" : ext === "gif" ? "image/gif" : ext === "svg" ? "" : "image/jpeg";
          // SVG can script: only raster pictures are drawn.
          if (!type) continue;
          const u = URL.createObjectURL(new Blob([bytes.slice()], { type }));
          urls.push(u);
          pics[sh.image] = u;
        }
      }
      if (!dead) {
        setDeck(d);
        setMedia(pics);
      }
    })().catch(() => !dead && setErr(t("This deck could not be read. Download it instead.")));
    return () => {
      dead = true;
      urls.forEach((u) => URL.revokeObjectURL(u));
    };
  }, [data]);
  if (err) return <p className="text-ink-2">{err}</p>;
  if (!deck) return <div className="skeleton aspect-video w-full rounded-sm" />;
  if (deck.slides.length === 0) return <p className="text-ink-2">{t("This deck has no slides.")}</p>;
  return (
    <div className="space-y-5">
      {deck.slides.map((s, i) => {
        const placed = s.shapes.filter((x) => x.box);
        const loose = s.shapes.filter((x) => !x.box);
        return (
          <figure key={i} className="space-y-1.5">
            <div
              className="@container relative w-full overflow-hidden rounded-sm shadow-card"
              style={{ aspectRatio: `${deck.cx} / ${deck.cy}`, background: s.bg ?? PAPER, color: INK }}
            >
              {placed.map((sh, j) => (
                <Placed key={j} sh={sh} deck={deck} media={media} />
              ))}
            </div>
            <figcaption className="flex items-center justify-between font-mono text-[11.5px] text-ink-3">
              <span>{t("Slide {{n}} of {{total}}", { n: i + 1, total: deck.slides.length })}</span>
            </figcaption>
            {loose.length ? (
              <div className="rounded-sm bg-well p-3 text-[13px]">
                <p className="mb-1 text-[12px] text-ink-3">{t("Not placed on the slide:")}</p>
                {loose.map((sh, j) => (
                  <Outline key={j} sh={sh} />
                ))}
              </div>
            ) : null}
          </figure>
        );
      })}
    </div>
  );
}

function Placed({ sh, deck, media }: { sh: Shape; deck: Deck; media: Record<string, string> }) {
  const b = sh.box!;
  const pos = {
    left: `${(b.x / deck.cx) * 100}%`,
    top: `${(b.y / deck.cy) * 100}%`,
    width: `${(b.w / deck.cx) * 100}%`,
    height: `${(b.h / deck.cy) * 100}%`,
  };
  // One point of type, as a share of the slide's width (cqw).
  const pt = (size: number) => `${(size / (deck.cx / 12700)) * 100}cqw`;
  if (sh.kind === "image") {
    const src = sh.image ? media[sh.image] : undefined;
    return src ? <img src={src} alt="" className="absolute object-contain" style={pos} /> : null;
  }
  if (sh.kind === "chart") {
    return (
      <div className="absolute flex items-center justify-center gap-1.5 rounded-xs border border-dashed border-current/30 opacity-60" style={{ ...pos, fontSize: pt(14) }}>
        <BarChart3 style={{ width: "1.2em", height: "1.2em" }} />
        {t("Chart")}
      </div>
    );
  }
  if (sh.kind === "table") {
    return (
      <div className="absolute overflow-hidden" style={pos}>
        <table className="h-full w-full border-collapse" style={{ fontSize: pt(12) }}>
          <tbody>
            {(sh.rows ?? []).map((r, i) => (
              <tr key={i} className={i === 0 ? "font-semibold" : ""}>
                {r.map((c, j) => (
                  <td key={j} className="border border-current/20 px-[0.4em] py-[0.2em] align-top">
                    {c}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  const base = sh.title ? 40 : 20;
  return (
    <div className="absolute overflow-hidden" style={{ ...pos, background: sh.fill, display: "flex", flexDirection: "column", justifyContent: sh.title ? "center" : "flex-start", padding: "0.4%" }}>
      {(sh.paras ?? []).map((p, i) => (
        <ParaView key={i} p={p} fontSize={(s?: number) => pt(s ?? (p.level > 0 ? base * 0.85 : base))} title={!!sh.title} />
      ))}
    </div>
  );
}

function ParaView({ p, fontSize, title }: { p: Para; fontSize: (s?: number) => string; title: boolean }) {
  const first = p.runs.find((r) => r.size)?.size;
  return (
    <p style={{ textAlign: p.align, fontSize: fontSize(first), lineHeight: 1.18, paddingLeft: `${p.level * 1.4}em`, fontWeight: title ? 600 : undefined, margin: "0 0 0.25em" }}>
      {p.bullet && p.runs.some((r) => r.text.trim()) ? <span className="mr-[0.4em]">•</span> : null}
      {p.runs.map((r, i) =>
        r.text === "\n" ? (
          <br key={i} />
        ) : (
          <span key={i} style={{ fontSize: r.size && r.size !== first ? fontSize(r.size) : undefined, fontWeight: r.bold ? 700 : undefined, fontStyle: r.italic ? "italic" : undefined, color: r.color }}>
            {r.text}
          </span>
        ),
      )}
    </p>
  );
}

function Outline({ sh }: { sh: Shape }) {
  if (sh.kind === "table") return <p>{(sh.rows ?? []).map((r) => r.join(" · ")).join(" / ")}</p>;
  if (sh.kind !== "text") return null;
  return (
    <div className={sh.title ? "font-semibold" : ""}>
      {(sh.paras ?? []).map((p, i) => (
        <p key={i} style={{ paddingLeft: `${p.level * 1.2}em` }}>
          {p.bullet ? "• " : ""}
          {p.runs.map((r) => r.text).join("")}
        </p>
      ))}
    </div>
  );
}
