import CodeMirror from "@uiw/react-codemirror";
import { yaml } from "@codemirror/lang-yaml";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

const highlight = HighlightStyle.define([
  { tag: t.comment, color: "#64748b" },
  { tag: t.lineComment, color: "#64748b" },
  { tag: t.keyword, color: "#1d4ed8" },
  { tag: t.atom, color: "#1d4ed8" },
  { tag: t.bool, color: "#1d4ed8" },
  { tag: t.number, color: "#1d4ed8" },
  { tag: t.string, color: "#059669" },
  { tag: t.propertyName, color: "#0f172a" },
  { tag: t.definition(t.propertyName), color: "#0f172a" },
  { tag: t.separator, color: "#64748b" },
]);

const theme = EditorView.theme(
  {
    "&": {
      backgroundColor: "#ffffff",
      color: "#0f172a",
      fontSize: "13px",
    },
    ".cm-content": {
      fontFamily: '"Geist Mono Variable", "Geist Mono", "IBM Plex Mono", ui-monospace, monospace',
      caretColor: "#0f172a",
      minHeight: "16rem",
    },
    ".cm-gutters": {
      backgroundColor: "#f1f5f9",
      color: "#64748b",
      borderRight: "1px solid #e2e8f0",
    },
    ".cm-activeLine": { backgroundColor: "rgba(241, 245, 249, 0.6)" },
    ".cm-activeLineGutter": { backgroundColor: "#e2e8f0" },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#0f172a" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground": {
      backgroundColor: "#dbeafe",
    },
  },
  { dark: false },
);

export function YamlEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="overflow-hidden rounded-[6px] border border-thread bg-folio">
      <CodeMirror
        value={value}
        height="20rem"
        extensions={[yaml(), syntaxHighlighting(highlight), theme, EditorView.lineWrapping]}
        onChange={onChange}
        basicSetup={{ foldGutter: false }}
      />
    </div>
  );
}
