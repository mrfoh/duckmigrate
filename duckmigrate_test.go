package duckmigrate_test

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrfoh/duckmigrate"
)

//go:embed testdata/migrations/*.sql
var embedded embed.FS

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func writeMigrations(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func reversibleSet() map[string]string {
	return map[string]string{
		"0001_a.up.sql":   `CREATE TABLE a (id INTEGER);`,
		"0001_a.down.sql": `DROP TABLE a;`,
		"0002_b.up.sql":   `CREATE TABLE b (id INTEGER);`,
		"0002_b.down.sql": `DROP TABLE b;`,
	}
}

func newMigrator(t *testing.T, db *sql.DB, dir string, opts ...duckmigrate.Option) *duckmigrate.Migrator {
	t.Helper()
	base := []duckmigrate.Option{
		duckmigrate.WithDB(db),
		duckmigrate.WithFileSource(dir),
		duckmigrate.WithLogger(quietLogger()),
	}
	m, err := duckmigrate.New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT count(*) FROM duckdb_tables() WHERE table_name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestUpAppliesAndTracks(t *testing.T) {
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, reversibleSet()))
	ctx := context.Background()

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	v, dirty, err := m.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 || dirty {
		t.Fatalf("got version=%d dirty=%v, want 2/false", v, dirty)
	}
	if !tableExists(t, db, "a") || !tableExists(t, db, "b") {
		t.Fatal("expected tables a and b to exist")
	}
	if err := m.Up(ctx); !errors.Is(err, duckmigrate.ErrNoChange) {
		t.Fatalf("second Up = %v, want ErrNoChange", err)
	}
}

func TestUpDownRoundTrip(t *testing.T) {
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, reversibleSet()))
	ctx := context.Background()

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Version(ctx); !errors.Is(err, duckmigrate.ErrNilVersion) {
		t.Fatalf("Version after Down = %v, want ErrNilVersion", err)
	}
	if tableExists(t, db, "a") || tableExists(t, db, "b") {
		t.Fatal("expected tables to be gone after Down")
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("re-Up failed: %v", err)
	}
}

func TestSteps(t *testing.T) {
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, reversibleSet()))
	ctx := context.Background()

	mustVersion := func(want uint64) {
		t.Helper()
		v, _, err := m.Version(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if v != want {
			t.Fatalf("version=%d want %d", v, want)
		}
	}

	if err := m.Steps(ctx, 1); err != nil {
		t.Fatal(err)
	}
	mustVersion(1)
	if err := m.Steps(ctx, 1); err != nil {
		t.Fatal(err)
	}
	mustVersion(2)
	if err := m.Steps(ctx, -1); err != nil {
		t.Fatal(err)
	}
	mustVersion(1)
}

func TestGoto(t *testing.T) {
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, reversibleSet()))
	ctx := context.Background()

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Goto(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "b") {
		t.Fatal("expected b to be rolled back after goto 1")
	}
	if err := m.Goto(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "a") {
		t.Fatal("expected a to be rolled back after goto 0")
	}
	if err := m.Goto(ctx, 99); !errors.As(err, new(duckmigrate.ErrUnknownVersion)) {
		t.Fatalf("goto unknown = %v, want ErrUnknownVersion", err)
	}
}

func TestIrreversibleBlocksRollback(t *testing.T) {
	files := reversibleSet()
	files["0003_c.up.sql"] = `CREATE TABLE c (id INTEGER);`
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, files))
	ctx := context.Background()

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	err := m.Down(ctx)
	var irr duckmigrate.ErrIrreversibleMigration
	if !errors.As(err, &irr) || irr.Version != 3 {
		t.Fatalf("Down = %v, want ErrIrreversibleMigration{3}", err)
	}
	v, _, _ := m.Version(ctx)
	if v != 3 {
		t.Fatalf("version=%d, want 3 (unchanged after refused rollback)", v)
	}
	if !tableExists(t, db, "a") {
		t.Fatal("refused rollback must not have changed the database")
	}
}

func TestForceClearsDirtyAndDoesNotFabricateHistory(t *testing.T) {
	files := map[string]string{
		"0001_bad.up.sql": "-- duckmigrate:no-transaction\nCREATE TABLE good (id INTEGER); SELECT * FROM missing_table;",
	}
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, files))
	ctx := context.Background()

	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the bad migration to fail")
	}
	if _, dirty, _ := m.Version(ctx); !dirty {
		t.Fatal("expected dirty state after a failed no-transaction migration")
	}
	if err := m.Up(ctx); !errors.As(err, new(duckmigrate.ErrDirty)) {
		t.Fatalf("Up while dirty = %v, want ErrDirty", err)
	}

	if err := m.Force(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Version(ctx); !errors.Is(err, duckmigrate.ErrNilVersion) {
		t.Fatalf("Version after force 0 = %v, want ErrNilVersion", err)
	}

	if err := m.Force(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var ups int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations_history WHERE direction = 'up'`).Scan(&ups); err != nil {
		t.Fatal(err)
	}
	if ups != 0 {
		t.Fatalf("force fabricated %d up history rows, want 0", ups)
	}
}

func TestChecksumMismatch(t *testing.T) {
	db := memDB(t)
	dir := writeMigrations(t, map[string]string{
		"0001_a.up.sql":   `CREATE TABLE a (id INTEGER);`,
		"0001_a.down.sql": `DROP TABLE a;`,
	})
	ctx := context.Background()

	m1 := newMigrator(t, db, dir)
	if err := m1.Up(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "0001_a.up.sql"), []byte(`CREATE TABLE a (id BIGINT);`), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := newMigrator(t, db, dir)
	err := m2.Up(ctx)
	if !errors.As(err, new(duckmigrate.ErrChecksumMismatch)) {
		t.Fatalf("Up after editing applied file = %v, want ErrChecksumMismatch", err)
	}

	m3 := newMigrator(t, db, dir, duckmigrate.WithChecksumMode(duckmigrate.ChecksumOff))
	if err := m3.Up(ctx); !errors.Is(err, duckmigrate.ErrNoChange) {
		t.Fatalf("Up with checksum off = %v, want ErrNoChange", err)
	}
}

func TestEmbedFSSource(t *testing.T) {
	db := memDB(t)
	m, err := duckmigrate.New(
		duckmigrate.WithDB(db),
		duckmigrate.WithFS(embedded, "testdata/migrations"),
		duckmigrate.WithLogger(quietLogger()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, db, "users") || !tableExists(t, db, "audit") {
		t.Fatal("expected embedded migrations to create users and audit")
	}
}

func TestDrop(t *testing.T) {
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, reversibleSet()))
	ctx := context.Background()

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "a") || tableExists(t, db, "schema_migrations") || tableExists(t, db, "schema_migrations_history") {
		t.Fatal("Drop should remove user tables and tracking tables")
	}
}

func TestStatus(t *testing.T) {
	files := reversibleSet()
	files["0003_c.up.sql"] = `CREATE TABLE c (id INTEGER);`
	db := memDB(t)
	m := newMigrator(t, db, writeMigrations(t, files))
	ctx := context.Background()

	if err := m.Steps(ctx, 1); err != nil {
		t.Fatal(err)
	}
	items, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d status items, want 3", len(items))
	}
	if !items[0].Applied || items[0].Checksum != duckmigrate.ChecksumStateOK {
		t.Fatalf("migration 1 status = %+v, want applied/ok", items[0])
	}
	if items[1].Applied {
		t.Fatalf("migration 2 should be pending")
	}
	if !items[2].Irreversible {
		t.Fatalf("migration 3 should be marked irreversible")
	}
}
