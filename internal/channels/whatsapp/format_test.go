package whatsapp

import "testing"

func TestToWhatsApp(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain text is untouched", "Hello, world.", "Hello, world."},
		{"bold", "this is **important** stuff", "this is *important* stuff"},
		{"bold with underscores", "this is __important__ stuff", "this is *important* stuff"},
		{"italic", "this is *slanted* text", "this is _slanted_ text"},
		{"italic keeps WhatsApp italic", "already _italic_ here", "already _italic_ here"},
		{"bold and italic side by side", "**bold** and *italic*", "*bold* and _italic_"},
		{"two italics", "*a* then *b*", "_a_ then _b_"},
		{"strikethrough", "~~gone~~", "~gone~"},
		{"heading becomes bold", "## Plan\nstep one", "*Plan*\nstep one"},
		{"heading with bold inside", "# **Plan**", "*Plan*"},
		{"star bullets become dashes", "* one\n* two\n  * nested", "- one\n- two\n  - nested"},
		{"dash bullets stay", "- one\n- two", "- one\n- two"},
		{"numbered list stays", "1. one\n2. two", "1. one\n2. two"},
		{"link with a label", "see [the docs](https://example.com/a?b=1)", "see the docs (https://example.com/a?b=1)"},
		{"link that is its own label", "[https://example.com](https://example.com)", "https://example.com"},
		{"image becomes its address", "![chart](https://example.com/c.png)", "chart (https://example.com/c.png)"},
		{"horizontal rule vanishes", "above\n---\nbelow", "above\n\nbelow"},
		{"math is not italic", "2 * 3 * 4 = 24", "2 * 3 * 4 = 24"},
		{"inline code is untouched", "run `a **b** *c*` now", "run `a **b** *c*` now"},
		{"code around markup", "**x** `y` **z**", "*x* `y` *z*"},
		{"fenced code is untouched", "before **b**\n```go\nx := **p\n* not a bullet\n```\nafter **a**", "before *b*\n```go\nx := **p\n* not a bullet\n```\nafter *a*"},
		{"snake_case is not italic", "use my_var_name here", "use my_var_name here"},
		{"quote stays", "> quoted", "> quoted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := toWhatsApp(tc.in); got != tc.want {
				t.Fatalf("\n in: %q\ngot: %q\nwant:%q", tc.in, got, tc.want)
			}
		})
	}
}
