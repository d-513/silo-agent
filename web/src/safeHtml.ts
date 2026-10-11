// HTML made from a document somebody else wrote (a .docx turned into markup)
// is shown inside the console's own page, so anything in it that could act
// there is taken out first. The converter escapes text but passes a link's
// target through as written, and a `javascript:` link clicked in a preview
// would run as the person looking, signed in. What survives is a short list
// of formatting tags, web and mail links, and embedded raster images.

const TAGS = new Set([
  "a", "b", "blockquote", "br", "caption", "code", "col", "colgroup", "dd", "del", "div", "dl", "dt", "em",
  "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "img", "ins", "li", "ol", "p", "pre", "s", "span", "strong",
  "sub", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "u", "ul",
]);

// Tags whose content is not text to show: they go, children and all.
const DROPPED = new Set(["script", "style", "iframe", "object", "embed", "template", "noscript", "svg", "math", "form", "link", "meta", "base", "head", "title"]);

// safeHref is href when it leads to the web, a mailbox, or a place in the same
// document; "" otherwise. Browsers drop tabs and newlines inside a scheme, so
// they are dropped before looking.
export function safeHref(href: string): string {
  const v = href.replace(/[\u0000- ]+/g, "");
  if (v.startsWith("#")) return v;
  return /^(https?:\/\/|mailto:)/i.test(v) ? v : "";
}

// safeImage is src when it is an embedded raster image; "" otherwise. SVG can
// script and a remote address tells its server who opened the file.
export function safeImage(src: string): string {
  return /^data:image\/(png|jpe?g|gif|webp|bmp);base64,[a-z0-9+/=\s]*$/i.test(src) ? src : "";
}

// keptAttrs is what an element of this tag keeps of its attributes.
export function keptAttrs(tag: string, attrs: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  if (tag === "a") {
    const href = safeHref(attrs.href ?? "");
    if (href) {
      out.href = href;
      if (!href.startsWith("#")) {
        out.target = "_blank";
        out.rel = "noopener noreferrer";
      }
    }
    if (attrs.id && /^[\w-]+$/.test(attrs.id)) out.id = attrs.id;
  }
  if (tag === "img") {
    const src = safeImage(attrs.src ?? "");
    if (src) out.src = src;
    if (attrs.alt) out.alt = attrs.alt;
  }
  if (tag === "td" || tag === "th") {
    for (const k of ["colspan", "rowspan"]) if (/^\d{1,3}$/.test(attrs[k] ?? "")) out[k] = attrs[k];
  }
  return out;
}

// verdict says what happens to an element: kept, unwrapped (its children stay,
// the tag goes), or dropped with everything in it.
export function verdict(tag: string): "keep" | "unwrap" | "drop" {
  if (TAGS.has(tag)) return "keep";
  return DROPPED.has(tag) ? "drop" : "unwrap";
}

// safeHtml returns html with only the formatting above left in it. It parses
// with the browser, into a document that runs nothing and loads nothing.
export function safeHtml(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const clean = (parent: Element) => {
    for (const node of Array.from(parent.childNodes)) {
      if (node.nodeType === 3) continue;
      if (node.nodeType !== 1) {
        node.remove();
        continue;
      }
      const el = node as Element;
      const tag = el.localName;
      const what = el.namespaceURI === "http://www.w3.org/1999/xhtml" ? verdict(tag) : "drop";
      if (what === "drop") {
        el.remove();
        continue;
      }
      clean(el);
      if (what === "unwrap") {
        el.replaceWith(...Array.from(el.childNodes));
        continue;
      }
      const had: Record<string, string> = {};
      for (const a of Array.from(el.attributes)) {
        had[a.name.toLowerCase()] = a.value;
        el.removeAttribute(a.name);
      }
      for (const [k, v] of Object.entries(keptAttrs(tag, had))) el.setAttribute(k, v);
      if (tag === "img" && !el.getAttribute("src")) el.remove();
    }
  };
  clean(doc.body);
  return doc.body.innerHTML;
}
