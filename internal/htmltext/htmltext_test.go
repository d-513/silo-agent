package htmltext

import (
	"strings"
	"testing"
)

func TestFlattenKeepsTextLinksAndBlocks(t *testing.T) {
	got := Flatten(`<html><head><style>p{color:red}</style><title>x</title></head><body>
<script>alert(1)</script>
<h1>Confirm your address</h1>
<p>Hello  <b>Ada</b>,<br>your code is <code>482913</code>.</p>
<p><a href="https://example.com/verify?t=abc">Verify now</a></p>
<p><a href="https://example.com">https://example.com</a></p>
</body></html>`)
	// Blocks are separated by one blank line; a <br> is a plain line break.
	want := "Confirm your address\n\nHello Ada,\nyour code is 482913.\n\nVerify now (https://example.com/verify?t=abc)\n\nhttps://example.com"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "alert") || strings.Contains(got, "color") {
		t.Fatalf("script or style leaked: %q", got)
	}
}
