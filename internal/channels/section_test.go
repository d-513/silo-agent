package channels

import (
	"reflect"
	"testing"
)

func TestSplitterAcrossChunks(t *testing.T) {
	s := &Splitter{}
	if got := s.Write("Hello "); len(got) != 0 {
		t.Fatalf("unexpected sections: %v", got)
	}
	if got := s.Write("world <section"); len(got) != 0 {
		t.Fatalf("sentinel should not resolve yet: %v", got)
	}
	got := s.Write("_send /> second")
	if !reflect.DeepEqual(got, []string{"Hello world"}) {
		t.Fatalf("got %v", got)
	}
	if tail := s.Flush(); tail != "second" {
		t.Fatalf("tail %q", tail)
	}
}

func TestSplitterVariants(t *testing.T) {
	for _, marker := range []string{"<section_send />", "<section_send/>", "< SECTION_SEND >", "<section_send>"} {
		s := &Splitter{}
		got := s.Write("one " + marker + " two")
		if !reflect.DeepEqual(got, []string{"one"}) {
			t.Fatalf("marker %q: got %v", marker, got)
		}
		if s.Flush() != "two" {
			t.Fatalf("marker %q tail", marker)
		}
	}
}

func TestSplitNoSentinel(t *testing.T) {
	parts, found := Split("  just one message  ")
	if found || len(parts) != 1 || parts[0] != "just one message" {
		t.Fatalf("got %v found=%v", parts, found)
	}
	parts, found = Split("a<section_send />b")
	if !found || !reflect.DeepEqual(parts, []string{"a", "b"}) {
		t.Fatalf("got %v found=%v", parts, found)
	}
}
