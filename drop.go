package duckmigrate

import (
	"context"
	"fmt"
)

func (m *Migrator) Drop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var current string
	if err := m.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&current); err != nil {
		return err
	}

	views, err := m.catalogNames(ctx,
		`SELECT view_name FROM duckdb_views() WHERE NOT internal AND database_name = ? AND schema_name = 'main'`, current)
	if err != nil {
		return err
	}
	tables, err := m.catalogNames(ctx,
		`SELECT table_name FROM duckdb_tables() WHERE NOT internal AND database_name = ? AND schema_name = 'main'`, current)
	if err != nil {
		return err
	}
	sequences, err := m.catalogNames(ctx,
		`SELECT sequence_name FROM duckdb_sequences() WHERE database_name = ? AND schema_name = 'main'`, current)
	if err != nil {
		return err
	}

	for _, v := range views {
		if _, err := m.db.ExecContext(ctx, fmt.Sprintf(`DROP VIEW IF EXISTS "%s" CASCADE`, v)); err != nil {
			return err
		}
	}
	for _, t := range tables {
		if _, err := m.db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, t)); err != nil {
			return err
		}
	}
	for _, s := range sequences {
		if _, err := m.db.ExecContext(ctx, fmt.Sprintf(`DROP SEQUENCE IF EXISTS "%s" CASCADE`, s)); err != nil {
			return err
		}
	}
	m.logger.Info("dropped", "views", len(views), "tables", len(tables), "sequences", len(sequences))
	return nil
}

func (m *Migrator) catalogNames(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
