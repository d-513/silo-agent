package egress

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReplacerSwapsAcrossReads(t *testing.T) {
	in := strings.Repeat("a", 100) + "ghp_SECRET" + "b" + "ghp_SECRET"
	r := newReplacer(iotest.OneByteReader(strings.NewReader(in)), []swap{{old: "ghp_SECRET", new: "ghp_STANDN"}})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", 100) + "ghp_STANDN" + "b" + "ghp_STANDN"; string(out) != want {
		t.Fatalf("got %q", out)
	}
}

// A tail that only might start a match is the one thing held back; anything
// else goes on at once, so a stream of small events is not delayed.
func TestReplacerHoldsOnlyAPossibleMatch(t *testing.T) {
	pr, pw := io.Pipe()
	r := newReplacer(pr, []swap{{old: "SECRETVALUE", new: "STANDINVALU"}})
	go func() { _, _ = pw.Write([]byte("data: 1\n\nSECR")) }()
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "data: 1\n\n" {
		t.Fatalf("first read %q %v", buf[:n], err)
	}
	go func() { _, _ = pw.Write([]byte("XY")); pw.Close() }()
	rest, _ := io.ReadAll(r)
	if string(rest) != "SECRXY" {
		t.Fatalf("rest %q", rest)
	}
}

func TestReplacerStopsOnAForbiddenValue(t *testing.T) {
	r := newReplacer(strings.NewReader("hello ghp_SURROGATE world"), []swap{{old: "ghp_SURROGATE", name: "GITHUB", forbid: true}})
	out, err := io.ReadAll(r)
	if !errors.Is(err, errForbidden) {
		t.Fatalf("err = %v", err)
	}
	if string(out) != "hello " || leakedFrom(r) != "GITHUB" {
		t.Fatalf("out %q leaked %q", out, leakedFrom(r))
	}
}

func TestReplacerWithNoSwapsIsTheSource(t *testing.T) {
	src := strings.NewReader("x")
	if newReplacer(src, nil) != io.Reader(src) {
		t.Fatal("an empty replacer wraps the source")
	}
}
