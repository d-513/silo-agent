// Package dbtest gives each test its own throwaway Postgres schema on the dev
// database from docker-compose.dev.yml (`make db-up`).
package dbtest

import (
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

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
		adminErr = sqlDB.Ping()
	})
	return admin, adminErr
}

// New opens a fresh schema, migrates every table into it, and drops it when
// the test ends. It fails the test at once when Postgres is not running.
func New(t testing.TB) *gorm.DB {
	t.Helper()
	adm, err := adminDB()
	if err != nil {
		t.Fatalf("Postgres not reachable at %s (run `make db-up`): %v", URL(), err)
	}
	schema := "t_" + ids.New()[:16]
	if err := adm.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	u, err := url.Parse(URL())
	if err != nil {
		t.Fatalf("test database url: %v", err)
	}
	q := u.Query()
	// public stays on the path: that is where the vector extension lives.
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	gdb, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		if err := adm.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)).Error; err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}
