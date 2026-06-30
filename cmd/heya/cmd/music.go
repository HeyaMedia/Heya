package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var musicCmd = &cobra.Command{
	Use:   "music",
	Short: "Music library maintenance",
}

var musicSplitArtistCmd = &cobra.Command{
	Use:   "split-artist <artist-id> <folder>",
	Short: "Split a folder's albums out of an artist (undo a bad enrichment merge)",
	Long: `Move every album of <artist-id> whose files live under <folder> into its
own artist row, then queue that artist for re-enrichment.

Use the folder name exactly as printed by scripts/diagnose-collab-dupes.sql,
e.g. to lift Avicii's discography back out of artist 123 (Alicia Keys):

    heya music split-artist 123 "Avicii"

Idempotent — re-running once the albums have moved is a no-op.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		artistID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid artist id %q: %w", args[0], err)
		}
		folder := args[1]

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		res, err := app.SplitArtist(ctx, artistID, folder)
		if err != nil {
			return err
		}
		if res.AlbumsMoved == 0 {
			ui.Warn("No albums of artist %d live under folder %q — nothing to split.", artistID, folder)
			return nil
		}
		ui.Success("Moved %d album(s) out of artist %d into %q (artist %d); queued for re-enrichment.",
			res.AlbumsMoved, artistID, res.NewArtistName, res.NewArtistID)
		return nil
	},
}

func init() {
	musicCmd.AddCommand(musicSplitArtistCmd)
}
