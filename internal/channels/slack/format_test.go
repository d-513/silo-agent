package slack

import (
	"strings"
	"testing"
)

func TestToMrkdwn(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bold and italic", "**bold** and *it* and _it_ and __b__", "*bold* and _it_ and _it_ and *b*"},
		{"strike", "~~gone~~", "~gone~"},
		{"heading", "## Plan", "*Plan*"},
		{"link", "see [the doc](https://x.test/a?b=1&c=2)", "see <https://x.test/a?b=1&amp;c=2|the doc>"},
		{"bare link label", "[https://x.test](https://x.test)", "<https://x.test>"},
		{"autolink", "<https://x.test/a>", "<https://x.test/a>"},
		{"image", "![chart](https://x.test/c.png)", "<https://x.test/c.png|chart>"},
		{"a pipe in the address", "[a](https://x.test/a|b)", "<https://x.test/a%7Cb|a>"},
		{"escapes", "a < b && c > d", "a &lt; b &amp;&amp; c &gt; d"},
		{"no pings", "<!channel> <!here> <@U123>", "&lt;!channel&gt; &lt;!here&gt; &lt;@U123&gt;"},
		{"inline code", "run `a <b> **c**` now", "run `a &lt;b&gt; **c**` now"},
		{"bullets", "- one\n* two\n  + three", "• one\n• two\n  • three"},
		{"tasks", "- [ ] todo\n- [x] done", "• ☐ todo\n• ☑ done"},
		{"numbers stay", "1. first\n2. second", "1. first\n2. second"},
		{"quote", "> wise **words** <x>", "> wise *words* &lt;x&gt;"},
		{"rule", "a\n---\nb", "a\n\nb"},
		{"fence", "```go\nif a < b && **c** {\n```", "```\nif a &lt; b &amp;&amp; **c** {\n```"},
		{"word stars", "2*3*4 stays", "2*3*4 stays"},
	}
	for _, c := range cases {
		if got := toMrkdwn(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestToMrkdwnTablesBecomeAlignedCode(t *testing.T) {
	in := "Results:\n\n| Name | Score |\n|:-----|------:|\n| **Alice** | 10 |\n| Bob & co | 7 |\n\nDone."
	want := strings.Join([]string{
		"Results:",
		"",
		"```",
		"Name     | Score",
		"---------+------",
		"Alice    | 10",
		"Bob &amp; co | 7",
		"```",
		"",
		"Done.",
	}, "\n")
	if got := toMrkdwn(in); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// A line with a pipe and no separator under it is just text.
	if got := toMrkdwn("a | b"); got != "a | b" {
		t.Fatalf("got %q", got)
	}
}

func TestFromSlack(t *testing.T) {
	user := func(id string) string { return map[string]string{"U1": "Alice"}[id] }
	channel := func(id string) string { return map[string]string{"C1": "general"}[id] }
	cases := []struct{ in, want string }{
		{"<@U1> hi", "@Alice hi"},
		{"<@U2|bob> hi", "@bob hi"},
		{"<@U9> hi", "@U9 hi"},
		{"in <#C1>", "in #general"},
		{"in <#C2|random>", "in #random"},
		{"<!here> and <!channel>", "@here and @channel"},
		{"<!subteam^S1|@devs> look", "@devs look"},
		{"<!date^1392734382^{date}|Feb 18> ok", "Feb 18 ok"},
		{"<https://x.test/a?b=1&amp;c=2>", "https://x.test/a?b=1&c=2"},
		{"<https://x.test|the site>", "the site (https://x.test)"},
		{"<https://x.test|https://x.test>", "https://x.test"},
		{"<mailto:a@x.test|a@x.test>", "a@x.test"},
		{"a &lt;b&gt; &amp;amp;", "a <b> &amp;"},
	}
	for _, c := range cases {
		if got := fromSlack(c.in, user, channel); got != c.want {
			t.Errorf("fromSlack(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGroupMembers(t *testing.T) {
	for in, want := range map[string]string{
		"mpdm-alice--bob--carol-1": "alice, bob, carol",
		"mpdm-alice--bob-12":       "alice, bob",
	} {
		if got := groupMembers(in); got != want {
			t.Errorf("groupMembers(%q) = %q, want %q", in, got, want)
		}
	}
}
