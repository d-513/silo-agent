package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/llm"
)

// A provider the operator signs in to (llm.Account) instead of giving a key.
// These RPCs are the same for every such provider: who is signed in, start a
// sign-in, hand over the code its page showed, sign out. What a sign-in
// leaves behind is the provider's business, except a setting it asks to have
// saved (a token), which goes into silo.yaml like any other.

// signedInTTL is how long a provider's answer to "is anyone signed in" is
// reused: the settings page asks with every load, and asking runs a program.
const signedInTTL = 30 * time.Second

type signedInAt struct {
	at time.Time
	ok bool
}

// signedIn says whether a sign-in provider has an account to work with.
func (s *Service) signedIn(ctx context.Context, provider string) bool {
	if v, ok := s.signed.Load(provider); ok && time.Since(v.(signedInAt).at) < signedInTTL {
		return v.(signedInAt).ok
	}
	client, err := s.models.Provider(provider)
	if err != nil {
		return false
	}
	acc, ok := client.(llm.Account)
	if !ok {
		return false
	}
	st, err := acc.AccountStatus(ctx)
	in := err == nil && st.SignedIn
	s.signed.Store(provider, signedInAt{at: time.Now(), ok: in})
	return in
}

// providerAccount finds a sign-in provider and builds its client from the
// saved settings.
func (s *Service) providerAccount(ctx context.Context, provider string) (llm.Account, string, error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, "", err
	}
	provider = strings.TrimSpace(provider)
	d, ok := llm.Lookup(provider)
	if !ok {
		return nil, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown model provider %q", provider))
	}
	client, err := s.models.Provider(provider)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, err)
	}
	acc, ok := client.(llm.Account)
	if !ok || !d.SignIn {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s has no sign-in; it takes a key", d.Name))
	}
	return acc, provider, nil
}

// accountStatus reads who is signed in. A provider that cannot say is a
// status with a problem, never an error: the page still shows the sign-in.
func accountStatus(ctx context.Context, acc llm.Account) *v1.ProviderAccount {
	st, err := acc.AccountStatus(ctx)
	if err != nil {
		st = llm.AccountStatus{Problem: err.Error()}
	}
	out := &v1.ProviderAccount{
		SignedIn: st.SignedIn, Method: st.Method, Email: st.Email, Plan: st.Plan, Problem: st.Problem,
	}
	if !st.Expires.IsZero() {
		out.ExpiresAt = st.Expires.Unix()
	}
	for _, m := range acc.SignInMethods() {
		out.Methods = append(out.Methods, &v1.ProviderSignInMethod{Id: m.ID, Label: m.Label, Description: m.Description})
	}
	return out
}

func (s *Service) GetProviderAccount(ctx context.Context, req *connect.Request[v1.GetProviderAccountRequest]) (*connect.Response[v1.ProviderAccount], error) {
	acc, _, err := s.providerAccount(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(accountStatus(ctx, acc)), nil
}

func (s *Service) BeginProviderSignIn(ctx context.Context, req *connect.Request[v1.BeginProviderSignInRequest]) (*connect.Response[v1.BeginProviderSignInResponse], error) {
	acc, _, err := s.providerAccount(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	step, err := acc.BeginSignIn(ctx, strings.TrimSpace(req.Msg.GetMethod()))
	if err != nil {
		// Never Unavailable: the control plane is fine, the provider is not.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.BeginProviderSignInResponse{Url: step.URL}), nil
}

func (s *Service) FinishProviderSignIn(ctx context.Context, req *connect.Request[v1.FinishProviderSignInRequest]) (*connect.Response[v1.ProviderAccount], error) {
	acc, provider, err := s.providerAccount(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	save, err := acc.FinishSignIn(ctx, req.Msg.GetCode())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	s.signed.Delete(provider)
	if len(save) > 0 {
		if s.store == nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("config store missing"))
		}
		patch := map[string]string{}
		for k, v := range save {
			patch["providers."+provider+"."+k] = v
		}
		if err := s.store.Patch(patch); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("the sign-in worked, but saving it did not: %w", err))
		}
		// The status is the saved settings': a token just stored counts.
		if acc, _, err = s.providerAccount(ctx, provider); err != nil {
			return nil, err
		}
	}
	return connect.NewResponse(accountStatus(ctx, acc)), nil
}

func (s *Service) SignOutProvider(ctx context.Context, req *connect.Request[v1.SignOutProviderRequest]) (*connect.Response[v1.ProviderAccount], error) {
	acc, provider, err := s.providerAccount(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	drop, err := acc.SignOut(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	s.signed.Delete(provider)
	patch := map[string]string{}
	for _, k := range drop {
		key := "providers." + provider + "." + k
		if s.store != nil && s.store.Get(key) != "" {
			patch[key] = ""
		}
	}
	if len(patch) > 0 {
		if err := s.store.Patch(patch); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		if acc, _, err = s.providerAccount(ctx, provider); err != nil {
			return nil, err
		}
	}
	return connect.NewResponse(accountStatus(ctx, acc)), nil
}
