import type { ReactElement } from "react";
import { t } from "../i18n.ts";

type Pos = number[];
type Geometry = { type: string; coordinates?: unknown; geometries?: Geometry[] };
type Feature = { type: "Feature"; geometry: Geometry | null; properties?: Record<string, unknown> | null };

// A GeoJSON file drawn as shapes on a plain projection. No map tiles: those
// would have the console ask a tile server about where the human is looking.
export default function GeoMap({ text }: { text: string }) {
  let features: Feature[] = [];
  try {
    const g = JSON.parse(text) as { type: string; features?: Feature[] } & Geometry;
    if (g.type === "FeatureCollection") features = g.features ?? [];
    else if (g.type === "Feature") features = [g as unknown as Feature];
    else features = [{ type: "Feature", geometry: g, properties: {} }];
  } catch {
    return <p className="text-ink-2">{t("This map could not be read.")}</p>;
  }
  const pts: Pos[] = [];
  const collect = (c: unknown) => {
    if (Array.isArray(c) && typeof c[0] === "number") pts.push(c as Pos);
    else if (Array.isArray(c)) c.forEach(collect);
  };
  const geoms = (g: Geometry | null): Geometry[] => (!g ? [] : g.type === "GeometryCollection" ? (g.geometries ?? []).flatMap(geoms) : [g]);
  features.forEach((f) => geoms(f.geometry).forEach((g) => collect(g.coordinates)));
  if (pts.length === 0) return <p className="text-ink-2">{t("This map has no shapes.")}</p>;
  const lons = pts.map((p) => p[0]);
  const lats = pts.map((p) => p[1]);
  const [minX, maxX, minY, maxY] = [Math.min(...lons), Math.max(...lons), Math.min(...lats), Math.max(...lats)];
  const k = Math.cos((((minY + maxY) / 2) * Math.PI) / 180) || 1;
  const w = Math.max((maxX - minX) * k, 1e-6);
  const h = Math.max(maxY - minY, 1e-6);
  const W = 1000;
  const H = Math.max(200, Math.min(1400, (W * h) / w));
  const pad = 24;
  const sx = (W - 2 * pad) / w;
  const sy = (H - 2 * pad) / h;
  const s = Math.min(sx, sy);
  const x = (lon: number) => pad + (lon - minX) * k * s + ((W - 2 * pad) - w * s) / 2;
  const y = (lat: number) => pad + (maxY - lat) * s + ((H - 2 * pad) - h * s) / 2;
  const ring = (r: Pos[]) => r.map((p, i) => `${i ? "L" : "M"}${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join("") + "Z";
  const line = (r: Pos[]) => r.map((p, i) => `${i ? "L" : "M"}${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join("");
  const shapes: ReactElement[] = [];
  features.forEach((f, fi) => {
    const name = String(f.properties?.name ?? f.properties?.NAME ?? "");
    geoms(f.geometry).forEach((g, gi) => {
      const key = `${fi}-${gi}`;
      const c = g.coordinates as never;
      const title = name ? <title>{name}</title> : null;
      switch (g.type) {
        case "Polygon":
          shapes.push(<path key={key} d={(c as Pos[][]).map(ring).join("")} className="fill-well stroke-ink-2" strokeWidth={1.2} fillRule="evenodd">{title}</path>);
          break;
        case "MultiPolygon":
          shapes.push(<path key={key} d={(c as Pos[][][]).flatMap((p) => p.map(ring)).join("")} className="fill-well stroke-ink-2" strokeWidth={1.2} fillRule="evenodd">{title}</path>);
          break;
        case "LineString":
          shapes.push(<path key={key} d={line(c as Pos[])} className="fill-none stroke-ink" strokeWidth={2}>{title}</path>);
          break;
        case "MultiLineString":
          shapes.push(<path key={key} d={(c as Pos[][]).map(line).join("")} className="fill-none stroke-ink" strokeWidth={2}>{title}</path>);
          break;
        case "Point":
          shapes.push(<circle key={key} cx={x((c as Pos)[0])} cy={y((c as Pos)[1])} r={5} className="fill-ink">{title}</circle>);
          break;
        case "MultiPoint":
          (c as Pos[]).forEach((p, pi) => shapes.push(<circle key={`${key}-${pi}`} cx={x(p[0])} cy={y(p[1])} r={5} className="fill-ink">{title}</circle>));
          break;
      }
    });
  });
  return (
    <div className="space-y-1.5">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full rounded-sm bg-surface shadow-card" role="img">
        {shapes}
      </svg>
      <p className="font-mono text-[11.5px] text-ink-3">
        {t("{{count}} features", { count: features.length })} · {minY.toFixed(3)}, {minX.toFixed(3)} → {maxY.toFixed(3)}, {maxX.toFixed(3)}
      </p>
    </div>
  );
}
