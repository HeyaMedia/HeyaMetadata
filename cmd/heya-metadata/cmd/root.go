package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/buildinfo"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

var (
	cfg config.Config

	jsonOutput bool
	noColor    bool
	logLevel   string
	logFormat  string
)

var rootCmd = &cobra.Command{
	Use:           "heya-metadata",
	Short:         "Canonical metadata for Heya",
	Long:          "Heya Metadata gathers, normalizes, and serves canonical metadata for Heya media servers.",
	Version:       buildinfo.Version,
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		ui.Init(jsonOutput, noColor)

		if err := config.LoadEnvFiles(); err != nil {
			return err
		}
		loaded, err := config.Load()
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("log-level") {
			loaded.LogLevel = logLevel
		}
		if cmd.Flags().Changed("log-format") {
			loaded.LogFormat = logFormat
		}
		if err := configureLogger(loaded.LogLevel, loaded.LogFormat); err != nil {
			return err
		}
		cfg = loaded
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Print(ui.HelpBanner(buildinfo.Version))
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		ui.Error("%v", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON when supported")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Log level: debug, info, warn, or error")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", "Log format: text or json")

	rootCmd.AddCommand(newServeCommand())
	rootCmd.AddCommand(newDevProxyCommand())
	rootCmd.AddCommand(newMigrateCommand())
	rootCmd.AddCommand(newWorkerCommand())
	rootCmd.AddCommand(newSmokeCommand())
	rootCmd.AddCommand(newMovieCommand())
	rootCmd.AddCommand(newArtistCommand())
	rootCmd.AddCommand(newReleaseGroupCommand())
	rootCmd.AddCommand(newReleaseCommand())
	rootCmd.AddCommand(newRetentionCommand())
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newOpenAPICommand())
	rootCmd.AddCommand(newProviderCommand())
	rootCmd.AddCommand(newDiscoverCommand())
}

func configureLogger(levelName, format string) error {
	var level slog.Level
	switch strings.ToLower(levelName) {
	case "debug":
		level = slog.LevelDebug
	case "", "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return fmt.Errorf("unknown log level %q", levelName)
	}

	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch strings.ToLower(format) {
	case "", "text", "console":
		handler = slog.NewTextHandler(os.Stderr, options)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, options)
	default:
		return fmt.Errorf("unknown log format %q", format)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}
