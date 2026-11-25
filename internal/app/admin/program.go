package admin

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/llm"
)

// A provider that runs a program Silo does not ship (llm.Program: Claude
// Code, which is Anthropic's). These RPCs are the same for every such
// provider: is the program there, and fetch it. The download is the
// provider's; here it is only started and watched.

func (s *Service) providerProgram(ctx context.Context, provider string) (llm.Program, string, error) {
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
	prog, ok := client.(llm.Program)
	if !ok || !d.Program {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s runs no program on this server", d.Name))
	}
	return prog, provider, nil
}

func programProto(st llm.ProgramStatus) *v1.ProviderProgram {
	return &v1.ProviderProgram{
		Name: st.Name, About: st.About, Installed: st.Installed, Version: st.Version, Path: st.Path,
		Managed: st.Managed, CanInstall: st.CanInstall, Installing: st.Installing,
		DoneBytes: st.Done, TotalBytes: st.Total, Problem: st.Problem,
	}
}

func (s *Service) GetProviderProgram(ctx context.Context, req *connect.Request[v1.GetProviderProgramRequest]) (*connect.Response[v1.ProviderProgram], error) {
	prog, provider, err := s.providerProgram(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	st := prog.ProgramStatus(ctx)
	if !st.Installing {
		// A program that just arrived changes who can be signed in.
		s.signed.Delete(provider)
	}
	return connect.NewResponse(programProto(st)), nil
}

func (s *Service) InstallProviderProgram(ctx context.Context, req *connect.Request[v1.InstallProviderProgramRequest]) (*connect.Response[v1.ProviderProgram], error) {
	prog, _, err := s.providerProgram(ctx, req.Msg.GetProvider())
	if err != nil {
		return nil, err
	}
	if err := prog.InstallProgram(ctx); err != nil {
		// Never Unavailable: the control plane is fine, the install is not.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(programProto(prog.ProgramStatus(ctx))), nil
}
