import { hasCode, hasMath } from "./mdPlugins.ts";
import { eq } from "./testing.ts";

// KaTeX loads only for messages that have math, so a miss here means a formula
// that never renders. A false hit just loads the chunk for nothing.
eq(hasMath("plain text, no formulas"), false, "no math");
eq(hasMath("cost is $5"), true, "dollar sign");
eq(hasMath("inline \\(x^2\\)"), true, "paren delimiters");
eq(hasMath("display \\[ x \\]"), true, "bracket delimiters");
eq(hasMath("\\begin{align} a \\end{align}"), true, "environment");
eq(hasMath("a ( b ) and [ c ] and a lone \\ backslash"), false, "brackets and backslash alone");

eq(hasCode("no fences here, just `inline`"), false, "inline code");
eq(hasCode("```python\nprint(1)\n```"), true, "backtick fence");
eq(hasCode("~~~\ncode\n~~~"), true, "tilde fence");
