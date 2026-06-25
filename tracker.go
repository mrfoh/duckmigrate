package duckmigrate

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
)

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type tracker struct {
	db      *sql.DB
	state   string
	history string
	seq     string
}

func newTracker(db *sql.DB, base string) (*tracker, error) {
	if !identifierPattern.MatchString(base) {
		return nil, fmt.Errorf("duckmigrate: invalid table name %q", base)
	}
	return &tracker{
		db:      db,
		state:   base,
		history: base + "_history",
		seq:     base + "_history_seq",
	}, nil
}

func (t *tracker) ensure(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (version BIGINT, dirty BOOLEAN)`, t.state),
		fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS %s START 1`, t.seq),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			seq BIGINT PRIMARY KEY DEFAULT nextval('%s'),
			version BIGINT NOT NULL,
			title VARCHAR NOT NULL,
			direction VARCHAR NOT NULL,
			checksum VARCHAR,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			execution_ms BIGINT NOT NULL
		)`, t.history, t.seq),
	}
	for _, s := range stmts {
		if _, err := t.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("duckmigrate: ensuring tracking tables: %w", err)
		}
	}
	return nil
}

func (t *tracker) current(ctx context.Context) (version uint64, dirty bool, err error) {
	row := t.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT version, dirty FROM %s LIMIT 1`, t.state))
	var v sql.NullInt64
	var d sql.NullBool
	switch err := row.Scan(&v, &d); err {
	case sql.ErrNoRows:
		return 0, false, nil
	case nil:
		return uint64(v.Int64), d.Bool, nil
	default:
		return 0, false, err
	}
}

func (t *tracker) setState(ctx context.Context, x execer, version uint64, dirty bool) error {
	if _, err := x.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s`, t.state)); err != nil {
		return err
	}
	if version == 0 && !dirty {
		return nil
	}
	_, err := x.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (version, dirty) VALUES (?, ?)`, t.state), int64(version), dirty)
	return err
}

func (t *tracker) appendHistory(ctx context.Context, x execer, version uint64, title string, dir Direction, sum *string, execMS int64) error {
	_, err := x.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (version, title, direction, checksum, execution_ms) VALUES (?, ?, ?, ?, ?)`, t.history),
		int64(version), title, string(dir), sum, execMS)
	return err
}

func (t *tracker) appendForce(ctx context.Context, x execer, version uint64, title string) error {
	_, err := x.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (version, title, direction, checksum, execution_ms) VALUES (?, ?, 'force', NULL, 0)`, t.history),
		int64(version), title)
	return err
}

type appliedRecord struct {
	checksum  string
	forced    bool
	appliedAt sql.NullTime
	found     bool
}

func (t *tracker) latestApplied(ctx context.Context, version uint64) (appliedRecord, error) {
	row := t.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT direction, checksum, applied_at FROM %s WHERE version = ? ORDER BY seq DESC LIMIT 1`, t.history),
		int64(version))
	var dir string
	var sum sql.NullString
	var at sql.NullTime
	switch err := row.Scan(&dir, &sum, &at); err {
	case sql.ErrNoRows:
		return appliedRecord{}, nil
	case nil:
		return appliedRecord{
			checksum:  sum.String,
			forced:    dir == "force" || !sum.Valid,
			appliedAt: at,
			found:     true,
		}, nil
	default:
		return appliedRecord{}, err
	}
}
