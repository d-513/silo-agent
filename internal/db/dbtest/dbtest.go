// Package dbtest gives each test its own throwaway Postgres schema on the dev
// database from docker-compose.dev.yml (`make db-up`).
package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// DefaultURL is the silo_test database created by deploy/postgres/init.sql.
const DefaultURL = "postgres://silo:silo@localhost:5433/silo_test?sslmode=disable"

var (
	adminOnce sync.Once
	admin     *gorm.DB
	adminErr  error
)

// URL is SILO_TEST_DATABASE_URL, or DefaultURL.
func URL() string {
	if u := os.Getenv("SILO_TEST_DATABASE_URL"); u != "" {
		return u
	}
	return DefaultURL
}

func adminDB() (*gorm.DB, error) {
	adminOnce.Do(func() {
		admin, adminErr = gorm.Open(postgres.Open(URL()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if adminErr != nil {
			return
		}
		sqlDB, err := admin.DB()
		if err != nil {
			adminErr = err
			return
		}
		sqlDB.SetMaxOpenConns(2)
		if adminErr = sqlDB.Ping(); adminErr == nil {
			dropStale(admin)
		}
	})
	return admin, adminErr
}

// New opens an empty, migrated schema and gives it back when the test ends.
// It fails the test at once when Postgres is not running.
//
// Migrating a schema is ~60 tables of DDL, and under parallel test processes
// that catalog work is what the suite waits on, so schemas are pooled per
// process: a finished test's schema is emptied (TRUNCATE, and any table made
// at run time dropped) and handed to the next test. One whose shape a test
// changed (a migration test drops columns) is dropped instead.
func New(t testing.TB) *gorm.DB {
	t.Helper()
	adm, err := adminDB()
	if err != nil {
		t.Fatalf("Postgres not reachable at %s (run `make db-up`): %v", URL(), err)
	}
	schema, fresh := pool.take()
	if fresh {
		schema = fmt.Sprintf("tp_%d_%s", time.Now().Unix(), ids.New()[:8])
		if err := adm.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	gdb, sqlDB, err := open(schema)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		if !pool.give(adm, schema) {
			if err := adm.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)).Error; err != nil {
				t.Logf("drop schema %s: %v", schema, err)
			}
		}
	})
	if fresh {
		if err := db.Migrate(gdb); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		pool.learn(adm, schema)
	}
	return gdb
}

func open(schema string) (*gorm.DB, *sql.DB, error) {
	u, err := url.Parse(URL())
	if err != nil {
		return nil, nil, err
	}
	q := u.Query()
	// public stays on the path: that is where the vector extension lives.
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	gdb, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	return gdb, sqlDB, nil
}

// poolStale is the age past which a pooled schema left by an earlier process
// is dropped: a run takes minutes, so an older one has no owner.
const poolStale = time.Hour

var pool schemaPool

// schemaPool is this process's emptied, migrated schemas and the shape a
// fresh migration has, to tell an untouched schema from one a test altered.
type schemaPool struct {
	mu     sync.Mutex
	free   []string
	tables map[string]bool
	shape  string
}

// take hands out a pooled schema, or reports that a new one must be made.
func (p *schemaPool) take() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := len(p.free); n > 0 {
		s := p.free[n-1]
		p.free = p.free[:n-1]
		return s, false
	}
	return "", true
}

// learn records what a freshly migrated schema looks like.
func (p *schemaPool) learn(adm *gorm.DB, schema string) {
	tables := tablesOf(adm, schema)
	shape := shapeOf(adm, schema)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tables == nil {
		p.tables, p.shape = map[string]bool{}, shape
		for _, t := range tables {
			p.tables[t] = true
		}
	}
}

// give empties schema and pools it, or reports false when it cannot be
// reused and must be dropped.
func (p *schemaPool) give(adm *gorm.DB, schema string) bool {
	p.mu.Lock()
	known, shape := p.tables, p.shape
	p.mu.Unlock()
	if known == nil {
		return false
	}
	var keep, extra []string
	for _, t := range tablesOf(adm, schema) {
		if known[t] {
			keep = append(keep, schema+"."+quote(t))
		} else {
			extra = append(extra, schema+"."+quote(t))
		}
	}
	if len(extra) > 0 && adm.Exec("DROP TABLE "+strings.Join(extra, ", ")+" CASCADE").Error != nil {
		return false
	}
	if len(keep) != len(known) || shapeOf(adm, schema) != shape {
		return false
	}
	// A connection a test left in a transaction must not hang the run here.
	err := adm.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '2s'").Error; err != nil {
			return err
		}
		return tx.Exec("TRUNCATE " + strings.Join(keep, ", ") + " RESTART IDENTITY CASCADE").Error
	})
	if err != nil {
		return false
	}
	p.mu.Lock()
	p.free = append(p.free, schema)
	p.mu.Unlock()
	return true
}

func tablesOf(adm *gorm.DB, schema string) []string {
	var out []string
	adm.Raw("SELECT tablename FROM pg_tables WHERE schemaname = ? ORDER BY tablename", schema).Scan(&out)
	return out
}

// shapeOf summarizes a schema's columns and indexes.
func shapeOf(adm *gorm.DB, schema string) string {
	var cols, idx string
	adm.Raw(`SELECT COALESCE(md5(string_agg(table_name || '.' || column_name || ':' || data_type || ':' || COALESCE(column_default, ''), ',' ORDER BY table_name, column_name)), '')
		FROM information_schema.columns WHERE table_schema = ?`, schema).Scan(&cols)
	adm.Raw(`SELECT COALESCE(md5(string_agg(indexdef, ',' ORDER BY indexname)), '') FROM pg_indexes WHERE schemaname = ?`, schema).Scan(&idx)
	return cols + idx
}

func quote(ident string) string { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }

// dropStale drops pooled schemas earlier processes left behind.
func dropStale(adm *gorm.DB) {
	var names []string
	adm.Raw("SELECT nspname FROM pg_namespace WHERE nspname LIKE 'tp\\_%'").Scan(&names)
	cutoff := time.Now().Add(-poolStale).Unix()
	for _, n := range names {
		parts := strings.SplitN(n, "_", 3)
		if len(parts) != 3 {
			continue
		}
		if at, err := strconv.ParseInt(parts[1], 10, 64); err == nil && at < cutoff {
			adm.Exec("DROP SCHEMA " + n + " CASCADE")
		}
	}
}
