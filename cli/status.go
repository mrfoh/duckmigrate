package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/mrfoh/duckmigrate"
	"github.com/spf13/cobra"
)

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show each migration and whether it is applied",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withMigrator(func(ctx context.Context, m *duckmigrate.Migrator) error {
				items, err := m.Status(ctx)
				if err != nil {
					return err
				}
				if len(items) == 0 {
					fmt.Fprintln(a.out, "no migrations found")
					return nil
				}
				tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "VERSION\tTITLE\tSTATE\tCHECKSUM\tAPPLIED AT")
				for _, it := range items {
					state := "pending"
					if it.Applied {
						state = "applied"
					}
					if it.Irreversible {
						state += " (no down)"
					}
					appliedAt := ""
					if it.AppliedAt != nil {
						appliedAt = it.AppliedAt.Format("2006-01-02 15:04:05")
					}
					fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", it.Version, it.Title, state, checksumLabel(it.Checksum), appliedAt)
				}
				return tw.Flush()
			})
		},
	}
}

func checksumLabel(s duckmigrate.ChecksumState) string {
	if s == duckmigrate.ChecksumStatePending {
		return "-"
	}
	return string(s)
}
