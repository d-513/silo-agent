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

func TestOwnBotAndRows(t *testing.T) {
	gdb := dbtest.New(t)
	alice, bob := &db.User{ID: "alice", Email: "a@x"}, &db.User{ID: "bob", Email: "b@x"}
	gdb.Create(alice)
	gdb.Create(bob)
	gdb.Create(&db.Bot{ID: "bot1", UserID: "alice", Name: "one"})
	gdb.Create(&db.FeedPost{ID: "p1", BotID: "bot1", Body: "hi"})
	gdb.Create(&db.FeedPost{ID: "p2", BotID: "other", Body: "elsewhere"})

	ctxA := WithUser(context.Background(), alice)
	ctxB := WithUser(context.Background(), bob)

	if b, err := OwnBot(ctxA, gdb, "bot1"); err != nil || b.Name != "one" {
		t.Fatalf("owner: %v %v", b, err)
	}
	if _, err := OwnBot(ctxB, gdb, "bot1"); code(err) != connect.CodeNotFound {
		t.Fatalf("another user's bot must be NotFound, got %v", err)
	}
	if _, err := OwnBot(ctxA, gdb, "missing"); code(err) != connect.CodeNotFound {
		t.Fatalf("missing bot must be NotFound, got %v", err)
	}

	if p, err := OwnBotRow[db.FeedPost](ctxA, gdb, "bot1", "p1", "feed post"); err != nil || p.Body != "hi" {
		t.Fatalf("own row: %v %v", p, err)
	}
	if _, err := OwnBotRow[db.FeedPost](ctxA, gdb, "bot1", "p2", "feed post"); code(err) != connect.CodeNotFound {
		t.Fatalf("a row of another bot must be NotFound, got %v", err)
	}
	if _, err := OwnBotRow[db.FeedPost](ctxB, gdb, "bot1", "p1", "feed post"); code(err) != connect.CodeNotFound {
		t.Fatalf("another user's row must be NotFound, got %v", err)
	}
}
