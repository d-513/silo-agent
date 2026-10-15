package security

// The operator's rule layer sits over the owner's. An operator row for a
// connector.action is the installation's opinion; while it is locked the owner
// may tighten it but not loosen it, and the engine takes the stricter of the
// two. A row that is not locked is a default the owner may override either
// way, so it only applies while the owner has no rule of their own.
//
// "Stricter" is by how much a mode holds a call back: allow lets it run, auto
// puts it to a model, ask to the human, deny refuses. The output side runs
// pass, flag (always framed), check (a model judges) then withhold.

var ruleRank = map[string]int{Allow: 0, Auto: 1, Ask: 2, Deny: 3}

var outputRank = map[string]int{OutPass: 0, OutFlag: 1, OutCheck: 2, OutWithhold: 3}

// Strictness is the rank of a call rule; an unknown mode ranks as ask, so a
// bad value can never loosen a lock.
func Strictness(mode string) int {
	if r, ok := ruleRank[mode]; ok {
		return r
	}
	return ruleRank[Ask]
}

// OutputStrictness is the rank of an output rule; an unknown mode ranks as
// check.
func OutputStrictness(mode string) int {
	if r, ok := outputRank[mode]; ok {
		return r
	}
	return outputRank[OutCheck]
}

// Stricter answers the call rule that holds a call back more.
func Stricter(a, b string) string {
	if Strictness(b) > Strictness(a) {
		return b
	}
	return a
}

// Looser reports whether call rule a holds a call back less than b: what a
// locked operator row refuses to let an owner do.
func Looser(a, b string) bool { return Strictness(a) < Strictness(b) }

// StricterOutput answers the output rule that hides more.
func StricterOutput(a, b string) string {
	if OutputStrictness(b) > OutputStrictness(a) {
		return b
	}
	return a
}

// LooserOutput reports whether output rule a hides less than b.
func LooserOutput(a, b string) bool { return OutputStrictness(a) < OutputStrictness(b) }
