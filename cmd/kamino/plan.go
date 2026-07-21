package main

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
)

func newPlanCmd() *cobra.Command {
	var (
		profileID string
		arch      string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Resolve a profile into an ordered install plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}

			if problems := manifest.Validate(resolved); problems.HasErrors() {
				return fmt.Errorf("config repo is invalid:\n%s", problems.Error())
			}

			p, err := findProfile(resolved, profileID)
			if err != nil {
				return err
			}

			built, err := plan.Build(resolved, p, arch)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(built)
			}
			renderPlanText(cmd.OutOrStdout(), built)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileID, "profile", "", "profile to resolve (required)")
	cmd.Flags().StringVar(&arch, "arch", runtime.GOARCH, "target architecture")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the plan as JSON")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}

// renderPlanText prints a human-readable plan. Every command that will run as
// root is shown, because the operator is approving arbitrary code from their
// own repo and deserves to see it first.
func renderPlanText(w io.Writer, p *plan.Plan) {
	if p.Stale {
		fmt.Fprintf(w, "warning: stale config\n\n")
	}
	fmt.Fprintf(w, "profile %s on %s (config SHA %s)\n\n", p.ProfileID, p.Arch, p.ConfigSHA)

	for i, s := range p.Steps {
		suffix := ""
		if s.Implicit {
			suffix = "  (implicit dependency)"
		}
		version := s.Version
		if version != "" {
			version = " " + version
		}
		fmt.Fprintf(w, "%2d. %-24s %s%s [%s]%s\n", i+1, s.Ref, s.Name, version, s.Type, suffix)

		for _, line := range s.Item.PreInstall {
			fmt.Fprintf(w, "      pre:  %s\n", line)
		}
		for _, line := range s.Item.PostInstall {
			fmt.Fprintf(w, "      post: %s\n", line)
		}
	}

	if len(p.Warnings) > 0 {
		fmt.Fprintln(w)
		for _, warning := range p.Warnings {
			fmt.Fprintf(w, "warning: %s\n", warning)
		}
	}
	fmt.Fprintf(w, "\n%d step(s)\n", len(p.Steps))
}
