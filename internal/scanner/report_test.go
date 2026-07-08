package scanner

import (
	"bytes"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestWriteReportHighlightsReviewItems(t *testing.T) {
	result := Result{
		Movies: []MoviePlan{
			{Title: "Bad Grandpa", Year: "2013", Files: []string{"Bad.Grandpa.2013.mkv"}},
			{Title: "Kill Bill Vol 1", Year: "2003", Files: []string{"Kill.Bill.CD1.mkv"}},
			{Title: "Kill Bill Vol 1", Year: "2003", Files: []string{"Kill.Bill.CD2.mkv"}},
		},
		MovieMatches: []MovieMatch{
			{Key: "title_year:bad grandpa|2013", KeyType: "title_year", Title: "Bad Grandpa", Year: "2013", Files: []string{"Bad.Grandpa.2013.mkv"}, Plans: []MoviePlan{{}}},
			{Key: "title_year:kill bill vol 1|2003", KeyType: "title_year", Title: "Kill Bill Vol 1", Year: "2003", Files: []string{"Kill.Bill.CD1.mkv", "Kill.Bill.CD2.mkv"}, Plans: []MoviePlan{{}, {}}},
		},
		MovieSearch: []MovieSearchMatch{
			{
				Key:    "title_year:bad grandpa|2013",
				Query:  MovieSearchQuery{Title: "Dab GrandPaw", Year: "2013"},
				Reason: "ambiguous_or_low_confidence",
				Candidates: []MovieSearchCandidate{
					{Title: "Bad Grandpa", Year: "2013", Confidence: 0.74, ProviderID: "heya:movie:tmdb:208134"},
					{Title: "Grandpa", Year: "1990", Confidence: 0.51, ProviderID: "heya:movie:tmdb:1"},
				},
				Confidence: 0.74,
			},
		},
		MovieMetadata: []MovieFetchPreview{
			{Key: "tmdb:603", ProviderID: "heya:movie:tmdb:603", Title: "The Matrix", Year: "1999", WouldApply: []string{"external_ids", "title", "year"}, Cast: 3},
		},
	}
	events := []Event{
		{Event: "movie.file.unplanned", RelPath: "Documentaries/random.mkv", Reason: "no_movie_identity"},
	}

	var buf bytes.Buffer
	WriteReport(&buf, sqlc.Library{ID: 3, Name: "Movies", MediaType: sqlc.MediaTypeMovie}, result, events)
	report := buf.String()

	for _, want := range []string{
		"Movie scan report: Movies (id=3)",
		"Needs review: search rejected",
		"Dab GrandPaw (2013)",
		"Bad Grandpa (2013) score=0.74",
		"Grouped local identities",
		"Kill Bill Vol 1 (2003)",
		"Unplanned media",
		"Documentaries/random.mkv",
		"Metadata fetch preview",
		"The Matrix (1999) provider=heya:movie:tmdb:603 would_apply=external_ids,title,year",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestWriteReportMaterializationPreviewSuppressesFetchPreview(t *testing.T) {
	result := Result{
		MovieMetadata: []MovieFetchPreview{
			{Key: "tmdb:603", ProviderID: "heya:movie:tmdb:603", Title: "The Matrix", Year: "1999", WouldApply: []string{"external_ids", "title", "year"}},
		},
		MovieMaterialize: []MovieMaterializePreview{
			{
				Key:             "tmdb:603",
				Action:          "create",
				Title:           "The Matrix",
				Year:            "1999",
				ProviderID:      "heya:movie:tmdb:603",
				MediaItemAction: "create_media_item",
				MovieRowAction:  "create_movie_row",
				FileActions:     []MovieMaterializeFileAction{{RelPath: "The Matrix (1999)/The Matrix.mkv", Action: "create_library_file_and_attach"}},
				Collection:      "The Matrix Collection",
				RemoteArtwork:   12,
				Cast:            3,
			},
			{
				Key:    "tmdb:999001",
				Action: "blocked",
				Reason: "search_rejected",
				Title:  "Bad Metadata",
				Year:   "2021",
				Issues: []string{"no_candidates"},
			},
			{
				Key:             "tmdb:273641",
				Action:          "repair",
				Reason:          "stale_file_attachment",
				Title:           "Jackass Presents: Bad Grandpa .5",
				Year:            "2014",
				ProviderID:      "heya:movie:tmdb:273641",
				MediaItemAction: "create_media_item",
				MovieRowAction:  "create_movie_row",
				FileActions: []MovieMaterializeFileAction{{
					RelPath:             "Jackass/Bad Grandpa .5.mkv",
					Action:              "reassign_library_file",
					FileID:              153,
					ExistingMediaItemID: 121,
					ExistingItem:        &MovieMaterializeExistingItem{ID: 121, Title: "Jackass Presents: Bad Grandpa", Year: "2013", ExternalIDs: map[string]string{"tmdb": "208134"}},
				}},
			},
		},
	}

	var buf bytes.Buffer
	WriteReport(&buf, sqlc.Library{ID: 3, Name: "Movies", MediaType: sqlc.MediaTypeMovie}, result, nil)
	report := buf.String()

	for _, want := range []string{
		"Materialize:      1 create, 0 update, 1 repair, 1 blocked",
		"Materialization blocked",
		"blocked Bad Metadata (2021) reason=search_rejected",
		"Materialization repairs",
		"repair Jackass Presents: Bad Grandpa .5 (2014) provider=heya:movie:tmdb:273641 media=create_media_item movie=create_movie_row reason=stale_file_attachment files=reassign_library_file=1",
		"repair: file=153 reassign from media_item=121 Jackass Presents: Bad Grandpa (2013) ids=tmdb:208134",
		"Materialization preview",
		"create The Matrix (1999) provider=heya:movie:tmdb:603 media=create_media_item movie=create_movie_row collection=\"The Matrix Collection\" artwork=12 cast=3 files=create_library_file_and_attach=1",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "Metadata fetch preview") {
		t.Fatalf("materialization report should suppress raw metadata fetch preview:\n%s", report)
	}
}

func TestWriteReportApplyResults(t *testing.T) {
	result := Result{
		MovieApply: []MovieApplyResult{
			{
				Key:             "tmdb:603",
				Action:          "create",
				Title:           "The Matrix",
				Year:            "1999",
				ProviderID:      "heya:movie:tmdb:603",
				MediaItemID:     198,
				MediaItemAction: "create_media_item",
				MovieRowAction:  "create_movie_row",
				FilesCreated:    1,
				FilesAttached:   1,
				LocalAssets:     2,
				RemoteAssets:    3,
				RichMetadata:    true,
			},
			{
				Key:             "tmdb:273641",
				Action:          "repair",
				Reason:          "stale_file_attachment",
				Title:           "Jackass Presents: Bad Grandpa .5",
				Year:            "2014",
				ProviderID:      "heya:movie:tmdb:273641",
				MediaItemID:     190,
				MediaItemAction: "create_media_item",
				MovieRowAction:  "create_movie_row",
				FilesReassigned: 1,
				RichMetadata:    true,
			},
			{
				Key:                  "tmdb:120",
				Action:               "update",
				Title:                "The Lord of the Rings: The Fellowship of the Ring",
				Year:                 "2001",
				ProviderID:           "heya:movie:tmdb:120",
				MediaItemID:          125,
				MediaItemAction:      "update_media_item",
				MovieRowAction:       "update_movie_row",
				FilesAlreadyAttached: 1,
				RichMetadata:         true,
			},
			{
				Key:     "tmdb:999001",
				Action:  "skipped",
				Reason:  "search_rejected",
				Title:   "Actually Corrected By NFO",
				Year:    "2021",
				Skipped: true,
			},
		},
	}

	var buf bytes.Buffer
	WriteReport(&buf, sqlc.Library{ID: 3, Name: "Movies", MediaType: sqlc.MediaTypeMovie}, result, nil)
	report := buf.String()

	for _, want := range []string{
		"Applied:          1 create, 1 update, 1 repair, 1 skipped, 0 failed",
		"Apply skipped or failed",
		"skipped Actually Corrected By NFO (2021) reason=search_rejected",
		"Apply results",
		"repair Jackass Presents: Bad Grandpa .5 (2014) provider=heya:movie:tmdb:273641 media_item=190 media=create_media_item movie=create_movie_row reason=stale_file_attachment files=reassigned=1 rich=true",
		"create The Matrix (1999) provider=heya:movie:tmdb:603 media_item=198 media=create_media_item movie=create_movie_row files=attached=1, created=1 assets=local:2,remote:3 rich=true",
		"update The Lord of the Rings: The Fellowship of the Ring (2001) provider=heya:movie:tmdb:120 media_item=125 media=update_media_item movie=update_movie_row files=already_attached=1 rich=true",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
