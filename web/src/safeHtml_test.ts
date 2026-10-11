import { eq } from "./testing.ts";
import { keptAttrs, safeHref, safeImage, verdict } from "./safeHtml.ts";

// Links: the web, mail, and anchors in the same document.
eq(safeHref("https://example.com/a?b=1"), "https://example.com/a?b=1", "https");
eq(safeHref("HTTP://example.com"), "HTTP://example.com", "scheme case");
eq(safeHref("mailto:a@example.com"), "mailto:a@example.com", "mailto");
eq(safeHref("#section-2"), "#section-2", "anchor");
eq(safeHref("javascript:alert(1)"), "", "javascript");
eq(safeHref("JaVaScRiPt:alert(1)"), "", "javascript in mixed case");
eq(safeHref(" java\tscript:alert(1)"), "", "a tab inside the scheme, as browsers read it");
eq(safeHref("java\nscript:alert(1)"), "", "a newline inside the scheme");
eq(safeHref("data:text/html,<script>alert(1)</script>"), "", "data page");
eq(safeHref("vbscript:x"), "", "vbscript");
eq(safeHref("//evil.example/x"), "", "protocol-relative");
eq(safeHref("/silo.v1.UI/DeleteBot"), "", "a path on the console itself");
eq(safeHref(""), "", "empty");

// Images: embedded raster only.
eq(safeImage("data:image/png;base64,iVBORw0KGgo="), "data:image/png;base64,iVBORw0KGgo=", "embedded png");
eq(safeImage("data:image/jpeg;base64,/9j/4AAQ"), "data:image/jpeg;base64,/9j/4AAQ", "embedded jpeg");
eq(safeImage("data:image/svg+xml;base64,PHN2Zz4="), "", "svg can script");
eq(safeImage("https://tracker.example/pixel.png"), "", "a remote image phones home");
eq(safeImage("data:text/html;base64,PHNjcmlwdD4="), "", "not an image");
eq(safeImage("javascript:alert(1)"), "", "javascript");

// Attributes: handlers and styles never survive, whatever the tag.
eq(keptAttrs("a", { href: "javascript:alert(1)", onclick: "alert(1)", style: "x" }), {}, "a bad link keeps nothing");
eq(
  keptAttrs("a", { href: "https://example.com", onclick: "alert(1)" }),
  { href: "https://example.com", target: "_blank", rel: "noopener noreferrer" },
  "a web link opens elsewhere and brings nothing back",
);
eq(keptAttrs("a", { href: "#note-1", id: "ref-1" }), { href: "#note-1", id: "ref-1" }, "footnote anchors work in place");
eq(keptAttrs("img", { src: "x", onerror: "alert(1)", alt: "a" }), { alt: "a" }, "an image with no safe source loses it");
eq(keptAttrs("p", { onmouseover: "alert(1)", style: "background:url(x)", class: "c" }), {}, "a paragraph keeps no attributes");
eq(keptAttrs("td", { colspan: "2", rowspan: "x", onclick: "y" }), { colspan: "2" }, "table spans, digits only");

// Elements.
eq(verdict("p"), "keep", "formatting stays");
eq(verdict("table"), "keep", "tables stay");
eq(verdict("script"), "drop", "script goes with its content");
eq(verdict("iframe"), "drop", "frames go");
eq(verdict("svg"), "drop", "svg goes");
eq(verdict("form"), "drop", "forms go");
eq(verdict("style"), "drop", "styles go");
eq(verdict("font"), "unwrap", "an unknown tag leaves its text behind");
eq(verdict("button"), "unwrap", "controls leave their text behind");

console.log("safeHtml ok");
