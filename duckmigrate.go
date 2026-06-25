package duckmigrate

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

type Migrator struct {
	cfg     config
	db      *sql.DB
	ownDB   bool
	source  Source
	tracker *tracker
	logger  *slog.Logger
	mu      sync.Mutex
}

func New(opts ...Option) (*Migrator, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.source == nil {
		return nil, ErrNoSource
	}

	m := &Migrator{cfg: cfg, source: cfg.source, logger: cfg.logger}

	if cfg.db != nil {
		m.db = cfg.db
	} else {
		db, err := openDuckDB(normalizeDSN(cfg.dsn), cfg.lockTimeout)
		if err != nil {
			return nil, err
		}
		m.db = db
		m.ownDB = true
	}

	tr, err := newTracker(m.db, cfg.table)
	if err != nil {
		if m.ownDB {
			_ = m.db.Close()
		}
		return nil, err
	}
	m.tracker = tr
	return m, nil
}

func normalizeDSN(dsn string) string {
	return strings.TrimPrefix(dsn, "duckdb://")
}

func isInMemory(dsn string) bool {
	return dsn == "" || dsn == ":memory:" || strings.HasPrefix(dsn, ":memory:?")
}

func openDuckDB(dsn string, lockTimeout time.Duration) (*sql.DB, error) {
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, err
	}
	if isInMemory(dsn) {
		db.SetMaxOpenConns(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout+time.Second)
	defer cancel()

	deadline := time.Now().Add(lockTimeout)
	for {
		perr := db.PingContext(ctx)
		if perr == nil {
			return db, nil
		}
		if !isLockError(perr) || lockTimeout <= 0 || time.Now().After(deadline) {
			_ = db.Close()
			if isLockError(perr) {
				return nil, ErrLockTimeout{cause: perr}
			}
			return nil, perr
		}
		select {
		case <-ctx.Done():
			_ = db.Close()
			return nil, ErrLockTimeout{cause: perr}
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func isLockError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "could not set lock") ||
		strings.Contains(s, "conflicting lock") ||
		strings.Contains(s, "lock on file")
}

func (m *Migrator) DB() *sql.DB { return m.db }

func (m *Migrator) Close() error {
	if m.ownDB {
		return m.db.Close()
	}
	return nil
}

func (m *Migrator) Up(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	migs, current, err := m.load(ctx)
	if err != nil {
		return err
	}
	pend := pending(migs, current)
	if len(pend) == 0 {
		return ErrNoChange
	}
	for _, mig := range pend {
		if err := m.applyUp(ctx, mig); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) Down(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	migs, current, err := m.load(ctx)
	if err != nil {
		return err
	}
	applied := appliedAsc(migs, current)
	if len(applied) == 0 {
		return ErrNoChange
	}
	return m.rollback(ctx, applied, 0)
}

func (m *Migrator) Steps(ctx context.Context, n int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if n == 0 {
		return ErrNoChange
	}

	migs, current, err := m.load(ctx)
	if err != nil {
		return err
	}

	if n > 0 {
		pend := pending(migs, current)
		if len(pend) == 0 {
			return ErrNoChange
		}
		n = min(n, len(pend))
		for _, mig := range pend[:n] {
			if err := m.applyUp(ctx, mig); err != nil {
				return err
			}
		}
		return nil
	}

	applied := appliedAsc(migs, current)
	if len(applied) == 0 {
		return ErrNoChange
	}
	steps := min(-n, len(applied))
	return m.rollback(ctx, applied, len(applied)-steps)
}

func (m *Migrator) Goto(ctx context.Context, version uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	migs, current, err := m.load(ctx)
	if err != nil {
		return err
	}
	if version == current {
		return ErrNoChange
	}
	if version != 0 && !containsVersion(migs, version) {
		return ErrUnknownVersion{Version: version}
	}

	if version > current {
		for _, mig := range migs {
			if mig.Version > current && mig.Version <= version {
				if err := m.applyUp(ctx, mig); err != nil {
					return err
				}
			}
		}
		return nil
	}

	applied := appliedAsc(migs, current)
	startIndex := len(applied)
	for i, mig := range applied {
		if mig.Version > version {
			startIndex = i
			break
		}
	}
	return m.rollback(ctx, applied, startIndex)
}

func (m *Migrator) Force(ctx context.Context, version uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.tracker.ensure(ctx); err != nil {
		return err
	}
	if err := m.tracker.setState(ctx, m.db, version, false); err != nil {
		return err
	}
	if version == 0 {
		return nil
	}
	title := ""
	if migs, err := m.source.Migrations(); err == nil {
		if mig := findVersion(migs, version); mig != nil {
			title = mig.Title
		}
	}
	return m.tracker.appendForce(ctx, m.db, version, title)
}

func (m *Migrator) Version(ctx context.Context) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.tracker.ensure(ctx); err != nil {
		return 0, false, err
	}
	version, dirty, err := m.tracker.current(ctx)
	if err != nil {
		return 0, false, err
	}
	if version == 0 && !dirty {
		return 0, false, ErrNilVersion
	}
	return version, dirty, nil
}

type ChecksumState string

const (
	ChecksumStateOK       ChecksumState = "ok"
	ChecksumStateMismatch ChecksumState = "mismatch"
	ChecksumStateForced   ChecksumState = "forced"
	ChecksumStatePending  ChecksumState = ""
)

type MigrationStatus struct {
	Version      uint64
	Title        string
	Applied      bool
	Irreversible bool
	Checksum     ChecksumState
	AppliedAt    *time.Time
}

func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	migs, err := m.source.Migrations()
	if err != nil {
		return nil, err
	}
	if err := m.tracker.ensure(ctx); err != nil {
		return nil, err
	}
	current, _, err := m.tracker.current(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]MigrationStatus, 0, len(migs))
	for _, mig := range migs {
		st := MigrationStatus{
			Version:      mig.Version,
			Title:        mig.Title,
			Applied:      mig.Version <= current,
			Irreversible: !mig.HasDown,
			Checksum:     ChecksumStatePending,
		}
		if st.Applied {
			rec, err := m.tracker.latestApplied(ctx, mig.Version)
			if err != nil {
				return nil, err
			}
			switch {
			case !rec.found || rec.forced:
				st.Checksum = ChecksumStateForced
			case rec.checksum == mig.UpChecksum:
				st.Checksum = ChecksumStateOK
			default:
				st.Checksum = ChecksumStateMismatch
			}
			if rec.appliedAt.Valid {
				t := rec.appliedAt.Time
				st.AppliedAt = &t
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func containsVersion(migs []*Migration, version uint64) bool {
	return findVersion(migs, version) != nil
}

func findVersion(migs []*Migration, version uint64) *Migration {
	for _, m := range migs {
		if m.Version == version {
			return m
		}
	}
	return nil
}
