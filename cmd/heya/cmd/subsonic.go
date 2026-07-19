package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/subsonic"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var subsonicCmd = &cobra.Command{
	Use:   "subsonic",
	Short: "Subsonic-compatible API tools",
}

var subsonicCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Show spec coverage of the Subsonic-compatible API",
	Long: "Triage state of every endpoint in the Subsonic 1.16.1 + OpenSubsonic API: " +
		"implemented (real behavior), stubbed (correct 'feature absent' answers), " +
		"unsupported (answers the in-protocol 'not implemented' error).",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries := subsonic.CoverageReport()

		if ui.JSONMode {
			return ui.OutputJSON(entries)
		}

		counts := map[subsonic.CoverageStatus]int{}
		for _, e := range entries {
			counts[e.Status]++
		}
		ui.Header("Subsonic API coverage")
		fmt.Printf("\nimplemented %d · stubbed %d · unsupported %d · total %d\n\n",
			counts[subsonic.CoverageImplemented], counts[subsonic.CoverageStubbed],
			counts[subsonic.CoverageUnsupported], len(entries))

		verbose, _ := cmd.Flags().GetBool("all")
		t := ui.NewTable("STATUS", "CATEGORY", "ENDPOINT")
		for _, e := range entries {
			if !verbose && e.Status == subsonic.CoverageUnsupported {
				continue
			}
			t.AddRow(string(e.Status), e.Category, e.Endpoint)
		}
		fmt.Println(t.Render())
		if !verbose {
			fmt.Println(ui.Dim("(--all to include unsupported endpoints)"))
		}
		return nil
	},
}

var subsonicCredentialCmd = &cobra.Command{
	Use:   "credential <username>",
	Short: "Show, create, rotate, or revoke a user's Subsonic app password",
	Long: "Subsonic clients authenticate with a dedicated per-user app password (never the Heya " +
		"login password — the protocol's md5 token requires the server to know the secret). " +
		"Without flags, shows the current credential; --rotate mints a new one (invalidating " +
		"configured clients); --revoke deletes it.",
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
				if err := app.RevokeSubsonicCredential(ctx, user.ID); err != nil {
					return err
				}
				ui.Success("Revoked Subsonic credential for %s", user.Username)
				return nil
			case rotate:
				cred, err := app.RotateSubsonicCredential(ctx, user.ID)
				if err != nil {
					return err
				}
				if ui.JSONMode {
					return ui.OutputJSON(cred)
				}
				ui.Success("Rotated Subsonic credential for %s", user.Username)
				fmt.Printf("\n  username:     %s\n  app password: %s\n\n", user.Username, cred.Secret)
				fmt.Println(ui.Dim("Point a Subsonic client at {server} with these credentials."))
				return nil
			default:
				cred, err := app.GetSubsonicCredential(ctx, user.ID)
				if err != nil {
					if errors.Is(err, service.ErrSubsonicNoCredential) {
						ui.Warn("No Subsonic credential for %s — create one with --rotate.", user.Username)
						return nil
					}
					return err
				}
				if ui.JSONMode {
					return ui.OutputJSON(cred)
				}
				fmt.Printf("\n  username:     %s\n  app password: %s\n  rotated:      %s\n",
					user.Username, cred.Secret, cred.RotatedAt.Format("2006-01-02 15:04"))
				if cred.LastUsedAt != nil {
					fmt.Printf("  last used:    %s\n", cred.LastUsedAt.Format("2006-01-02 15:04"))
				} else {
					fmt.Println("  last used:    never")
				}
				fmt.Println()
				return nil
			}
		})
	},
}

func findUserByName(ctx context.Context, app *service.App, username string) (sqlc.User, error) {
	users, err := app.ListUsers(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	for _, u := range users {
		if u.Username == username {
			return u, nil
		}
	}
	return sqlc.User{}, fmt.Errorf("user not found: %s", username)
}

func init() {
	subsonicCoverageCmd.Flags().Bool("all", false, "include unsupported endpoints")
	subsonicCredentialCmd.Flags().Bool("rotate", false, "create or rotate the app password")
	subsonicCredentialCmd.Flags().Bool("revoke", false, "delete the app password")
	subsonicCmd.AddCommand(subsonicCoverageCmd)
	subsonicCmd.AddCommand(subsonicCredentialCmd)
	rootCmd.AddCommand(subsonicCmd)
}
