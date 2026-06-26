package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/mrfoh/duckmigrate"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags -X.
var version = "dev"

type app struct {
	database    string
	path        string
	table       string
	noTx        bool
	checksum    string
	lockTimeout time.Duration
	verbose     bool

	out io.Writer
	err io.Writer
}

func Execute() int {
	a := &app{out: os.Stdout, err: os.Stderr}
	if err := a.root().Execute(); err != nil {
		fmt.Fprintln(a.err, "Error:", err)
		return 1
	}
	return 0
}

func (a *app) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "duckmigrate",
		Short:         "Database migrations for DuckDB",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&a.database, "database", "d", envOr("DUCKMIGRATE_DATABASE", ""), "DuckDB path, :memory:, or duckdb:// URL")
	pf.StringVarP(&a.path, "path", "p", envOr("DUCKMIGRATE_PATH", "migrations"), "migrations directory")
	pf.StringVar(&a.table, "table", envOr("DUCKMIGRATE_TABLE", "schema_migrations"), "tracking table name")
	pf.BoolVar(&a.noTx, "no-transaction", false, "do not wrap migrations in a transaction")
	pf.StringVar(&a.checksum, "checksum", "strict", "checksum validation mode: strict|warn|off")
	pf.DurationVar(&a.lockTimeout, "lock-timeout", 0, "how long to wait for a database lock held by another process")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "verbose logging")

	root.AddCommand(
		a.createCmd(),
		a.upCmd(),
		a.downCmd(),
		a.gotoCmd(),
		a.forceCmd(),
		a.versionCmd(),
		a.statusCmd(),
		a.dropCmd(),
	)
	return root
}

func (a *app) logger() *slog.Logger {
	level := slog.LevelWarn
	if a.verbose {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(a.err, &slog.HandlerOptions{Level: level}))
}

func (a *app) migrator() (*duckmigrate.Migrator, error) {
	if a.database == "" {
		return nil, duckmigrate.ErrNoDatabase
	}
	return duckmigrate.New(
		duckmigrate.WithDSN(a.database),
		duckmigrate.WithFileSource(a.path),
		duckmigrate.WithTable(a.table),
		duckmigrate.WithTransactions(!a.noTx),
		duckmigrate.WithChecksumMode(parseChecksumMode(a.checksum)),
		duckmigrate.WithLockTimeout(a.lockTimeout),
		duckmigrate.WithLogger(a.logger()),
	)
}

func (a *app) withMigrator(fn func(context.Context, *duckmigrate.Migrator) error) error {
	m, err := a.migrator()
	if err != nil {
		return err
	}
	defer m.Close()
	return fn(context.Background(), m)
}

func parseChecksumMode(s string) duckmigrate.ChecksumMode {
	switch s {
	case "warn":
		return duckmigrate.ChecksumWarn
	case "off":
		return duckmigrate.ChecksumOff
	default:
		return duckmigrate.ChecksumStrict
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
