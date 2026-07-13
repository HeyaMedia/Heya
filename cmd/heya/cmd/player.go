package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// heya player — inspect and drive the server-owned play queue
// (docs/queue-plan.md). Thin HTTP client against the running server (the
// cast.go pattern): the queue lives in the serve process, so the CLI
// mutates it through the same /api/me/queue surface the web UI uses.
// (`heya queue` is taken by the River job queue.)

var playerCmd = &cobra.Command{
	Use:   "player",
	Short: "Inspect and control the play queue",
}

var (
	playerShuffleFlag bool
	playerAlbumFlag   int64
	playerArtistFlag  int64
	playerGenreFlag   string
	playerLimitFlag   int
)

type playerItemJSON struct {
	ItemID     int64  `json:"item_id"`
	TrackID    int64  `json:"track_id"`
	Title      string `json:"title"`
	ArtistName string `json:"artist_name"`
	AlbumTitle string `json:"album_title"`
	Duration   int32  `json:"duration"`
}

type playerViewJSON struct {
	Version          int64            `json:"version"`
	CurrentItemID    int64            `json:"current_item_id"`
	CurrentIndex     int64            `json:"current_index"`
	Total            int64            `json:"total"`
	PositionSeconds  float64          `json:"position_seconds"`
	Playing          bool             `json:"playing"`
	RepeatMode       string           `json:"repeat_mode"`
	Shuffled         bool             `json:"shuffled"`
	ActiveOutput     string           `json:"active_output"`
	Items            []playerItemJSON `json:"items"`
	WindowStartIndex int64            `json:"window_start_index"`
}

var playerShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the queue window around the current track",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var view playerViewJSON
		path := fmt.Sprintf("/api/me/queue?limit=%d", playerLimitFlag)
		if err := castAPI(cmd.Context(), http.MethodGet, path, nil, &view); err != nil {
			return err
		}
		if view.Total == 0 {
			fmt.Println("queue is empty")
			return nil
		}
		state := "paused"
		if view.Playing {
			state = "playing"
		}
		fmt.Printf("queue: %d tracks · %s at %.0fs · repeat %s · shuffle %v · output %q · v%d\n",
			view.Total, state, view.PositionSeconds, view.RepeatMode, view.Shuffled, view.ActiveOutput, view.Version)
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, " #\tITEM\tTITLE\tARTIST\tALBUM")
		for i, it := range view.Items {
			marker := " "
			if it.ItemID == view.CurrentItemID {
				marker = "▶"
			}
			_, _ = fmt.Fprintf(w, "%s%d\t%d\t%s\t%s\t%s\n",
				marker, view.WindowStartIndex+int64(i), it.ItemID, it.Title, it.ArtistName, it.AlbumTitle)
		}
		return w.Flush()
	},
}

var playerPlayCmd = &cobra.Command{
	Use:   "play [track-ids...]",
	Short: "Replace the queue from a source (--album/--artist/--genre or explicit track ids)",
	RunE: func(cmd *cobra.Command, args []string) error {
		source := map[string]any{}
		switch {
		case playerAlbumFlag > 0:
			source["kind"], source["id"] = "album", playerAlbumFlag
		case playerArtistFlag > 0:
			source["kind"], source["id"] = "artist", playerArtistFlag
		case playerGenreFlag != "":
			source["kind"], source["genre"] = "genre", playerGenreFlag
		case len(args) > 0:
			ids := make([]int64, 0, len(args))
			for _, a := range args {
				id, err := strconv.ParseInt(a, 10, 64)
				if err != nil {
					return fmt.Errorf("track id must be numeric, got %q", a)
				}
				ids = append(ids, id)
			}
			source["kind"], source["track_ids"] = "tracks", ids
		default:
			return fmt.Errorf("pick a source: --album, --artist, --genre, or track ids")
		}
		body := map[string]any{"source": source, "shuffle": playerShuffleFlag, "output": "cli"}
		var view playerViewJSON
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/me/queue", body, &view); err != nil {
			return err
		}
		fmt.Printf("queue replaced: %d tracks (shuffle %v)\n", view.Total, playerShuffleFlag)
		return nil
	},
}

var playerAddCmd = &cobra.Command{
	Use:   "add <track-ids...>",
	Short: "Append tracks to the queue",
	Args:  cobra.MinimumNArgs(1),
	RunE:  playerEnqueue("end"),
}

var playerNextCmd = &cobra.Command{
	Use:   "next <track-ids...>",
	Short: "Insert tracks right after the current one",
	Args:  cobra.MinimumNArgs(1),
	RunE:  playerEnqueue("next"),
}

func playerEnqueue(at string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ids := make([]int64, 0, len(args))
		for _, a := range args {
			id, err := strconv.ParseInt(a, 10, 64)
			if err != nil {
				return fmt.Errorf("track id must be numeric, got %q", a)
			}
			ids = append(ids, id)
		}
		var out struct {
			Added int64 `json:"added"`
		}
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/me/queue/items",
			map[string]any{"track_ids": ids, "at": at}, &out); err != nil {
			return err
		}
		fmt.Printf("added %d track(s)\n", out.Added)
		return nil
	}
}

var playerSkipCmd = &cobra.Command{
	Use:   "skip",
	Short: "Advance to the next track",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var view playerViewJSON
		if err := castAPI(cmd.Context(), http.MethodGet, "/api/me/queue?limit=1", nil, &view); err != nil {
			return err
		}
		if view.CurrentItemID == 0 {
			return fmt.Errorf("nothing playing")
		}
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/me/queue/advance",
			map[string]any{"from_item_id": view.CurrentItemID, "reason": "skip"}, &view); err != nil {
			return err
		}
		for _, it := range view.Items {
			if it.ItemID == view.CurrentItemID {
				fmt.Printf("now: %s — %s\n", it.ArtistName, it.Title)
				break
			}
		}
		return nil
	},
}

var playerShuffleCmd = &cobra.Command{
	Use:   "shuffle <on|off>",
	Short: "Toggle server-side shuffle of the upcoming tracks",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		on := args[0] == "on"
		if !on && args[0] != "off" {
			return fmt.Errorf("expected on or off, got %q", args[0])
		}
		return castAPI(cmd.Context(), http.MethodPost, "/api/me/queue/shuffle",
			map[string]any{"on": on}, &struct{}{})
	},
}

var playerRepeatCmd = &cobra.Command{
	Use:   "repeat <off|all|one>",
	Short: "Set the repeat mode",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return castAPI(cmd.Context(), http.MethodPost, "/api/me/queue/repeat",
			map[string]any{"mode": args[0]}, &struct{}{})
	},
}

var playerClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Empty the queue",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return castAPI(cmd.Context(), http.MethodDelete, "/api/me/queue", nil, &struct{}{})
	},
}

func init() {
	playerPlayCmd.Flags().BoolVar(&playerShuffleFlag, "shuffle", false, "Materialize in true random order")
	playerPlayCmd.Flags().Int64Var(&playerAlbumFlag, "album", 0, "Album id")
	playerPlayCmd.Flags().Int64Var(&playerArtistFlag, "artist", 0, "Artist id")
	playerPlayCmd.Flags().StringVar(&playerGenreFlag, "genre", "", "Genre name")
	playerShowCmd.Flags().IntVar(&playerLimitFlag, "limit", 30, "Window size")
	playerCmd.AddCommand(playerShowCmd, playerPlayCmd, playerAddCmd, playerNextCmd,
		playerSkipCmd, playerShuffleCmd, playerRepeatCmd, playerClearCmd)
	rootCmd.AddCommand(playerCmd)
}
