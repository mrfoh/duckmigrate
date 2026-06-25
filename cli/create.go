package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func (a *app) createCmd() *cobra.Command {
	var seq bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create up/down migration files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := a.nextVersion(seq)
			if err != nil {
				return err
			}
			title := slugify(args[0])
			base := fmt.Sprintf("%s_%s", version, title)
			if err := os.MkdirAll(a.path, 0o755); err != nil {
				return err
			}
			for _, dir := range []string{"up", "down"} {
				name := filepath.Join(a.path, base+"."+dir+".sql")
				if err := os.WriteFile(name, nil, 0o644); err != nil {
					return err
				}
				fmt.Fprintln(a.out, name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&seq, "seq", false, "use a zero-padded sequential version instead of a timestamp")
	return cmd
}

func (a *app) nextVersion(seq bool) (string, error) {
	if !seq {
		return time.Now().UTC().Format("20060102150405"), nil
	}
	entries, err := os.ReadDir(a.path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var max uint64
	re := regexp.MustCompile(`^(\d+)_`)
	for _, e := range entries {
		if m := re.FindStringSubmatch(e.Name()); m != nil {
			var n uint64
			fmt.Sscanf(m[1], "%d", &n)
			if n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%06d", max+1), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}
