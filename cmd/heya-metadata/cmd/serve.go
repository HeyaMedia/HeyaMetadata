package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/buildinfo"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/server"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var host string
	var port int

	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("host") {
				cfg.Host = host
			}
			if cmd.Flags().Changed("port") {
				if port < 1 || port > 65535 {
					return fmt.Errorf("port must be between 1 and 65535")
				}
				cfg.Port = port
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			runtime, err := platform.Open(ctx, cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()

			application := server.NewWithRuntimeContext(ctx, buildinfo.Version, runtime)
			handler := application.Handler()
			if cfg.WebRoot != "" {
				handler, err = server.WithWebUI(handler, cfg.WebRoot, runtime, cfg.SiteURL)
				if err != nil {
					return fmt.Errorf("configure web UI: %w", err)
				}
				slog.Info("web UI enabled", "root", cfg.WebRoot, "site_url", cfg.SiteURL)
			}
			httpServer := &http.Server{
				Addr:              cfg.Address(),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				IdleTimeout:       2 * time.Minute,
			}

			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpServer.Addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", httpServer.Addr, err)
			}

			errorsCh := make(chan error, 1)
			go func() {
				slog.Info("server started",
					"address", httpServer.Addr,
					"docs", fmt.Sprintf("http://localhost:%d/api/docs", cfg.Port),
				)
				errorsCh <- httpServer.Serve(listener)
			}()

			select {
			case err := <-errorsCh:
				if !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			case <-ctx.Done():
				slog.Info("shutting down")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown: %w", err)
			}
			return nil
		},
	}

	command.Flags().StringVar(&host, "host", "", "Listen host (overrides HEYA_METADATA_HOST)")
	command.Flags().IntVar(&port, "port", 0, "Listen port (overrides HEYA_METADATA_PORT)")
	return command
}
