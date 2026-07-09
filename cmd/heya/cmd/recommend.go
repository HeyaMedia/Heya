package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var recommendCmd = &cobra.Command{
	Use:   "recommend",
	Short: "Personalized recommendations",
}

var recommendForYouCmd = &cobra.Command{
	Use:   "for-you <username>",
	Short: "Show personalized recommendations for a user",
	Long: "Rank unwatched library titles by the user's taste (hearts + watch history)\n" +
		"blended with the TMDB recommendation graph. Steer with --genre / --type / etc.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		typ, _ := cmd.Flags().GetString("type")
		genre, _ := cmd.Flags().GetString("genre")
		keyword, _ := cmd.Flags().GetString("keyword")
		minRating, _ := cmd.Flags().GetFloat64("min-rating")
		limit, _ := cmd.Flags().GetInt32("limit")

		return withApp(func(ctx context.Context, app *service.App) error {
			user, err := sqlc.New(app.DBPool()).GetUserByUsername(ctx, args[0])
			if err != nil {
				return fmt.Errorf("user not found: %s", args[0])
			}
			res, err := app.ForYou(ctx, user.ID, service.ForYouFacets{
				Type: typ, Genre: genre, Keyword: keyword, MinRating: minRating, Limit: limit,
			})
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(res)
			}

			if !res.HasSignal {
				ui.Warn("No taste signal yet (no hearts/watches) — showing a quality fallback.")
			}
			if len(res.Items) == 0 {
				ui.Warn("No recommendations matched the given filters.")
				return nil
			}
			t := ui.NewTable("#", "SCORE", "TYPE", "TITLE", "WHY")
			for i, it := range res.Items {
				t.AddRow(strconv.Itoa(i+1), fmt.Sprintf("%.3f", it.Score), it.MediaType, it.Title, it.Reason)
			}
			fmt.Println(t.Render())

			if len(res.Acquire) > 0 {
				ui.Println("")
				ui.Info("Acquire next", "highly recommended, not in your library")
				at := ui.NewTable("SCORE", "TYPE", "TITLE")
				for _, e := range res.Acquire {
					at.AddRow(fmt.Sprintf("%.2f", e.Score), e.MediaType, e.Title)
				}
				fmt.Println(at.Render())
			}
			return nil
		})
	},
}

func init() {
	recommendForYouCmd.Flags().String("type", "", "Restrict to movie | tv | anime")
	recommendForYouCmd.Flags().String("genre", "", "Only titles in this genre")
	recommendForYouCmd.Flags().String("keyword", "", "Only titles with this keyword/tag")
	recommendForYouCmd.Flags().Float64("min-rating", 0, "Minimum external rating (0-10)")
	recommendForYouCmd.Flags().Int32("limit", 20, "Number of results")
	recommendCmd.AddCommand(recommendForYouCmd)
}
