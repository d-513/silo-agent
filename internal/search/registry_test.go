package search

import (
	"context"
	"testing"
)

type stubEngine struct{ hits []Hit }

func (s stubEngine) Search(context.Context, string, int) (Result, error) {
	return Result{Results: s.hits}, nil
}

func TestClampMax(t *testing.T) {
	if ClampMax(0) != DefaultMaxResults || ClampMax(-5) != DefaultMaxResults {
		t.Fatal("zero/negative should default")
	}
	if ClampMax(MaxResultsCap+1) != MaxResultsCap {
		t.Fatal("should cap")
	}
	if ClampMax(3) != 3 {
		t.Fatal("should pass through")
	}
}

func TestDefaultAndLookup(t *testing.T) {
	if !Known(DuckDuckGoScraper) {
		t.Fatal("duckduckgo should be known")
	}
	if _, err := New("", nil); err != nil {
		t.Fatalf("empty id should use default: %v", err)
	}
	if _, err := New("nope", nil); err == nil {
		t.Fatal("unknown engine should fail")
	}
	d, ok := Lookup(DuckDuckGoScraper)
	if !ok || d.ID != DuckDuckGoScraper {
		t.Fatalf("lookup %+v %v", d, ok)
	}
}

func TestRegisterAndUnregister(t *testing.T) {
	Register(Descriptor{ID: "stub", Name: "Stub"}, func(Settings) Engine {
		return stubEngine{hits: []Hit{{Title: "one"}}}
	})
	if !Known("stub") {
		t.Fatal("registered engine not known")
	}
	eng, err := New("stub", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Search(context.Background(), "q", 1)
	if err != nil || len(res.Results) != 1 || res.Results[0].Title != "one" {
		t.Fatalf("stub search: %v %+v", err, res)
	}
	Unregister("stub")
	if Known("stub") {
		t.Fatal("unregistered engine still known")
	}
}
