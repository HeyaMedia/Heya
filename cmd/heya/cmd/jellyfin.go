package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/karbowiak/heya/internal/jellyfin"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var jellyfinCmd = &cobra.Command{
	Use:   "jellyfin",
	Short: "Jellyfin-compatible API tools",
}

var jellyfinCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Show spec coverage of the Jellyfin-compatible API",
	Long: "Triage state of every operation in the vendored Jellyfin OpenAPI spec: " +
		"implemented (real behavior), stubbed (correct 'feature off' answers), " +
		"planned (future work), out_of_scope (no Heya equivalent).",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries := jellyfin.CoverageReport()

		if ui.JSONMode {
			return ui.OutputJSON(entries)
		}

		counts := map[jellyfin.CoverageStatus]int{}
		for _, e := range entries {
			counts[e.Status]++
		}
		ui.Header("Jellyfin API coverage")
		fmt.Printf("\nimplemented %d · stubbed %d · planned %d · out of scope %d · total %d\n\n",
			counts[jellyfin.CoverageImplemented], counts[jellyfin.CoverageStubbed],
			counts[jellyfin.CoveragePlanned], counts[jellyfin.CoverageOutOfScope], len(entries))

		verbose, _ := cmd.Flags().GetBool("all")
		t := ui.NewTable("STATUS", "TAG", "OPERATION")
		for _, e := range entries {
			if !verbose && e.Status != jellyfin.CoverageImplemented && e.Status != jellyfin.CoverageStubbed {
				continue
			}
			t.AddRow(string(e.Status), e.Tag, e.Operation)
		}
		fmt.Println(t.Render())
		if !verbose {
			fmt.Println(ui.Dim("(--all to include planned / out-of-scope operations)"))
		}
		return nil
	},
}

var jellyfinCredentialCmd = &cobra.Command{
	Use:   "credential <username>",
	Short: "Show, create, rotate, or revoke a user's Jellyfin PIN",
	Long: "Jellyfin clients can sign in with the normal account password, or with a short " +
		"server-generated PIN that works ONLY on the Jellyfin API — built for TV remotes. " +
		"Without flags, shows the current PIN; --rotate mints a new one (the old PIN stops " +
		"working, existing sessions stay); --revoke deletes it.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := args[0]
		rotate, _ := cmd.Flags().GetBool("rotate")
		revoke, _ := cmd.Flags().GetBool("revoke")
		if rotate && revoke {
			return fmt.Errorf("--rotate and --revoke are mutually exclusive")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			user, err := findUserByName(ctx, app, username)
			if err != nil {
				return err
			}

			switch {
			case revoke:
				if err := app.RevokeJellyfinCredential(ctx, user.ID); err != nil {
					return err
				}
				ui.Success("Revoked Jellyfin PIN for %s — the account password still signs in", user.Username)
				return nil
			case rotate:
				cred, err := app.RotateJellyfinCredential(ctx, user.ID)
				if err != nil {
					return err
				}
				if ui.JSONMode {
					return ui.OutputJSON(cred)
				}
				ui.Success("Rotated Jellyfin PIN for %s", user.Username)
				fmt.Printf("\n  username: %s\n  PIN:      %s\n\n", user.Username, cred.PIN)
				fmt.Println(ui.Dim("Sign in to any Jellyfin client with this username + PIN."))
				return nil
			default:
				cred, err := app.GetJellyfinCredential(ctx, user.ID)
				if err != nil {
					if errors.Is(err, service.ErrJellyfinNoCredential) {
						ui.Warn("No Jellyfin PIN for %s — create one with --rotate.", user.Username)
						return nil
					}
					return err
				}
				if ui.JSONMode {
					return ui.OutputJSON(cred)
				}
				fmt.Printf("\n  username: %s\n  PIN:      %s\n  rotated:  %s\n",
					user.Username, cred.PIN, cred.RotatedAt.Format("2006-01-02 15:04"))
				if cred.LastUsedAt != nil {
					fmt.Printf("  last used: %s\n", cred.LastUsedAt.Format("2006-01-02 15:04"))
				} else {
					fmt.Println("  last used: never")
				}
				fmt.Println()
				return nil
			}
		})
	},
}

func init() {
	jellyfinCoverageCmd.Flags().Bool("all", false, "include planned and out-of-scope operations")
	jellyfinCredentialCmd.Flags().Bool("rotate", false, "create or rotate the PIN")
	jellyfinCredentialCmd.Flags().Bool("revoke", false, "delete the PIN")
	jellyfinCmd.AddCommand(jellyfinCoverageCmd)
	jellyfinCmd.AddCommand(jellyfinCredentialCmd)
	rootCmd.AddCommand(jellyfinCmd)
}
