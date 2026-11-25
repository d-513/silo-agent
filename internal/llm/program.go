package llm

import "context"

// Program is the optional interface of a provider that runs a program on the
// control plane's machine which Silo does not ship (it is not Silo's to
// redistribute): the provider can say whether the program is there, and
// fetch it from its publisher when the operator asks. Admin draws one panel
// for every such provider (Descriptor.Program).
type Program interface {
	// ProgramStatus never fails: what could not be told is its Problem.
	ProgramStatus(ctx context.Context) ProgramStatus
	// InstallProgram starts fetching the newest release into the provider's
	// own folder and returns at once; ProgramStatus follows it. One already
	// under way is left to finish.
	InstallProgram(ctx context.Context) error
}

// ProgramStatus is a provider's program on this machine.
type ProgramStatus struct {
	// Name is what the program is called, About where an install gets it
	// and under whose terms.
	Name  string
	About string
	// Installed says a program was found; Version and Path are its own.
	Installed bool
	Version   string
	Path      string
	// Managed says it is the copy Silo installed, not one the operator
	// pointed at or the machine's own.
	Managed bool
	// CanInstall is false when the operator named the program themselves: a
	// download would not be the one that runs.
	CanInstall bool
	// Installing is a download under way, Done of Total bytes (Total 0 when
	// the publisher did not say).
	Installing  bool
	Done, Total int64
	// Problem is why the last install failed, or why the program found does
	// not run, in words for the operator.
	Problem string
}
