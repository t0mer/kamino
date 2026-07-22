package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// globalFlags holds settings shared by every subcommand.
type globalFlags struct {
	logLevel  string
	logFormat string
	dataDir   string
	repo      string
	ref       string
	token     string
	rawBase   string
	configDir string
}

var flags globalFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "kamino",
		Short:         "Provision Ubuntu servers from a versioned config repo",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return setupLogging(flags.logLevel, flags.logFormat)
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&flags.logLevel, "log-level", "info", "log level: debug, info, warning, error")
	pf.StringVar(&flags.logFormat, "log-format", "auto",
		"diagnostic log format: json, text, or auto (text on an interactive terminal, json otherwise)")
	pf.StringVar(&flags.dataDir, "data-dir", "/var/lib/kamino", "data directory")
	pf.StringVar(&flags.repo, "repo", "", "config repo URL (overrides saved settings)")
	pf.StringVar(&flags.ref, "ref", "", "config repo branch, tag or SHA (default main)")
	pf.StringVar(&flags.token, "token", "", "access token for a private config repo")
	pf.StringVar(&flags.rawBase, "raw-base", "", "raw base URL template with {ref} and {path} placeholders")
	pf.StringVar(&flags.configDir, "config-dir", "", "read config from a local directory instead of a repo")
	_ = pf.MarkHidden("config-dir")

	cmd.AddCommand(
		newVersionCmd(),
		newValidateCmd(),
		newPlanCmd(),
		newApplyCmd(),
		newServeCmd(),
	)
	return cmd
}

// setupLogging configures the process-wide slog default logger from the
// --log-level and --log-format flags. Diagnostics always go to stderr,
// deliberately separate from the human-facing plan/progress output commands
// write to cmd.OutOrStdout (renderPlanText, termSink, …) — that separation is
// what keeps `apply --dry-run`'s output script-friendly whichever log format
// is in effect; only what goes through slog is affected by this function.
func setupLogging(level, format string) error {
	l, err := parseLogLevel(level)
	if err != nil {
		return err
	}
	handler, err := newLogHandler(os.Stderr, format, l)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(handler))
	return nil
}

// parseLogLevel maps the --log-level flag's accepted values to a slog.Level.
func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warning", "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", level)
	}
}

// newLogHandler builds the slog handler for --log-format, writing to w.
//
// "json" and "text" are explicit. "auto" (the default) picks text when w is
// an interactive terminal and json otherwise: Kamino runs both ways —
// interactively via `sudo ./kamino` at a shell, and headlessly from
// cloud-init/systemd where stderr is redirected to a file or a log
// collector. A human at a terminal wants readable text; a supervisor
// capturing stderr to a file wants structured, greppable/parseable JSON
// without having to know to pass a flag for it. Either can still be forced
// explicitly with --log-format.
func newLogHandler(w io.Writer, format string, level slog.Level) (slog.Handler, error) {
	opts := &slog.HandlerOptions{Level: level}
	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	case "text":
		return slog.NewTextHandler(w, opts), nil
	case "auto", "":
		if isTerminal(w) {
			return slog.NewTextHandler(w, opts), nil
		}
		return slog.NewJSONHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("invalid log format %q: want json, text, or auto", format)
	}
}

// isTerminal reports whether w is an interactive terminal. Only *os.File can
// be a terminal; any other io.Writer (a bytes.Buffer in a test, a pipe) is
// never one.
//
// A character device is a good enough proxy here without pulling in a
// dependency: Kamino refuses to run on anything but Ubuntu, so the Windows and
// cygwin cases a portable isatty handles are unreachable.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
