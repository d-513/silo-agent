package security

// Output rules are the mirror of the call rules: a call rule says whether an
// action may run, an output rule says what happens to its result before the
// Bot reads it.
const (
	// OutPass hands the result over as it is; nothing looks at it.
	OutPass = "pass"
	// OutFlag always wraps the result in the untrusted-data frame. No model
	// is asked, so it costs nothing.
	OutFlag = "flag"
	// OutCheck asks the filter model, which passes, flags or withholds it.
	OutCheck = "check"
	// OutWithhold never lets the result reach the Bot.
	OutWithhold = "withhold"
)

// Output normalises a stored or requested output mode; anything unknown passes.
func Output(s string) string {
	switch s {
	case OutFlag, OutCheck, OutWithhold:
		return s
	default:
		return OutPass
	}
}

// DefaultOutput is the output mode of a built-in action with no rule of the
// owner's. An action that is not built in (a connector's) has none here: its
// definition says.
func DefaultOutput(connector, action string) (string, bool) {
	if s, ok := lookup(connector, action); ok {
		return Output(s.output), true
	}
	if reserved[connector] {
		return OutPass, true
	}
	return "", false
}
