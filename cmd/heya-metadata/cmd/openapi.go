package cmd

import (
	"fmt"
	"os"

	"github.com/HeyaMedia/HeyaMetadata/internal/buildinfo"
	"github.com/HeyaMedia/HeyaMetadata/internal/server"
	"github.com/spf13/cobra"
)

func newOpenAPICommand() *cobra.Command {
	var output string
	var format string
	var specVersion string

	command := &cobra.Command{
		Use:   "openapi-spec",
		Short: "Render the OpenAPI document",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			document, err := server.OpenAPIDocument(buildinfo.Version, format, specVersion)
			if err != nil {
				return err
			}

			if output == "" || output == "-" {
				_, err = os.Stdout.Write(document)
				return err
			}
			if err := os.WriteFile(output, document, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}
			return nil
		},
	}

	command.Flags().StringVarP(&output, "output", "o", "", "Write to a file instead of stdout")
	command.Flags().StringVar(&format, "format", "json", "Output format: json or yaml")
	command.Flags().StringVar(&specVersion, "version", "3.1", "OpenAPI version: 3.0 or 3.1")
	return command
}
