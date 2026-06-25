package duckmigrate

import (
	"errors"
	"fmt"
)

var (
	ErrNoChange   = errors.New("duckmigrate: no change")
	ErrNilVersion = errors.New("duckmigrate: no migration applied")
	ErrNoSource   = errors.New("duckmigrate: no migration source configured")
	ErrNoDatabase = errors.New("duckmigrate: no database configured")
)

type ErrDirty struct {
	Version uint64
}

func (e ErrDirty) Error() string {
	return fmt.Sprintf("duckmigrate: database is dirty at version %d; run `force %d` after fixing it", e.Version, e.Version)
}

type ErrChecksumMismatch struct {
	Version uint64
	Title   string
}

func (e ErrChecksumMismatch) Error() string {
	return fmt.Sprintf("duckmigrate: checksum mismatch for applied migration %d_%s (the .up.sql file changed since it was applied)", e.Version, e.Title)
}

type ErrIrreversibleMigration struct {
	Version uint64
	Title   string
}

func (e ErrIrreversibleMigration) Error() string {
	return fmt.Sprintf("duckmigrate: migration %d_%s has no .down.sql file and cannot be rolled back", e.Version, e.Title)
}

type ErrDuplicateVersion struct {
	Version   uint64
	Direction Direction
}

func (e ErrDuplicateVersion) Error() string {
	return fmt.Sprintf("duckmigrate: duplicate %s migration for version %d", e.Direction, e.Version)
}

type ErrLockTimeout struct {
	cause error
}

func (e ErrLockTimeout) Error() string {
	return fmt.Sprintf("duckmigrate: database is locked by another process: %v", e.cause)
}

func (e ErrLockTimeout) Unwrap() error { return e.cause }

type ErrUnknownVersion struct {
	Version uint64
}

func (e ErrUnknownVersion) Error() string {
	return fmt.Sprintf("duckmigrate: version %d does not exist in the migration source", e.Version)
}
