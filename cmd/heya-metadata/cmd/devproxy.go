package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/signal"
	"syscall"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/devproxy"
	"github.com/spf13/cobra"
)

func newDevProxyCommand() *cobra.Command {
	var host string
	var port int
	var backend string
	var frontend string

	command := &cobra.Command{
		Use:    "dev-proxy",
		Short:  "Run the stable frontend/API development proxy",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			backendURL, err := parseDevUpstream("backend", backend)
			if err != nil {
				return err
			}
			frontendURL, err := parseDevUpstream("frontend", frontend)
			if err != nil {
				return err
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("port must be between 1 and 65535")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
			server := &http.Server{
				Addr:              address,
				Handler:           devproxy.New(backendURL, frontendURL, slog.Default()),
				ReadHeaderTimeout: 10 * time.Second,
				IdleTimeout:       2 * time.Minute,
			}
			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", address, err)
			}

			errorsCh := make(chan error, 1)
			go func() {
				slog.Info("development proxy started",
					"address", address,
					"backend", backendURL.String(),
					"frontend", frontendURL.String(),
				)
				errorsCh <- server.Serve(listener)
			}()

			select {
			case err := <-errorsCh:
				if !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			case <-ctx.Done():
				slog.Info("development proxy shutting down")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown development proxy: %w", err)
			}
			return nil
		},
	}

	command.Flags().StringVar(&host, "host", "127.0.0.1", "Public proxy listen host")
	command.Flags().IntVar(&port, "port", 3030, "Public proxy listen port")
	command.Flags().StringVar(&backend, "backend", "http://127.0.0.1:3031", "Go API origin")
	command.Flags().StringVar(&frontend, "frontend", "http://127.0.0.1:3032", "Nuxt origin")
	return command
}

func parseDevUpstream(name, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s URL: %w", name, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute HTTP(S) URL", name)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s URL must not contain a query or fragment", name)
	}
	return parsed, nil
}
