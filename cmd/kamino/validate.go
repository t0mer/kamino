package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/t0mer/kamino/internal/manifest"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Fetch and validate the config repo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			resolved, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}

			if resolved.Stale {
				fmt.Fprintf(out, "warning: stale config (SHA %s, fetched %s)\n",
					resolved.SHA, resolved.FetchedAt.Format("2006-01-02T15:04Z"))
			}

			problems := manifest.Validate(resolved)
			for _, p := range problems.Warnings() {
				fmt.Fprintf(out, "warning: %s\n", p)
			}
			for _, p := range problems.Errors() {
				fmt.Fprintf(out, "error: %s\n", p)
			}
			if problems.HasErrors() {
				return fmt.Errorf("config repo has %d error(s)", len(problems.Errors()))
			}

			fmt.Fprintf(out, "%s: %d categories, %d profiles, SHA %s — OK\n",
				resolved.Manifest.Name, len(resolved.Categories), len(resolved.Profiles), resolved.SHA)
			return nil
		},
	}
}
