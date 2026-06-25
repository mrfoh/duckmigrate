package duckmigrate

import (
	"database/sql"
	"io"
	"io/fs"
	"log/slog"
	"time"
)

type ChecksumMode int

const (
	ChecksumStrict ChecksumMode = iota
	ChecksumWarn
	ChecksumOff
)

type config struct {
	dsn          string
	db           *sql.DB
	source       Source
	table        string
	transactions bool
	checksumMode ChecksumMode
	lockTimeout  time.Duration
	logger       *slog.Logger
}

func defaultConfig() config {
	return config{
		table:        "schema_migrations",
		transactions: true,
		checksumMode: ChecksumStrict,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

type Option func(*config)

func WithDSN(dsn string) Option {
	return func(c *config) { c.dsn = dsn }
}

func WithDB(db *sql.DB) Option {
	return func(c *config) { c.db = db }
}

func WithSource(src Source) Option {
	return func(c *config) { c.source = src }
}

func WithFileSource(dir string) Option {
	return func(c *config) { c.source = NewFileSource(dir) }
}

func WithFS(fsys fs.FS, root string) Option {
	return func(c *config) { c.source = NewFSSource(fsys, root) }
}

func WithTable(name string) Option {
	return func(c *config) {
		if name != "" {
			c.table = name
		}
	}
}

func WithTransactions(enabled bool) Option {
	return func(c *config) { c.transactions = enabled }
}

func WithChecksumMode(mode ChecksumMode) Option {
	return func(c *config) { c.checksumMode = mode }
}

func WithLockTimeout(d time.Duration) Option {
	return func(c *config) { c.lockTimeout = d }
}

func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
