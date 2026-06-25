package duckmigrate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func noTxDirective(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "duckmigrate:no-transaction")
}

func (m *Migrator) load(ctx context.Context) (migs []*Migration, current uint64, err error) {
	migs, err = m.source.Migrations()
	if err != nil {
		return nil, 0, err
	}
	if err := m.tracker.ensure(ctx); err != nil {
		return nil, 0, err
	}
	current, dirty, err := m.tracker.current(ctx)
	if err != nil {
		return nil, 0, err
	}
	if dirty {
		return nil, 0, ErrDirty{Version: current}
	}
	if err := m.validateChecksums(ctx, migs, current); err != nil {
		return nil, 0, err
	}
	return migs, current, nil
}

func (m *Migrator) validateChecksums(ctx context.Context, migs []*Migration, current uint64) error {
	if m.cfg.checksumMode == ChecksumOff {
		return nil
	}
	for _, mig := range migs {
		if mig.Version > current {
			continue
		}
		rec, err := m.tracker.latestApplied(ctx, mig.Version)
		if err != nil {
			return err
		}
		if !rec.found || rec.forced {
			continue
		}
		if rec.checksum != mig.UpChecksum {
			if m.cfg.checksumMode == ChecksumWarn {
				m.logger.Warn("checksum mismatch", "version", mig.Version, "title", mig.Title)
				continue
			}
			return ErrChecksumMismatch{Version: mig.Version, Title: mig.Title}
		}
	}
	return nil
}

func (m *Migrator) applyUp(ctx context.Context, mig *Migration) error {
	m.logger.Info("applying", "direction", "up", "version", mig.Version, "title", mig.Title)
	start := time.Now()
	useTx := m.cfg.transactions && !noTxDirective(mig.UpSQL)
	sum := mig.UpChecksum

	if useTx {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, mig.UpSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("duckmigrate: applying up %d_%s: %w", mig.Version, mig.Title, err)
		}
		if err := m.tracker.setState(ctx, tx, mig.Version, false); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := m.tracker.appendHistory(ctx, tx, mig.Version, mig.Title, DirectionUp, &sum, time.Since(start).Milliseconds()); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	if err := m.tracker.setState(ctx, m.db, mig.Version, true); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, mig.UpSQL); err != nil {
		return fmt.Errorf("duckmigrate: applying up %d_%s (left dirty): %w", mig.Version, mig.Title, err)
	}
	if err := m.tracker.setState(ctx, m.db, mig.Version, false); err != nil {
		return err
	}
	return m.tracker.appendHistory(ctx, m.db, mig.Version, mig.Title, DirectionUp, &sum, time.Since(start).Milliseconds())
}

func (m *Migrator) applyDown(ctx context.Context, mig *Migration, target uint64) error {
	if !mig.HasDown {
		return ErrIrreversibleMigration{Version: mig.Version, Title: mig.Title}
	}
	m.logger.Info("applying", "direction", "down", "version", mig.Version, "title", mig.Title)
	start := time.Now()
	useTx := m.cfg.transactions && !noTxDirective(mig.DownSQL)

	if useTx {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, mig.DownSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("duckmigrate: applying down %d_%s: %w", mig.Version, mig.Title, err)
		}
		if err := m.tracker.setState(ctx, tx, target, false); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := m.tracker.appendHistory(ctx, tx, mig.Version, mig.Title, DirectionDown, nil, time.Since(start).Milliseconds()); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	if err := m.tracker.setState(ctx, m.db, mig.Version, true); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, mig.DownSQL); err != nil {
		return fmt.Errorf("duckmigrate: applying down %d_%s (left dirty): %w", mig.Version, mig.Title, err)
	}
	if err := m.tracker.setState(ctx, m.db, target, false); err != nil {
		return err
	}
	return m.tracker.appendHistory(ctx, m.db, mig.Version, mig.Title, DirectionDown, nil, time.Since(start).Milliseconds())
}

func pending(migs []*Migration, current uint64) []*Migration {
	var out []*Migration
	for _, m := range migs {
		if m.Version > current {
			out = append(out, m)
		}
	}
	return out
}

func appliedAsc(migs []*Migration, current uint64) []*Migration {
	var out []*Migration
	for _, m := range migs {
		if m.Version <= current {
			out = append(out, m)
		}
	}
	return out
}

func (m *Migrator) rollback(ctx context.Context, applied []*Migration, startIndex int) error {
	for i := startIndex; i < len(applied); i++ {
		if !applied[i].HasDown {
			return ErrIrreversibleMigration{Version: applied[i].Version, Title: applied[i].Title}
		}
	}
	for i := len(applied) - 1; i >= startIndex; i-- {
		target := uint64(0)
		if i > 0 {
			target = applied[i-1].Version
		}
		if err := m.applyDown(ctx, applied[i], target); err != nil {
			return err
		}
	}
	return nil
}
