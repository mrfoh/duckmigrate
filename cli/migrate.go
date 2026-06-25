package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mrfoh/duckmigrate"
	"github.com/spf13/cobra"
)

func (a *app) upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up [N]",
		Short: "Apply all pending migrations, or the next N",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				if len(args) == 1 {
					n, err := strconv.Atoi(args[0])
					if err != nil || n <= 0 {
						return fmt.Errorf("N must be a positive integer")
					}
					return reportNoChange(a, m.Steps(ctx, n))
				}
				return reportNoChange(a, m.Up(ctx))
			})
		},
	}
}

func (a *app) downCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "down [N]",
		Short: "Roll back the last N migrations, or all with -f",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				if len(args) == 1 {
					n, err := strconv.Atoi(args[0])
					if err != nil || n <= 0 {
						return fmt.Errorf("N must be a positive integer")
					}
					return reportNoChange(a, m.Steps(ctx, -n))
				}
				if !force {
					return fmt.Errorf("refusing to roll back all migrations without -f")
				}
				return reportNoChange(a, m.Down(ctx))
			})
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "confirm rolling back all migrations")
	return cmd
}

func (a *app) gotoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "goto <version>",
		Short: "Migrate up or down to a specific version (0 for none)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("version must be a non-negative integer")
			}
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				return reportNoChange(a, m.Goto(ctx, v))
			})
		},
	}
}

func (a *app) forceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force <version>",
		Short: "Set the recorded version and clear the dirty flag without running SQL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("version must be a non-negative integer")
			}
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				if err := m.Force(ctx, v); err != nil {
					return err
				}
				fmt.Fprintf(a.out, "forced version to %d\n", v)
				return nil
			})
		},
	}
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current migration version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				v, dirty, err := m.Version(ctx)
				if errors.Is(err, duckmigrate.ErrNilVersion) {
					fmt.Fprintln(a.out, "no migrations applied")
					return nil
				}
				if err != nil {
					return err
				}
				if dirty {
					fmt.Fprintf(a.out, "%d (dirty)\n", v)
				} else {
					fmt.Fprintf(a.out, "%d\n", v)
				}
				return nil
			})
		},
	}
}

func (a *app) dropCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "drop",
		Short: "Drop every table, view, and sequence in the main schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return fmt.Errorf("refusing to drop the database without -f")
			}
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				if err := m.Drop(ctx); err != nil {
					return err
				}
				fmt.Fprintln(a.out, "dropped all objects in main schema")
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "confirm dropping the database")
	return cmd
}

func reportNoChange(a *app, err error) error {
	if errors.Is(err, duckmigrate.ErrNoChange) {
		fmt.Fprintln(a.out, "no change")
		return nil
	}
	return err
}
