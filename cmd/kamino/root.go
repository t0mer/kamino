package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// globalFlags holds settings shared by every subcommand.
type globalFlags struct {
	logLevel string
	dataDir  string
	repo     string
	ref      string
	token    string
}

var flags globalFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "kamino",
		Short:         "Provision Ubuntu servers from a versioned config repo",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return setupLogging(flags.logLevel)
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&flags.logLevel, "log-level", "info", "log level: debug, info, warning, error")
	pf.StringVar(&flags.dataDir, "data-dir", "/var/lib/kamino", "data directory")
	pf.StringVar(&flags.repo, "repo", "", "config repo URL (overrides saved settings)")
	pf.StringVar(&flags.ref, "ref", "", "config repo branch, tag or SHA (default main)")
	pf.StringVar(&flags.token, "token", "", "access token for a private config repo")

	cmd.AddCommand(newVersionCmd())
	return cmd
}

func setupLogging(level string) error {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warning", "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		return fmt.Errorf("invalid log level %q", level)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
	return nil
}
