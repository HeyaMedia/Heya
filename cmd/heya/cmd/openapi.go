package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/karbowiak/heya/internal/server"
	"github.com/karbowiak/heya/internal/service"
	"github.com/spf13/cobra"
)

var openapiOutput string
var openapiFormat string
var openapiVersion string

var openapiCmd = &cobra.Command{
	Use:   "openapi-spec",
	Short: "Dump the generated OpenAPI document",
	Long: `Dumps the OpenAPI spec for the Heya API without booting a server or
touching the database.

The spec is built by registering every Huma operation against a throwaway
mux and serializing the resulting OpenAPI document — handler closures are
never invoked, so no database connection is needed. The output feeds the
TypeScript client generator: see web/shared/types/api.gen.ts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Empty app: registration just captures &service.App{} in handler
		// closures. Spec generation reads operation metadata via reflection
		// (input types, output types, tags) — closures are never called,
		// so a zero-valued app is safe here.
		mux := http.NewServeMux()
		api := server.BuildAPI(mux, &service.App{}, cfg)

		var out []byte
		var err error
		switch openapiFormat {
		case "json":
			if openapiVersion == "3.0" {
				out, err = api.OpenAPI().Downgrade()
			} else {
				out, err = json.MarshalIndent(api.OpenAPI(), "", "  ")
			}
		case "yaml":
			if openapiVersion == "3.0" {
				out, err = api.OpenAPI().DowngradeYAML()
			} else {
				out, err = api.OpenAPI().YAML()
			}
		default:
			return fmt.Errorf("unknown --format %q (want json or yaml)", openapiFormat)
		}
		if err != nil {
			return fmt.Errorf("marshal openapi: %w", err)
		}

		if openapiOutput == "" || openapiOutput == "-" {
			_, _ = os.Stdout.Write(out)
			if len(out) > 0 && out[len(out)-1] != '\n' {
				_, _ = os.Stdout.Write([]byte("\n"))
			}
			return nil
		}
		// OpenAPI spec is non-secret machine-readable output checked into the
		// repo — 0o644 is appropriate.
		if err := os.WriteFile(openapiOutput, out, 0o644); err != nil { //nolint:gosec // G306: spec file is intentionally world-readable
			return fmt.Errorf("write %s: %w", openapiOutput, err)
		}
		return nil
	},
}

func init() {
	openapiCmd.Flags().StringVarP(&openapiOutput, "output", "o", "", "Write spec to file (default stdout)")
	openapiCmd.Flags().StringVar(&openapiFormat, "format", "json", "Output format: json or yaml")
	openapiCmd.Flags().StringVar(&openapiVersion, "version", "3.1", "OpenAPI version: 3.0 or 3.1")
	rootCmd.AddCommand(openapiCmd)
}
