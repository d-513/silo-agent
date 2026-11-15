package llm

import (
	"context"
	"time"
)

// Account is the optional interface of a provider whose access is a sign-in
// the operator does in a browser instead of a key they paste: the control
// plane starts it, shows the page to open, and is handed the code that page
// gives back. Admin draws one panel for every such provider (Descriptor.SignIn),
// so a new one is this interface and nothing in the UI.
type Account interface {
	// AccountStatus says who is signed in. A provider that cannot tell (its
	// binary is missing) answers with Problem set, not an error.
	AccountStatus(ctx context.Context) (AccountStatus, error)
	// SignInMethods are the ways to sign in, the first the usual one.
	SignInMethods() []SignInMethod
	// BeginSignIn starts a sign-in and answers the page to open. A sign-in
	// begun earlier and never finished is dropped.
	BeginSignIn(ctx context.Context, method string) (SignInStep, error)
	// FinishSignIn hands the code from that page to the sign-in begun last.
	// Save holds provider settings the caller must store (a token the
	// provider was issued), keyed like Descriptor.Settings.
	FinishSignIn(ctx context.Context, code string) (save map[string]string, err error)
	// SignOut forgets the stored sign-in. Clear names the provider settings
	// the caller must empty with it (a saved token).
	SignOut(ctx context.Context) (clear []string, err error)
}

// AccountStatus is who a provider is signed in as.
type AccountStatus struct {
	SignedIn bool
	// Method is how: the provider's own word ("claude.ai", "oauth_token").
	Method string
	Email  string
	// Plan is the subscription the account is on, when the provider says.
	Plan    string
	Expires time.Time
	// Problem is why the status could not be read, in words for the operator.
	Problem string
}

// SignInMethod is one way to sign in to a provider.
type SignInMethod struct {
	ID          string
	Label       string
	Description string
}

// SignInStep is what the operator does next: open URL and paste what it shows.
type SignInStep struct {
	URL string
}
