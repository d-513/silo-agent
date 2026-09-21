package channels

import (
	"reflect"
	"testing"
)

// TestSplitterCharByChar feeds a sentinel one byte at a time, which is the
// worst case for a streaming adapter.
func TestSplitterCharByChar(t *testing.T) {
	s := &Splitter{}
	var sections []string
	for _, r := range "abc<section_send />def" {
		sections = append(sections, s.Write(string(r))...)
	}
	if !reflect.DeepEqual(sections, []string{"abc"}) {
		t.Fatalf("sections %v", sections)
	}
	if tail := s.Flush(); tail != "def" {
		t.Fatalf("tail %q", tail)
	}
}

func TestSplitterMultipleSentinels(t *testing.T) {
	s := &Splitter{}
	got := s.Write("a<section_send />b<section_send />c")
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
	if tail := s.Flush(); tail != "c" {
		t.Fatalf("tail %q", tail)
	}
}

func TestSplitterTrimsWhitespace(t *testing.T) {
	s := &Splitter{}
	got := s.Write("  alpha  <section_send />  beta  ")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("got %v", got)
	}
	if tail := s.Flush(); tail != "beta" {
		t.Fatalf("tail %q", tail)
	}
}

func TestSplitterEmpty(t *testing.T) {
	s := &Splitter{}
	if got := s.Write(""); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if tail := s.Flush(); tail != "" {
		t.Fatalf("tail %q", tail)
	}
}

func TestSplitEmptySegmentsDropped(t *testing.T) {
	parts, found := Split("<section_send /><section_send />real")
	if !found {
		t.Fatal("sentinel not found")
	}
	if !reflect.DeepEqual(parts, []string{"real"}) {
		t.Fatalf("parts %v", parts)
	}
}
