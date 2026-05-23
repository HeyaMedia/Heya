package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var mediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Browse and manage media items",
	Long:  "List, search, and manage matched media items.",
}

var mediaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List media items",
	RunE: func(cmd *cobra.Command, args []string) error {
		mediaType, _ := cmd.Flags().GetString("type")
		if mediaType == "" {
			return fmt.Errorf("--type is required (movie, tv, music, book)")
		}

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		q := sqlc.New(app.DBPool())
		items, err := q.ListMediaItemsByType(ctx, sqlc.ListMediaItemsByTypeParams{
			MediaType: sqlc.MediaType(mediaType),
			Limit:     100,
			Offset:    0,
		})
		if err != nil {
			return err
		}

		if ui.JSONMode {
			return ui.OutputJSON(items)
		}

		if len(items) == 0 {
			ui.Warn("No %s items found.", mediaType)
			return nil
		}

		t := ui.NewTable("ID", "TYPE", "TITLE", "YEAR")
		for _, item := range items {
			t.AddRow(
				strconv.FormatInt(item.ID, 10),
				ui.MediaBadge(string(item.MediaType)),
				item.Title,
				item.Year,
			)
		}
		fmt.Println(t.Render())
		return nil
	},
}

var mediaInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show media item details",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		q := sqlc.New(app.DBPool())
		item, err := q.GetMediaItemByID(ctx, id)
		if err != nil {
			return fmt.Errorf("media item %d not found", id)
		}

		if ui.JSONMode {
			result := map[string]any{"media_item": item}
			return ui.OutputJSON(result)
		}

		fmt.Println()
		fmt.Printf("  %s %s\n", ui.Bold(item.Title), ui.Dim("("+item.Year+")"))
		fmt.Printf("  %s\n", ui.MediaBadge(string(item.MediaType)))
		fmt.Println()

		if item.Description != "" {
			desc := item.Description
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
			fmt.Printf("  %s\n\n", ui.Dim(desc))
		}

		switch item.MediaType {
		case sqlc.MediaTypeMovie:
			renderMovieDetail(ctx, q, item)
		case sqlc.MediaTypeTv:
			renderTVDetail(ctx, q, item)
		case sqlc.MediaTypeMusic:
			renderMusicDetail(ctx, q, item)
		case sqlc.MediaTypeBook:
			renderBookDetail(ctx, q, item)
		}

		return nil
	},
}

func renderMovieDetail(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem) {
	movie, err := q.GetMovieByMediaItemID(ctx, item.ID)
	if err != nil {
		return
	}

	if movie.RuntimeMinutes > 0 {
		ui.Info("Runtime", fmt.Sprintf("%d min", movie.RuntimeMinutes))
	}
	if len(movie.Genres) > 0 {
		ui.Info("Genres", strings.Join(movie.Genres, ", "))
	}
	if movie.Tagline != "" {
		ui.Info("Tagline", movie.Tagline)
	}
	if movie.Rating.Valid {
		f, _ := movie.Rating.Float64Value()
		ui.Info("Rating", fmt.Sprintf("%.1f/10", f.Float64))
	}
	var extIDs map[string]string
	json.Unmarshal(item.ExternalIds, &extIDs)
	if extIDs["imdb"] != "" {
		ui.Info("IMDB", extIDs["imdb"])
	}
	if extIDs["tmdb"] != "" {
		ui.Info("TMDB", extIDs["tmdb"])
	}
	if movie.Budget > 0 {
		ui.Info("Budget", formatMoney(movie.Budget))
	}
	if movie.Revenue > 0 {
		ui.Info("Revenue", formatMoney(movie.Revenue))
	}

	cast, _ := q.ListMediaCastSlim(ctx, item.ID)
	if len(cast) > 0 {
		names := make([]string, 0, 5)
		for i, c := range cast {
			if i >= 5 {
				break
			}
			names = append(names, c.Name)
		}
		ui.Info("Cast", strings.Join(names, ", "))
	}

	crew, _ := q.ListMediaCrewSlim(ctx, item.ID)
	for _, c := range crew {
		if c.Job == "Director" {
			ui.Info("Director", c.Name)
			break
		}
	}
}

func renderTVDetail(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem) {
	series, err := q.GetTVSeriesByMediaItemID(ctx, item.ID)
	if err != nil {
		return
	}

	if series.Status != "" {
		ui.Info("Status", series.Status)
	}
	if len(series.Genres) > 0 {
		ui.Info("Genres", strings.Join(series.Genres, ", "))
	}
	networks, _ := q.ListNetworksForSeries(ctx, series.ID)
	if len(networks) > 0 {
		names := make([]string, len(networks))
		for i, n := range networks {
			names[i] = n.Name
		}
		ui.Info("Networks", strings.Join(names, ", "))
	}
	ui.Info("Seasons", strconv.Itoa(int(series.NumberOfSeasons)))
	ui.Info("Episodes", strconv.Itoa(int(series.NumberOfEpisodes)))
	if series.Rating.Valid {
		f, _ := series.Rating.Float64Value()
		ui.Info("Rating", fmt.Sprintf("%.1f/10", f.Float64))
	}

	seasons, _ := q.ListTVSeasonsBySeries(ctx, series.ID)
	if len(seasons) > 0 {
		fmt.Println()
		ui.Header("Seasons")
		for _, s := range seasons {
			eps, _ := q.ListTVEpisodesBySeason(ctx, s.ID)
			fmt.Printf("  Season %d: %s (%d episodes)\n",
				s.SeasonNumber, s.Title, len(eps))
		}
	}
}

func renderMusicDetail(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem) {
	artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
	if err != nil {
		return
	}

	ui.Info("Artist", artist.SortName)
	albums, _ := q.ListAlbumsByArtist(ctx, artist.ID)
	for _, album := range albums {
		fmt.Println()
		ui.Info("Album", fmt.Sprintf("%s (%s)", album.Title, album.Year))
		ui.Info("Type", album.AlbumType)
		if len(album.Genres) > 0 {
			ui.Info("Genres", strings.Join(album.Genres, ", "))
		}
		if album.Label != "" {
			ui.Info("Label", album.Label)
		}

		tracks, _ := q.ListTracksByAlbum(ctx, album.ID)
		if len(tracks) > 0 {
			fmt.Println()
			t := ui.NewTable("#", "TITLE", "DURATION")
			for _, tr := range tracks {
				dur := fmt.Sprintf("%d:%02d", tr.Duration/60, tr.Duration%60)
				t.AddRow(strconv.Itoa(int(tr.TrackNumber)), tr.Title, dur)
			}
			fmt.Println(t.Render())
		}
	}
}

func renderBookDetail(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem) {
	book, err := q.GetBookByMediaItemID(ctx, item.ID)
	if err != nil {
		return
	}

	if book.AuthorID.Valid {
		author, err := q.GetAuthorByID(ctx, book.AuthorID.Int64)
		if err == nil {
			ui.Info("Author", author.Name)
		}
	}
	if book.Isbn != "" {
		ui.Info("ISBN", book.Isbn)
	}
	if book.PageCount > 0 {
		ui.Info("Pages", strconv.Itoa(int(book.PageCount)))
	}
	if book.Publisher != "" {
		ui.Info("Publisher", book.Publisher)
	}
	if book.Language != "" {
		ui.Info("Language", book.Language)
	}
	if len(book.Subjects) > 0 {
		ui.Info("Subjects", strings.Join(book.Subjects, ", "))
	}
}

var mediaSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search media items",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		q := sqlc.New(app.DBPool())
		results, err := q.SearchAllMedia(ctx, sqlc.SearchAllMediaParams{
			Lower:  args[0],
			Limit:  50,
			Offset: 0,
		})
		if err != nil {
			return err
		}

		if ui.JSONMode {
			return ui.OutputJSON(results)
		}

		if len(results) == 0 {
			ui.Warn("No results for %q", args[0])
			return nil
		}

		t := ui.NewTable("ID", "TYPE", "TITLE", "YEAR")
		for _, item := range results {
			t.AddRow(
				strconv.FormatInt(item.ID, 10),
				ui.MediaBadge(string(item.MediaType)),
				item.Title,
				item.Year,
			)
		}
		fmt.Println(t.Render())
		return nil
	},
}

var mediaMatchCmd = &cobra.Command{
	Use:   "match",
	Short: "Trigger metadata matching for unmatched files",
	RunE: func(cmd *cobra.Command, args []string) error {
		libraryID, _ := cmd.Flags().GetInt64("library-id")
		if libraryID == 0 {
			return fmt.Errorf("--library-id is required")
		}

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		ui.Header("Matching library " + strconv.FormatInt(libraryID, 10))
		result, err := app.MatchLibrary(ctx, libraryID)
		if err != nil {
			return err
		}

		ui.Success("Match complete")
		ui.Info("Matched", strconv.Itoa(result.Matched))
		ui.Info("Unmatched", strconv.Itoa(result.Unmatched))
		ui.Info("Errors", strconv.Itoa(result.Errors))
		return nil
	},
}

var mediaRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Re-fetch metadata for a media item",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		if err := app.RefreshMediaItem(ctx, id); err != nil {
			return err
		}

		ui.Success("Metadata refreshed for media item %d", id)
		return nil
	},
}

func formatMoney(cents int64) string {
	if cents >= 1_000_000_000 {
		return fmt.Sprintf("$%.1fB", float64(cents)/1_000_000_000)
	}
	if cents >= 1_000_000 {
		return fmt.Sprintf("$%.1fM", float64(cents)/1_000_000)
	}
	return fmt.Sprintf("$%d", cents)
}

func init() {
	mediaListCmd.Flags().String("type", "", "Filter by type (movie, tv, music, book)")

	mediaInfoCmd.Flags().Int64("id", 0, "Media item ID")

	mediaMatchCmd.Flags().Int64("library-id", 0, "Library ID to match")

	mediaRefreshCmd.Flags().Int64("id", 0, "Media item ID to refresh")

	mediaCmd.AddCommand(mediaListCmd)
	mediaCmd.AddCommand(mediaInfoCmd)
	mediaCmd.AddCommand(mediaSearchCmd)
	mediaCmd.AddCommand(mediaMatchCmd)
	mediaCmd.AddCommand(mediaRefreshCmd)
}
