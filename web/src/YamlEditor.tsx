import CodeMirror from "@uiw/react-codemirror";
import { yaml } from "@codemirror/lang-yaml";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

const highlight = HighlightStyle.define([
  { tag: t.comment, color: "var(--color-ink-3)" },
  { tag: t.lineComment, color: "var(--color-ink-3)" },
  { tag: t.keyword, color: "var(--color-cobalt)" },
  { tag: t.atom, color: "var(--color-cobalt)" },
  { tag: t.bool, color: "var(--color-cobalt)" },
  { tag: t.number, color: "var(--color-cobalt)" },
  { tag: t.string, color: "var(--color-emerald)" },
  { tag: t.propertyName, color: "var(--color-ink)" },
  { tag: t.definition(t.propertyName), color: "var(--color-ink)" },
  { tag: t.separator, color: "var(--color-ink-3)" },
]);

const theme = EditorView.theme(
  {
    "&": {
      backgroundColor: "var(--color-surface)",
      color: "var(--color-ink)",
      fontSize: "13px",
    },
    ".cm-content": {
      fontFamily: '"Geist Mono Variable", "Geist Mono", ui-monospace, monospace',
      caretColor: "var(--color-ink)",
      minHeight: "16rem",
    },
    ".cm-gutters": {
      backgroundColor: "var(--color-well)",
      color: "var(--color-ink-3)",
      borderRight: "1px solid var(--color-line)",
    },
    ".cm-activeLine": { backgroundColor: "var(--color-well)" },
    ".cm-activeLineGutter": { backgroundColor: "var(--color-pressed)" },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--color-ink)" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground": {
      backgroundColor: "var(--color-cobalt-pale)",
    },
  },
  { dark: false },
);

export function YamlEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="overflow-hidden rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface">
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
