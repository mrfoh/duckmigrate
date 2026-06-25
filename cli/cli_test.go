package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dbPath, migrationsDir string, args ...string) (string, string, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	a := &app{out: &out, err: &errBuf}
	cmd := a.root()
	full := append([]string{"--database", dbPath, "--path", migrationsDir}, args...)
	cmd.SetArgs(full)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestCLIFullFlow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.duckdb")

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0001_a.up.sql", `CREATE TABLE a (id INTEGER);`)
	write("0001_a.down.sql", `DROP TABLE a;`)
	write("0002_b.up.sql", `CREATE TABLE b (id INTEGER);`)
	write("0002_b.down.sql", `DROP TABLE b;`)

	if _, _, err := run(t, dbPath, dir, "up"); err != nil {
		t.Fatalf("up: %v", err)
	}

	out, _, err := run(t, dbPath, dir, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if strings.TrimSpace(out) != "2" {
		t.Fatalf("version output = %q, want 2", out)
	}

	out, _, err = run(t, dbPath, dir, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "applied") {
		t.Fatalf("status output missing applied state:\n%s", out)
	}

	if _, _, err := run(t, dbPath, dir, "down", "1"); err != nil {
		t.Fatalf("down 1: %v", err)
	}
	out, _, _ = run(t, dbPath, dir, "version")
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("version after down 1 = %q, want 1", out)
	}

	if _, _, err := run(t, dbPath, dir, "drop", "-f"); err != nil {
		t.Fatalf("drop: %v", err)
	}
}

func TestCLIDownAllRequiresForce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.duckdb")
	if err := os.WriteFile(filepath.Join(dir, "0001_a.up.sql"), []byte(`CREATE TABLE a (id INTEGER);`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, dbPath, dir, "up"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, dbPath, dir, "down"); err == nil {
		t.Fatal("down without -f should fail")
	}
}

func TestCLICreate(t *testing.T) {
	dir := t.TempDir()
	out, _, err := run(t, ":memory:", dir, "create", "Add Users Table", "--seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "000001_add_users_table.up.sql") || !strings.Contains(out, "000001_add_users_table.down.sql") {
		t.Fatalf("create output unexpected:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "000001_add_users_table.up.sql")); err != nil {
		t.Fatalf("up file not created: %v", err)
	}
}
