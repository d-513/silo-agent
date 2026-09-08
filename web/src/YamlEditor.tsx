import CodeMirror from "@uiw/react-codemirror";
import { yaml } from "@codemirror/lang-yaml";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

const highlight = HighlightStyle.define([
  { tag: t.comment, color: "#5f5e58" },
  { tag: t.lineComment, color: "#5f5e58" },
  { tag: t.keyword, color: "#2a3f5f" },
  { tag: t.atom, color: "#2a3f5f" },
  { tag: t.bool, color: "#2a3f5f" },
  { tag: t.number, color: "#2a3f5f" },
  { tag: t.string, color: "#3d6f6a" },
  { tag: t.propertyName, color: "#1e2126" },
  { tag: t.definition(t.propertyName), color: "#1e2126" },
  { tag: t.separator, color: "#5f5e58" },
]);

const theme = EditorView.theme(
  {
    "&": {
      backgroundColor: "#fffcf7",
      color: "#1e2126",
      fontSize: "13px",
    },
    ".cm-content": {
      fontFamily: '"IBM Plex Mono", ui-monospace, monospace',
      caretColor: "#1e2126",
      minHeight: "16rem",
    },
    ".cm-gutters": {
      backgroundColor: "#ede9df",
      color: "#5f5e58",
      borderRight: "1px solid #c9c3b6",
    },
    ".cm-activeLine": { backgroundColor: "rgba(237, 233, 223, 0.5)" },
    ".cm-activeLineGutter": { backgroundColor: "#ede9df" },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#1e2126" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground": {
      backgroundColor: "#d7dee8",
    },
  },
  { dark: false },
);

export function YamlEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="overflow-hidden rounded border border-thread bg-folio">
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
