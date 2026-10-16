package app_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	silov1connect "silo.agent/gen/silo/v1/silov1connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/dockerx"
)

// The Admin Containers tab: everything Silo runs on the engine, grouped by
// user and Bot, with strays the database no longer claims, and single
// container actions guarded to what the list named.

func listAdminContainers(t *testing.T, cl silov1connect.UIClient) *v1.AdminContainers {
	t.Helper()
	res, err := cl.ListAdminContainers(context.Background(), connect.NewRequest(&v1.ListAdminContainersRequest{}))
	if err != nil {
		t.Fatalf("ListAdminContainers: %v", err)
	}
	return res.Msg
}

// machineState returns the AdminContainers row of one Bot's machine.
func machineState(t *testing.T, out *v1.AdminContainers, botID string) string {
	t.Helper()
	for _, u := range out.Users {
		for _, g := range u.Bots {
			if g.BotId == botID {
				for _, c := range g.Containers {
					if c.Kind == "bot" {
						return c.State
					}
				}
				t.Fatalf("bot %s carries no machine", g.BotName)
			}
		}
	}
	t.Fatalf("bot %s is not in the tree", botID)
	return ""
}

func TestAdminContainersGroupsEveryUserAndBot(t *testing.T) {
	h := apptest.New(t)
	adminBot := h.CreateBot("Admin's bot")

	other, _ := h.SignedInUser("other@test.local")
	res, err := other.CreateBot(context.Background(), connect.NewRequest(&v1.CreateBotRequest{Name: "Other's bot"}))
	if err != nil {
		t.Fatalf("CreateBot (other): %v", err)
	}
	otherBot := res.Msg

	// A user who is not an admin gets nothing and no names.
	_, lerr := other.ListAdminContainers(context.Background(), connect.NewRequest(&v1.ListAdminContainersRequest{}))
	if lerr == nil || !strings.Contains(lerr.Error(), "admin only") {
		t.Fatalf("non-admin ListAdminContainers: %v, want admin-only", lerr)
	}

	out := listAdminContainers(t, h.Client)
	found := map[string]bool{}
	for _, u := range out.Users {
		switch u.Email {
		case "admin@test.local", "other@test.local":
		default:
			t.Fatalf("unexpected user group %q", u.Email)
		}
		for _, g := range u.Bots {
			if len(g.Containers) == 0 {
				t.Fatalf("bot %q carries no containers", g.BotName)
			}
			found[g.BotName] = true
			if (g.BotName == "Admin's bot" && g.BotId != adminBot.GetId()) || (g.BotName == "Other's bot" && g.BotId != otherBot.GetId()) {
				t.Fatalf("bot %q under the wrong id", g.BotName)
			}
			if st := machineState(t, out, g.BotId); st != "running" {
				t.Fatalf("machine of %q: %s, want running", g.BotName, st)
			}
			for _, c := range g.Containers {
				if c.Kind == "bot" && c.Name != dockerx.Name(g.BotId) {
					t.Fatalf("machine name %q", c.Name)
				}
			}
		}
	}
	if !found["Admin's bot"] || !found["Other's bot"] {
		t.Fatalf("missing bot groups: %v", found)
	}
	if out.System == nil {
		t.Fatal("system group must always be present")
	}
}

func TestAdminContainerActionsAndGuard(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Boxed")
	id := bot.GetId()
	machine := dockerx.Name(id)
	ctx := context.Background()

	// Stop and start go through the Bot's own lifecycle, so the row stays
	// consistent with the engine.
	if _, err := h.Client.AdminContainerAction(ctx, connect.NewRequest(&v1.AdminContainerActionRequest{Name: machine, Action: "stop"})); err != nil {
		t.Fatalf("stop machine: %v", err)
	}
	out := listAdminContainers(t, h.Client)
	if st := machineState(t, out, id); st != "stopped" {
		t.Fatalf("machine after stop: %s, want stopped", st)
	}
	if _, err := h.Client.AdminContainerAction(ctx, connect.NewRequest(&v1.AdminContainerActionRequest{Name: machine, Action: "start"})); err != nil {
		t.Fatalf("start machine: %v", err)
	}

	// A stray the database does not claim is listed, and removable.
	h.Fake.AddSiloContainer(dockerx.SiloContainer{ID: "left-over", Name: "silo-mcp-gone", Role: "mcp-stdio", Running: true})
	out = listAdminContainers(t, h.Client)
	var stray bool
	for _, c := range out.System.Containers {
		if c.Kind == "stray" && c.Name == "silo-mcp-gone" {
			stray = true
		}
	}
	if !stray {
		t.Fatal("the leftover sidecar is not listed under System")
	}
	if _, err := h.Client.AdminContainerAction(ctx, connect.NewRequest(&v1.AdminContainerActionRequest{Name: "silo-mcp-gone", Action: "remove"})); err != nil {
		t.Fatalf("remove stray: %v", err)
	}

	// Anything the list did not name is refused, admin or not.
	if _, err := h.Client.AdminContainerAction(ctx, connect.NewRequest(&v1.AdminContainerActionRequest{Name: "someone-elses-container", Action: "stop"})); err == nil || !strings.Contains(err.Error(), "not a Silo container") {
		t.Fatalf("action on a foreign container: %v, want refusal", err)
	}

	other, _ := h.SignedInUser("other@test.local")
	if _, aerr := other.AdminContainerAction(ctx, connect.NewRequest(&v1.AdminContainerActionRequest{Name: machine, Action: "stop"})); aerr == nil || !strings.Contains(aerr.Error(), "admin only") {
		t.Fatalf("non-admin action: %v, want admin-only", aerr)
	}
}

func TestAdminRemoveBotContainers(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("To sweep")
	id := bot.GetId()
	if _, err := h.Client.RemoveAdminBotContainers(context.Background(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatalf("RemoveAdminBotContainers: %v", err)
	}
	if !h.Fake.Gone(id) {
		t.Fatal("machine still there after remove all")
	}
	// A Bot that does not exist is refused, never wiped blindly.
	if _, err := h.Client.RemoveAdminBotContainers(context.Background(), connect.NewRequest(&v1.GetBotRequest{Id: "nope"})); err == nil {
		t.Fatal("removed an imaginary bot")
	}
}
