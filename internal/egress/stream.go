package egress

import (
	"bytes"
	"io"
)

// swap is one rewrite a replacer makes as a stream passes: every old becomes
// new. A swap with no new is forbidden: meeting old ends the stream, and the
// replacer remembers whose it was.
type swap struct {
	old, new string
	// name is the secret the swap is for, reported when a forbidden one is
	// met.
	name   string
	forbid bool
}

// replacer rewrites a stream as it passes, never holding it whole: what it
// cannot yet tell from the start of a match (a tail that is a prefix of some
// old) is all it keeps back, so a server-sent event is not held for the next
// one, and a body of any size costs one buffer.
type replacer struct {
	r     io.Reader
	swaps []swap
	first [256]bool

	in  []byte // read, not yet decided
	out []byte // decided, not yet handed on
	err error  // what the source said last

	// leaked names the forbidden swap that stopped the stream.
	leaked string
}

// errForbidden is what a replacer answers once a forbidden value went by.
var errForbidden = io.ErrClosedPipe

// newReplacer wraps r. With no swaps it is r itself.
func newReplacer(r io.Reader, swaps []swap) io.Reader {
	var keep []swap
	for _, s := range swaps {
		if s.old != "" {
			keep = append(keep, s)
		}
	}
	if len(keep) == 0 {
		return r
	}
	rp := &replacer{r: r, swaps: keep}
	for _, s := range keep {
		rp.first[s.old[0]] = true
	}
	return rp
}

func (rp *replacer) Read(p []byte) (int, error) {
	for len(rp.out) == 0 {
		if rp.leaked != "" {
			return 0, errForbidden
		}
		if rp.err != nil {
			// The source is done: what is left can no longer grow into a match.
			if len(rp.in) > 0 {
				rp.decide(true)
				continue
			}
			return 0, rp.err
		}
		buf := make([]byte, 32<<10)
		n, err := rp.r.Read(buf)
		rp.in = append(rp.in, buf[:n]...)
		rp.err = err
		rp.decide(err != nil)
	}
	n := copy(p, rp.out)
	rp.out = rp.out[n:]
	return n, nil
}

// decide moves what can be decided from in to out. With final set nothing
// more will arrive, so a partial match is plain text.
func (rp *replacer) decide(final bool) {
	i := 0
	start := 0
	for i < len(rp.in) {
		if !rp.first[rp.in[i]] {
			i++
			continue
		}
		s, wait := rp.match(rp.in[i:], final)
		if wait {
			break
		}
		if s == nil {
			i++
			continue
		}
		rp.out = append(rp.out, rp.in[start:i]...)
		if s.forbid {
			rp.leaked = s.name
			rp.in = nil
			return
		}
		rp.out = append(rp.out, s.new...)
		i += len(s.old)
		start = i
	}
	rp.out = append(rp.out, rp.in[start:i]...)
	rp.in = append(rp.in[:0:0], rp.in[i:]...)
}

// match finds the swap whose old starts b, the longest when several do. wait
// reports that b is too short to tell yet.
func (rp *replacer) match(b []byte, final bool) (*swap, bool) {
	var best *swap
	for k := range rp.swaps {
		s := &rp.swaps[k]
		if len(b) >= len(s.old) {
			if bytes.HasPrefix(b, []byte(s.old)) && (best == nil || len(s.old) > len(best.old)) {
				best = s
			}
			continue
		}
		if !final && bytes.HasPrefix([]byte(s.old), b) {
			return nil, true
		}
	}
	return best, false
}

// leakedFrom names the forbidden value that stopped r, if r is a replacer that
// met one.
func leakedFrom(r io.Reader) string {
	if rp, ok := r.(*replacer); ok {
		return rp.leaked
	}
	return ""
}
