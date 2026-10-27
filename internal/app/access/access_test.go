package access

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

func code(err error) connect.Code { return connect.CodeOf(err) }

func TestUserRoundTripsThroughContext(t *testing.T) {
	if User(context.Background()) != nil {
		t.Fatal("empty context has a user")
	}
	u := &db.User{ID: "u1"}
	if got := User(WithUser(context.Background(), u)); got != u {
		t.Fatalf("got %v", got)
	}
}

func TestRequireAdmin(t *testing.T) {
	if code(RequireAdmin(context.Background())) != connect.CodePermissionDenied {
		t.Fatal("no user must be denied")
	}
	if code(RequireAdmin(WithUser(context.Background(), &db.User{ID: "u"}))) != connect.CodePermissionDenied {
		t.Fatal("a non-admin must be denied")
	}
	if err := RequireAdmin(WithUser(context.Background(), &db.User{ID: "a", Admin: true})); err != nil {
		t.Fatalf("admin refused: %v", err)
	}
}

func TestBotAccess(t *testing.T) {
	gdb := dbtest.New(t)
	alice := &db.User{ID: "alice", Email: "a@x"}
	bob := &db.User{ID: "bob", Email: "b@x"}
	carol := &db.User{ID: "carol", Email: "c@x"}
	dave := &db.User{ID: "dave", Email: "d@x"}
	for _, u := range []*db.User{alice, bob, carol, dave} {
		gdb.Create(u)
	}
	gdb.Create(&db.Space{ID: "s1", Name: "Acme"})
	gdb.Create(&db.SpaceMember{SpaceID: "s1", UserID: "alice", Role: RoleOwner})
	gdb.Create(&db.SpaceMember{SpaceID: "s1", UserID: "bob", Role: RoleMember})
	gdb.Create(&db.SpaceMember{SpaceID: "s1", UserID: "carol", Role: RoleAdmin})
	gdb.Create(&db.Bot{ID: "bot1", SpaceID: "s1", CreatedBy: "alice", Name: "Scout"})
	gdb.Create(&db.BotAccess{BotID: "bot1", UserID: "alice", Role: RoleManage})
	gdb.Create(&db.BotAccess{BotID: "bot1", UserID: "bob", Role: RoleUse})
	gdb.Create(&db.FeedPost{ID: "p1", BotID: "bot1", Body: "hi"})
	gdb.Create(&db.FeedPost{ID: "p2", BotID: "other", Body: "elsewhere"})

	as := func(u *db.User) context.Context { return WithUser(context.Background(), u) }
	cases := []struct {
		who  *db.User
		need Need
		want connect.Code // 0 = allowed
	}{
		{alice, Use, 0}, {alice, Manage, 0}, {alice, Administer, 0},
		{bob, Use, 0}, {bob, Manage, connect.CodePermissionDenied}, {bob, Administer, connect.CodePermissionDenied},
		// An admin of the Workspace who is not on the Bot may delete it, never open it.
		{carol, Use, connect.CodeNotFound}, {carol, Manage, connect.CodeNotFound}, {carol, Administer, 0},
		{dave, Use, connect.CodeNotFound}, {dave, Administer, connect.CodeNotFound},
	}
	for _, c := range cases {
		b, err := Bot(as(c.who), gdb, "bot1", c.need)
		if c.want == 0 {
			if err != nil || b.Name != "Scout" {
				t.Errorf("%s need %d: %v %v", c.who.ID, c.need, b, err)
			}
			continue
		}
		if code(err) != c.want {
			t.Errorf("%s need %d: got %v, want %v", c.who.ID, c.need, err, c.want)
		}
	}
	if _, err := Bot(as(alice), gdb, "missing", Use); code(err) != connect.CodeNotFound {
		t.Fatalf("missing bot must be NotFound, got %v", err)
	}

	if p, err := Row[db.FeedPost](as(bob), gdb, "bot1", "p1", "feed post", Use); err != nil || p.Body != "hi" {
		t.Fatalf("own row: %v %v", p, err)
	}
	if _, err := Row[db.FeedPost](as(alice), gdb, "bot1", "p2", "feed post", Use); code(err) != connect.CodeNotFound {
		t.Fatalf("a row of another bot must be NotFound, got %v", err)
	}
	if _, err := Row[db.FeedPost](as(dave), gdb, "bot1", "p1", "feed post", Use); code(err) != connect.CodeNotFound {
		t.Fatalf("a stranger's row must be NotFound, got %v", err)
	}
}

func TestSpaceAccess(t *testing.T) {
	gdb := dbtest.New(t)
	for _, id := range []string{"o", "a", "m", "g", "x"} {
		gdb.Create(&db.User{ID: id, Email: id + "@x"})
	}
	gdb.Create(&db.Space{ID: "s1", Name: "Acme"})
	for id, role := range map[string]string{"o": RoleOwner, "a": RoleAdmin, "m": RoleMember, "g": RoleGuest} {
		gdb.Create(&db.SpaceMember{SpaceID: "s1", UserID: id, Role: role})
	}
	allowed := map[string]SpaceNeed{"o": Own, "a": Admin, "m": Build, "g": View}
	for id, top := range allowed {
		ctx := WithUser(context.Background(), &db.User{ID: id})
		for need := View; need <= Own; need++ {
			_, _, err := Space(ctx, gdb, "s1", need)
			if need <= top && err != nil {
				t.Errorf("%s need %d refused: %v", id, need, err)
			}
			if need > top && code(err) != connect.CodePermissionDenied {
				t.Errorf("%s need %d: got %v, want PermissionDenied", id, need, err)
			}
		}
	}
	if _, _, err := Space(WithUser(context.Background(), &db.User{ID: "x"}), gdb, "s1", View); code(err) != connect.CodeNotFound {
		t.Fatalf("a stranger must get NotFound, got %v", err)
	}
}

func TestSuspendedIsNoEnabledManager(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "alice", Email: "a@x"})
	gdb.Create(&db.User{ID: "bob", Email: "b@x"})
	gdb.Create(&db.Bot{ID: "bot1", Name: "Scout"})
	gdb.Create(&db.BotAccess{BotID: "bot1", UserID: "alice", Role: RoleManage})
	gdb.Create(&db.BotAccess{BotID: "bot1", UserID: "bob", Role: RoleUse})
	if Suspended(gdb, "bot1") || !CanUse(gdb, "bot1", "bob") {
		t.Fatal("a Bot with an enabled manager runs")
	}
	gdb.Model(&db.User{}).Where("id = ?", "alice").Update("disabled", true)
	if !Suspended(gdb, "bot1") || CanUse(gdb, "bot1", "bob") {
		t.Fatal("a Bot whose only manager is disabled is suspended, for its users too")
	}
	gdb.Model(&db.BotAccess{}).Where("user_id = ?", "bob").Update("role", RoleManage)
	if Suspended(gdb, "bot1") {
		t.Fatal("another enabled manager keeps it running")
	}
	if CanUse(gdb, "bot1", "carol") {
		t.Fatal("someone not on the Bot cannot use it")
	}
}
