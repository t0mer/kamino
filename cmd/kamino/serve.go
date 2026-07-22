package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/t0mer/kamino/internal/config"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/metrics"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/server"
	"github.com/t0mer/kamino/internal/state"
)

// DefaultListen binds loopback only. This API installs software as root, so
// reaching the network is an explicit decision, never a default.
const DefaultListen = "127.0.0.1:8844"

func newServeCmd() *cobra.Command {
	var listen string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the HTTP API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, listen)
		},
	}

	cmd.Flags().StringVar(&listen, "listen", DefaultListen, "address to listen on")
	return cmd
}

func runServe(cmd *cobra.Command, listen string) error {
	out := cmd.OutOrStdout()

	token, generated, err := ensureAPIToken(flags.dataDir)
	if err != nil {
		return err
	}

	db, err := state.Open(filepath.Join(flags.dataDir, "kamino.db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	bus := events.NewBus(events.DefaultBuffer)
	defer bus.Close()

	mx := metrics.New()

	srv := server.New(server.Deps{
		DB:           db,
		Bus:          bus,
		Runs:         runmgr.New(db, bus, DefaultKeepRuns).WithMetrics(mx),
		DataDir:      flags.dataDir,
		APIToken:     token,
		Metrics:      mx.Handler(),
		ConfigSource: configSource(cmd.Context()),
		LoadConfig: func(ctx context.Context) (*manifest.Resolved, error) {
			return loadConfig(ctx)
		},
	})

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if !isLoopbackAddr(listen) {
		slog.Warn("listening on a non-loopback address: this API installs software as root",
			"listen", listen)
	}

	fmt.Fprintf(out, "kamino listening on http://%s\n", listen)
	if generated {
		fmt.Fprintf(out, "\nAPI token (shown once, stored in %s):\n\n    %s\n\n",
			filepath.Join(flags.dataDir, config.SettingsFile), token)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serving: %w", err)
		}
		return nil
	case <-ctx.Done():
		fmt.Fprintln(out, "\nshutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// ensureAPIToken returns the persisted API token, generating and saving one on
// first use. generated reports whether it was created now, so the caller can
// print it once — it is never retrievable from the API afterwards.
func ensureAPIToken(dataDir string) (token string, generated bool, err error) {
	saved, err := config.Load(dataDir)
	if err != nil {
		return "", false, err
	}
	if saved.APIToken != "" {
		return saved.APIToken, false, nil
	}

	token, err = config.GenerateAPIToken()
	if err != nil {
		return "", false, err
	}
	saved.APIToken = token
	if err := config.Save(dataDir, saved); err != nil {
		return "", false, err
	}
	return token, true, nil
}

// isLoopbackAddr reports whether addr binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
