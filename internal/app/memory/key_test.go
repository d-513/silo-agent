package memory

import (
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

// The database computes the store key from the same bytes, in the same order,
// as the Go fallback: a key written before must still match, or every digest
// would be rewritten (and every Bot's prompt cache with it).
func TestStoreKeyMatchesInDatabase(t *testing.T) {
	gdb := dbtest.New(t)
	s := &Service{db: gdb}
	vec := pgvector.NewVector(make([]float32, 1536))
	rows := [][2]string{{"a1", "likes tea"}, {"b2", "Zażółć gęślą jaźń — 東京"}, {"c3", "line\nbreak"}}
	for _, r := range rows {
		if err := gdb.Create(&db.Memory{ID: r[0], BotID: "bot", Content: r[1], Embedding: vec, CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	gdb.Create(&db.Memory{ID: "z9", BotID: "other", Content: "not this bot", Embedding: vec, CreatedAt: time.Now()})
	if got, want := s.storeKey("bot"), storeKeyOf(rows); got != want {
		t.Fatalf("key %s, want %s", got, want)
	}
	if got, want := s.storeKey("nobody"), storeKeyOf(nil); got != want {
		t.Fatalf("empty key %s, want %s", got, want)
	}
}
