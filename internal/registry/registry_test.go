package registry

import (
	"fmt"
	"sync"
	"testing"
)

type desc struct{ ID, Name string }

func newReg() *Registry[desc, string] {
	return New[desc, string](func(d desc) string { return d.ID })
}

func TestPutKeepsOrderAndReplacesInPlace(t *testing.T) {
	r := newReg()
	r.Put(desc{"a", "A"}, "fa")
	r.Put(desc{"b", "B"}, "fb")
	r.Put(desc{"a", "A2"}, "fa2")
	got := r.All()
	if len(got) != 2 || got[0] != (desc{"a", "A2"}) || got[1] != (desc{"b", "B"}) {
		t.Fatalf("order/replace: %+v", got)
	}
	if _, impl, ok := r.Lookup("a"); !ok || impl != "fa2" {
		t.Fatalf("lookup a: %q %v", impl, ok)
	}
}

func TestLookupMissingAndRemove(t *testing.T) {
	r := newReg()
	r.Put(desc{"a", "A"}, "fa")
	if d, impl, ok := r.Lookup("nope"); ok || d != (desc{}) || impl != "" {
		t.Fatalf("missing lookup should be zero: %+v %q %v", d, impl, ok)
	}
	r.Remove("a")
	r.Remove("a") // removing twice is fine
	if _, _, ok := r.Lookup("a"); ok || len(r.All()) != 0 {
		t.Fatal("removed entry still there")
	}
}

func TestAllIsACopy(t *testing.T) {
	r := newReg()
	r.Put(desc{"a", "A"}, "fa")
	all := r.All()
	all[0].Name = "mutated"
	if got, _, _ := r.Lookup("a"); got.Name != "A" {
		t.Fatal("All leaked the internal slice")
	}
}

func TestConcurrentUse(t *testing.T) {
	r := newReg()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i % 5)
			r.Put(desc{id, id}, id)
			r.Lookup(id)
			r.All()
			if i%3 == 0 {
				r.Remove(id)
			}
		}(i)
	}
	wg.Wait()
}
