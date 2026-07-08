//nolint:errcheck // CLI report output is best-effort; write failures are handled by the caller's writer.
package scanner

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

const reportCandidateLimit = 5

func WriteReport(w io.Writer, lib sqlc.Library, result Result, events []Event) {
	if w == nil {
		return
	}
	if lib.MediaType == sqlc.MediaTypeTv || lib.MediaType == sqlc.MediaTypeAnime {
		writeTVReport(w, lib, result, events)
		return
	}
	if lib.MediaType == sqlc.MediaTypeMusic {
		writeMusicReport(w, lib, result, events)
		return
	}
	if lib.MediaType == sqlc.MediaTypeBook {
		writeBookReport(w, lib, result, events)
		return
	}

	unplanned := eventsByName(events, "movie.file.unplanned")
	nfoFailures := eventsByName(events, "nfo.parse_failed")
	localExtras := inventoryFilesByClass(result.Inventory, ClassExtraMedia)
	grouped := groupedMovieMatches(result.MovieMatches)
	rejected, selected, suspicious := splitSearchResults(result.MovieSearch)
	fetched, fetchFailed := splitMetadataPreviews(result.MovieMetadata)
	materializeBlocked, materializeRepair, materializeCreate, materializeUpdate := splitMaterializePreviews(result.MovieMaterialize)
	applyFailed, applySkipped, applyRepair, applyCreate, applyUpdate := splitMovieApplyResults(result.MovieApply)

	fmt.Fprintf(w, "Movie scan report: %s (id=%d)\n", lib.Name, lib.ID)
	fmt.Fprintf(w, "Type: %s\n\n", lib.MediaType)

	fmt.Fprintln(w, "Summary")
	fmt.Fprintf(w, "  Classified files: %d\n", countInventoryFiles(result.Inventory))
	fmt.Fprintf(w, "  Movie plans:      %d\n", len(result.Movies))
	fmt.Fprintf(w, "  Local identities: %d\n", len(result.MovieMatches))
	if len(result.MovieSearch) > 0 {
		fmt.Fprintf(w, "  Search selected:  %d/%d\n", len(selected), len(result.MovieSearch))
		fmt.Fprintf(w, "  Search review:    %d rejected, %d suspicious selected\n", len(rejected), len(suspicious))
	} else {
		fmt.Fprintln(w, "  Search selected:  not run")
	}
	fmt.Fprintf(w, "  Grouped plans:    %d\n", len(grouped))
	fmt.Fprintf(w, "  Unplanned media:  %d\n", len(unplanned))
	fmt.Fprintf(w, "  Local extras:     %d\n", len(localExtras))
	fmt.Fprintf(w, "  NFO failures:     %d\n", len(nfoFailures))
	if len(result.MovieMetadata) > 0 {
		fmt.Fprintf(w, "  Metadata fetched: %d/%d\n", len(fetched), len(result.MovieMetadata))
	}
	if len(result.MovieMaterialize) > 0 {
		fmt.Fprintf(w, "  Materialize:      %d create, %d update, %d repair, %d blocked\n", len(materializeCreate), len(materializeUpdate), len(materializeRepair), len(materializeBlocked))
	}
	if len(result.MovieApply) > 0 {
		fmt.Fprintf(w, "  Applied:          %d create, %d update, %d repair, %d skipped, %d failed\n", len(applyCreate), len(applyUpdate), len(applyRepair), len(applySkipped), len(applyFailed))
	}

	if len(rejected) > 0 {
		fmt.Fprintln(w, "\nNeeds review: search rejected")
		for _, item := range rejected {
			writeSearchResult(w, item)
		}
	}

	if len(suspicious) > 0 {
		fmt.Fprintln(w, "\nNeeds review: selected but worth checking")
		for _, item := range suspicious {
			writeSearchResult(w, item)
		}
	}

	if len(grouped) > 0 {
		fmt.Fprintln(w, "\nGrouped local identities")
		for _, match := range grouped {
			fmt.Fprintf(w, "  - %s%s [%s] plans=%d files=%d\n", match.Title, reportYear(match.Year), match.Key, len(match.Plans), len(match.Files))
			for _, file := range limitStrings(match.Files, 6) {
				fmt.Fprintf(w, "      file: %s\n", file)
			}
		}
	}

	if len(result.MovieMetadata) > 0 && len(result.MovieMaterialize) == 0 {
		fmt.Fprintln(w, "\nMetadata fetch preview")
		for _, item := range fetchFailed {
			fmt.Fprintf(w, "  - %s failed provider=%s error=%s\n", item.Key, item.ProviderID, item.Error)
		}
		for _, item := range fetched {
			fmt.Fprintf(w, "  - %s%s provider=%s would_apply=%s", item.Title, reportYear(item.Year), item.ProviderID, strings.Join(item.WouldApply, ","))
			if item.Collection != "" {
				fmt.Fprintf(w, " collection=%q", item.Collection)
			}
			if item.Artwork > 0 {
				fmt.Fprintf(w, " artwork=%d", item.Artwork)
			}
			if item.Cast > 0 {
				fmt.Fprintf(w, " cast=%d", item.Cast)
			}
			fmt.Fprintln(w)
		}
	}

	if len(result.MovieMaterialize) > 0 {
		if len(materializeBlocked) > 0 {
			fmt.Fprintln(w, "\nMaterialization blocked")
			for _, item := range materializeBlocked {
				writeMaterializeResult(w, item)
			}
		}
		if len(materializeRepair) > 0 {
			fmt.Fprintln(w, "\nMaterialization repairs")
			for _, item := range materializeRepair {
				writeMaterializeResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nMaterialization preview")
		for _, item := range append(materializeCreate, materializeUpdate...) {
			writeMaterializeResult(w, item)
		}
	}

	if len(result.MovieApply) > 0 {
		if len(applyFailed) > 0 || len(applySkipped) > 0 {
			fmt.Fprintln(w, "\nApply skipped or failed")
			for _, item := range append(applyFailed, applySkipped...) {
				writeApplyResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nApply results")
		applyVisible := append(append(applyRepair, applyCreate...), applyUpdate...)
		for _, item := range applyVisible {
			writeApplyResult(w, item)
		}
	}

	if len(unplanned) > 0 {
		fmt.Fprintln(w, "\nUnplanned media")
		for _, ev := range limitEvents(unplanned, 20) {
			fmt.Fprintf(w, "  - %s", ev.RelPath)
			if ev.Reason != "" {
				fmt.Fprintf(w, " (%s)", ev.Reason)
			}
			if ev.Message != "" {
				fmt.Fprintf(w, ": %s", ev.Message)
			}
			fmt.Fprintln(w)
		}
		if len(unplanned) > 20 {
			fmt.Fprintf(w, "  ... %d more\n", len(unplanned)-20)
		}
	}

	if len(localExtras) > 0 {
		fmt.Fprintln(w, "\nLocal extras")
		writeLocalExtras(w, localExtras, 30)
	}

	if len(nfoFailures) > 0 {
		fmt.Fprintln(w, "\nNFO parse failures")
		for _, ev := range limitEvents(nfoFailures, 20) {
			fmt.Fprintf(w, "  - %s\n", ev.RelPath)
		}
		if len(nfoFailures) > 20 {
			fmt.Fprintf(w, "  ... %d more\n", len(nfoFailures)-20)
		}
	}

	if len(result.MovieSearch) == 0 {
		fmt.Fprintln(w, "\nSearch was not run. Add --search to include heya.media candidate review.")
	}
}

func writeBookReport(w io.Writer, lib sqlc.Library, result Result, events []Event) {
	unplanned := eventsByName(events, "book.file.unplanned")
	localArtwork := inventoryFilesByClass(result.Inventory, ClassArtwork)
	searchRejected, searchSelected, searchSuspicious := splitBookSearchResults(result.BookSearch)
	bookFetched, bookFetchFailed := splitBookMetadataPreviews(result.BookMetadata)
	materializeBlocked, materializeRepair, materializeCreate, materializeUpdate := splitBookMaterializePreviews(result.BookMaterialize)
	applyFailed, applySkipped, applyRepair, applyCreate, applyUpdate := splitBookApplyResults(result.BookApply)

	ebooks := 0
	audiobooks := 0
	issuePlans := 0
	for _, plan := range result.BookPlans {
		if plan.Format == "audiobook" {
			audiobooks++
		} else {
			ebooks++
		}
		if len(plan.Issues) > 0 {
			issuePlans++
		}
	}

	fmt.Fprintf(w, "Book scan report: %s (id=%d)\n", lib.Name, lib.ID)
	fmt.Fprintf(w, "Type: %s\n\n", lib.MediaType)

	fmt.Fprintln(w, "Summary")
	fmt.Fprintf(w, "  Classified files: %d\n", countInventoryFiles(result.Inventory))
	fmt.Fprintf(w, "  Book plans:       %d\n", len(result.BookPlans))
	fmt.Fprintf(w, "  Ebooks:           %d\n", ebooks)
	fmt.Fprintf(w, "  Audiobooks:       %d\n", audiobooks)
	fmt.Fprintf(w, "  Plan issues:      %d\n", issuePlans)
	fmt.Fprintf(w, "  Unplanned media:  %d\n", len(unplanned))
	fmt.Fprintf(w, "  Local artwork:    %d\n", len(localArtwork))
	if len(result.BookSearch) > 0 {
		fmt.Fprintf(w, "  Search selected:  %d/%d\n", len(searchSelected), len(result.BookSearch))
		fmt.Fprintf(w, "  Search review:    %d rejected, %d suspicious selected\n", len(searchRejected), len(searchSuspicious))
	} else {
		fmt.Fprintln(w, "  Search selected:  not run")
	}
	if len(result.BookMetadata) > 0 {
		fmt.Fprintf(w, "  Metadata fetched: %d/%d\n", len(bookFetched), len(result.BookMetadata))
	}
	if len(result.BookMaterialize) > 0 {
		fmt.Fprintf(w, "  Materialize:      %d create, %d update, %d repair, %d blocked\n", len(materializeCreate), len(materializeUpdate), len(materializeRepair), len(materializeBlocked))
	}
	if len(result.BookApply) > 0 {
		fmt.Fprintf(w, "  Applied:          %d create, %d update, %d repair, %d skipped, %d failed\n", len(applyCreate), len(applyUpdate), len(applyRepair), len(applySkipped), len(applyFailed))
	}

	if len(searchRejected) > 0 {
		fmt.Fprintln(w, "\nNeeds review: search rejected")
		for _, item := range searchRejected {
			writeBookSearchResult(w, item)
		}
	}
	if len(searchSuspicious) > 0 {
		fmt.Fprintln(w, "\nNeeds review: selected but worth checking")
		for _, item := range searchSuspicious {
			writeBookSearchResult(w, item)
		}
	}
	if issuePlans > 0 {
		fmt.Fprintln(w, "\nNeeds review: incomplete book identities")
		for _, plan := range result.BookPlans {
			if len(plan.Issues) == 0 {
				continue
			}
			fmt.Fprintf(w, "  - %s%s", plan.Title, reportYear(plan.Year))
			if plan.Author != "" {
				fmt.Fprintf(w, " by %s", plan.Author)
			}
			fmt.Fprintf(w, " [%s] format=%s issues=%s confidence=%.2f\n", plan.Key, plan.Format, strings.Join(plan.Issues, ","), plan.Confidence)
			for _, file := range limitStrings(plan.Files, 4) {
				fmt.Fprintf(w, "      file: %s\n", file)
			}
		}
	}

	if len(result.BookMetadata) > 0 && len(result.BookMaterialize) == 0 {
		fmt.Fprintln(w, "\nMetadata fetch preview")
		for _, item := range bookFetchFailed {
			fmt.Fprintf(w, "  - %s failed provider=%s error=%s\n", item.Key, item.ProviderID, item.Error)
		}
		for _, item := range bookFetched {
			fmt.Fprintf(w, "  - %s%s", item.Title, reportYear(item.Year))
			if item.Author != "" {
				fmt.Fprintf(w, " by %s", item.Author)
			}
			fmt.Fprintf(w, " provider=%s format=%s would_apply=%s", item.ProviderID, item.Format, strings.Join(item.WouldApply, ","))
			if item.PageCount > 0 {
				fmt.Fprintf(w, " pages=%d", item.PageCount)
			}
			if item.Publisher != "" {
				fmt.Fprintf(w, " publisher=%q", item.Publisher)
			}
			fmt.Fprintln(w)
			for _, issue := range limitStrings(item.Issues, 4) {
				fmt.Fprintf(w, "      issue: %s\n", issue)
			}
		}
	}

	if len(result.BookMaterialize) > 0 {
		if len(materializeBlocked) > 0 {
			fmt.Fprintln(w, "\nMaterialization blocked")
			for _, item := range materializeBlocked {
				writeBookMaterializeResult(w, item)
			}
		}
		if len(materializeRepair) > 0 {
			fmt.Fprintln(w, "\nMaterialization repairs")
			for _, item := range materializeRepair {
				writeBookMaterializeResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nMaterialization preview")
		for _, item := range append(materializeCreate, materializeUpdate...) {
			writeBookMaterializeResult(w, item)
		}
	}

	if len(result.BookApply) > 0 {
		if len(applyFailed) > 0 || len(applySkipped) > 0 {
			fmt.Fprintln(w, "\nApply skipped or failed")
			for _, item := range append(applyFailed, applySkipped...) {
				writeBookApplyResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nApply results")
		for _, item := range append(append(applyRepair, applyCreate...), applyUpdate...) {
			writeBookApplyResult(w, item)
		}
	}

	fmt.Fprintln(w, "\nBook plan overview")
	for _, plan := range result.BookPlans {
		fmt.Fprintf(w, "  - %s%s", plan.Title, reportYear(plan.Year))
		if plan.Author != "" {
			fmt.Fprintf(w, " by %s", plan.Author)
		}
		fmt.Fprintf(w, " [%s] format=%s files=%d confidence=%.2f", plan.Key, plan.Format, len(plan.Files), plan.Confidence)
		if len(plan.FileFormats) > 0 {
			fmt.Fprintf(w, " file_formats=%s", strings.Join(plan.FileFormats, ","))
		}
		if len(plan.Assets) > 0 {
			fmt.Fprintf(w, " assets=%d", len(plan.Assets))
		}
		fmt.Fprintln(w)
	}

	if len(unplanned) > 0 {
		fmt.Fprintln(w, "\nUnplanned media")
		for _, ev := range limitEvents(unplanned, 20) {
			fmt.Fprintf(w, "  - %s", ev.RelPath)
			if ev.Reason != "" {
				fmt.Fprintf(w, " (%s)", ev.Reason)
			}
			if ev.Message != "" {
				fmt.Fprintf(w, ": %s", ev.Message)
			}
			fmt.Fprintln(w)
		}
	}
	if len(result.BookSearch) == 0 {
		fmt.Fprintln(w, "\nSearch was not run. Add --search to include heya.media candidate review.")
	}
}

func writeMusicReport(w io.Writer, lib sqlc.Library, result Result, events []Event) {
	unplanned := eventsByName(events, "music.file.unplanned")
	nfoFailures := eventsByName(events, "nfo.parse_failed")
	artwork := inventoryFilesByClass(result.Inventory, ClassArtwork)
	lyrics := inventoryFilesByClass(result.Inventory, ClassLyrics)
	trackIssues := musicTracksWithIssues(result.MusicTracks)
	albumIssues := musicAlbumsWithIssues(result.MusicAlbums)
	searchDeferred := eventsByName(events, "music.search.deferred")
	fetchDeferred := eventsByName(events, "music.fetch.deferred")
	materializeDeferred := eventsByName(events, "music.materialize.deferred")
	applyDeferred := eventsByName(events, "music.apply.deferred")
	searchRejected, searchErrors, searchSelected, searchSuspicious := splitMusicSearchResults(result.MusicSearch)
	musicFetched, musicFetchFailed := splitMusicMetadataPreviews(result.MusicMetadata)
	musicMappingIssues := musicMetadataWithIssues(musicFetched)
	materializeBlocked, materializeRepair, materializeCreate, materializeUpdate := splitMusicMaterializePreviews(result.MusicMaterialize)
	applyFailed, applySkipped, applyRepair, applyCreate, applyUpdate := splitMusicApplyResults(result.MusicApply)

	fmt.Fprintf(w, "Music scan report: %s (id=%d)\n", lib.Name, lib.ID)
	fmt.Fprintf(w, "Type: %s\n\n", lib.MediaType)

	fmt.Fprintln(w, "Summary")
	fmt.Fprintf(w, "  Classified files:       %d\n", countInventoryFiles(result.Inventory))
	fmt.Fprintf(w, "  Audio track plans:      %d\n", len(result.MusicTracks))
	fmt.Fprintf(w, "  Local album identities: %d\n", len(result.MusicAlbums))
	fmt.Fprintf(w, "  Local artist identities: %d\n", len(result.MusicArtists))
	fmt.Fprintf(w, "  Track issues:           %d\n", len(trackIssues))
	fmt.Fprintf(w, "  Album issues:           %d\n", len(albumIssues))
	fmt.Fprintf(w, "  Unplanned audio:        %d\n", len(unplanned))
	fmt.Fprintf(w, "  Local artwork:          %d\n", len(artwork))
	fmt.Fprintf(w, "  Local lyrics:           %d\n", len(lyrics))
	fmt.Fprintf(w, "  NFO failures:           %d\n", len(nfoFailures))
	if len(result.MusicSearch) > 0 {
		fmt.Fprintf(w, "  Search selected:        %d/%d\n", len(searchSelected), len(result.MusicSearch))
		if len(searchErrors) > 0 {
			fmt.Fprintf(w, "  Search review:          %d rejected, %d errors, %d suspicious selected\n", len(searchRejected), len(searchErrors), len(searchSuspicious))
		} else {
			fmt.Fprintf(w, "  Search review:          %d rejected, %d suspicious selected\n", len(searchRejected), len(searchSuspicious))
		}
	} else if len(searchDeferred) > 0 {
		fmt.Fprintln(w, "  Search selected:        deferred")
	} else {
		fmt.Fprintln(w, "  Search selected:        not run")
	}
	if len(fetchDeferred) > 0 {
		fmt.Fprintln(w, "  Metadata fetched:       deferred")
	} else if len(result.MusicMetadata) > 0 {
		fmt.Fprintf(w, "  Metadata fetched:       %d/%d artists\n", len(musicFetched), len(result.MusicMetadata))
		fmt.Fprintf(w, "  Discography mapped:     %d/%d albums, %d/%d tracks\n", countMappedMusicAlbums(musicFetched), countLocalMusicAlbums(musicFetched), countMappedMusicTracks(musicFetched), countLocalMusicTracks(musicFetched))
	}
	if len(materializeDeferred) > 0 {
		fmt.Fprintln(w, "  Materialize:            deferred")
	} else if len(result.MusicMaterialize) > 0 {
		fmt.Fprintf(w, "  Materialize:            %d create, %d update, %d repair, %d blocked\n", len(materializeCreate), len(materializeUpdate), len(materializeRepair), len(materializeBlocked))
	}
	if len(applyDeferred) > 0 {
		fmt.Fprintln(w, "  Applied:                deferred")
	} else if len(result.MusicApply) > 0 {
		fmt.Fprintf(w, "  Applied:                %d create, %d update, %d repair, %d skipped, %d failed\n", len(applyCreate), len(applyUpdate), len(applyRepair), len(applySkipped), len(applyFailed))
	}

	if len(trackIssues) > 0 {
		fmt.Fprintln(w, "\nNeeds review: incomplete music tracks")
		for _, track := range limitMusicTracks(trackIssues, 30) {
			fmt.Fprintf(w, "  - %s artist=%q album=%q", track.RelPath, track.Artist, track.Album)
			if track.DiscNumber > 0 || track.TrackNumber > 0 {
				fmt.Fprintf(w, " track=%d/%d", track.DiscNumber, track.TrackNumber)
			}
			if len(track.Issues) > 0 {
				fmt.Fprintf(w, " issues=%s", strings.Join(track.Issues, ","))
			}
			fmt.Fprintf(w, " confidence=%.2f\n", track.Confidence)
		}
		if len(trackIssues) > 30 {
			fmt.Fprintf(w, "  ... %d more tracks\n", len(trackIssues)-30)
		}
	}

	if len(unplanned) > 0 {
		fmt.Fprintln(w, "\nUnplanned audio")
		for _, ev := range limitEvents(unplanned, 30) {
			fmt.Fprintf(w, "  - %s", ev.RelPath)
			if ev.Reason != "" {
				fmt.Fprintf(w, " (%s)", ev.Reason)
			}
			if ev.Message != "" {
				fmt.Fprintf(w, ": %s", ev.Message)
			}
			fmt.Fprintln(w)
		}
		if len(unplanned) > 30 {
			fmt.Fprintf(w, "  ... %d more\n", len(unplanned)-30)
		}
	}

	if len(searchRejected) > 0 {
		fmt.Fprintln(w, "\nNeeds review: search rejected")
		for _, item := range searchRejected {
			writeMusicSearchResult(w, item)
		}
	}
	if len(searchErrors) > 0 {
		fmt.Fprintln(w, "\nSearch errors")
		for _, item := range searchErrors {
			writeMusicSearchResult(w, item)
		}
	}
	if len(searchSuspicious) > 0 {
		fmt.Fprintln(w, "\nNeeds review: selected but worth checking")
		for _, item := range searchSuspicious {
			writeMusicSearchResult(w, item)
		}
	}

	if len(musicMappingIssues) > 0 {
		fmt.Fprintln(w, "\nNeeds review: metadata mapping")
		for _, item := range musicMappingIssues {
			fmt.Fprintf(w, "  - %s [%s] provider=%s mapped_albums=%d/%d mapped_tracks=%d/%d\n", item.Artist, item.Key, item.ProviderID, item.MappedAlbums, item.LocalAlbums, item.MappedTracks, item.LocalTracks)
			if item.SearchProviderID != "" {
				fmt.Fprintf(w, "      selected_after_fetch: previous=%s reason=%s\n", item.SearchProviderID, item.SelectionReason)
			}
			for _, issue := range limitStrings(item.Issues, 6) {
				fmt.Fprintf(w, "      issue: %s\n", issue)
			}
			if len(item.Issues) > 6 {
				fmt.Fprintf(w, "      ... %d more issues\n", len(item.Issues)-6)
			}
		}
	}

	if len(result.MusicMetadata) > 0 && len(result.MusicMaterialize) == 0 {
		fmt.Fprintln(w, "\nMetadata fetch preview")
		for _, item := range musicFetchFailed {
			fmt.Fprintf(w, "  - %s failed provider=%s error=%s\n", item.Key, item.ProviderID, item.Error)
		}
		for _, item := range musicFetched {
			fmt.Fprintf(w, "  - %s provider=%s would_apply=%s", item.Artist, item.ProviderID, strings.Join(item.WouldApply, ","))
			if item.SearchProviderID != "" {
				fmt.Fprintf(w, " selected_after_fetch=%s previous=%s", item.SelectionReason, item.SearchProviderID)
			}
			if item.RemoteAlbums > 0 {
				fmt.Fprintf(w, " remote_albums=%d", item.RemoteAlbums)
			}
			if item.RemoteTracks > 0 {
				fmt.Fprintf(w, " remote_tracks=%d", item.RemoteTracks)
			}
			fmt.Fprintf(w, " mapped_albums=%d/%d mapped_tracks=%d/%d", item.MappedAlbums, item.LocalAlbums, item.MappedTracks, item.LocalTracks)
			if item.Artwork > 0 {
				fmt.Fprintf(w, " artwork=%d", item.Artwork)
			}
			if item.Tags > 0 {
				fmt.Fprintf(w, " tags=%d", item.Tags)
			}
			fmt.Fprintln(w)
			for _, album := range limitMusicAlbumFetchMatches(item.AlbumMappings, 8) {
				fmt.Fprintf(w, "      album: %s%s", album.LocalAlbum, reportYear(album.LocalYear))
				if album.RemoteAlbum != "" {
					fmt.Fprintf(w, " -> %s", album.RemoteAlbum)
					if album.RemoteYear > 0 {
						fmt.Fprintf(w, " (%d)", album.RemoteYear)
					}
					if album.Reason != "" {
						fmt.Fprintf(w, " reason=%s", album.Reason)
					}
					if album.Confidence > 0 {
						fmt.Fprintf(w, " score=%.2f", album.Confidence)
					}
					fmt.Fprintf(w, " tracks=%d/%d", album.MappedTracks, album.LocalTracks)
					if album.RemoteKind != "" {
						fmt.Fprintf(w, " kind=%s", album.RemoteKind)
					}
				} else {
					fmt.Fprintf(w, " -> unmatched tracks=0/%d", album.LocalTracks)
				}
				if len(album.Issues) > 0 {
					fmt.Fprintf(w, " issues=%d", len(album.Issues))
				}
				fmt.Fprintln(w)
			}
			if len(item.AlbumMappings) > 8 {
				fmt.Fprintf(w, "      ... %d more local albums\n", len(item.AlbumMappings)-8)
			}
			if item.SelectionReason == "discography_reranked" && len(item.CandidateEvaluations) > 0 {
				fmt.Fprintln(w, "      candidates:")
				for _, candidate := range limitMusicCandidateFetchEvaluations(item.CandidateEvaluations, 5) {
					fmt.Fprintf(w, "        - %s provider=%s mapped_albums=%d/%d mapped_tracks=%d/%d", candidate.Artist, candidate.ProviderID, candidate.MappedAlbums, candidate.LocalAlbums, candidate.MappedTracks, candidate.LocalTracks)
					if candidate.Selected {
						fmt.Fprintf(w, " selected")
					}
					if candidate.Confidence > 0 {
						fmt.Fprintf(w, " score=%.2f", candidate.Confidence)
					}
					if candidate.Error != "" {
						fmt.Fprintf(w, " error=%q", candidate.Error)
					}
					fmt.Fprintln(w)
				}
				if len(item.CandidateEvaluations) > 5 {
					fmt.Fprintf(w, "        ... %d more candidates\n", len(item.CandidateEvaluations)-5)
				}
			}
		}
	}

	if len(result.MusicMaterialize) > 0 {
		if len(materializeBlocked) > 0 {
			fmt.Fprintln(w, "\nMaterialization blocked")
			for _, item := range materializeBlocked {
				writeMusicMaterializeResult(w, item)
			}
		}
		if len(materializeRepair) > 0 {
			fmt.Fprintln(w, "\nMaterialization repairs")
			for _, item := range materializeRepair {
				writeMusicMaterializeResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nMaterialization preview")
		for _, item := range append(materializeCreate, materializeUpdate...) {
			writeMusicMaterializeResult(w, item)
		}
	}

	if len(result.MusicApply) > 0 {
		if len(applyFailed) > 0 || len(applySkipped) > 0 {
			fmt.Fprintln(w, "\nApply skipped or failed")
			for _, item := range append(applyFailed, applySkipped...) {
				writeMusicApplyResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nApply results")
		for _, item := range append(append(applyRepair, applyCreate...), applyUpdate...) {
			writeMusicApplyResult(w, item)
		}
	}

	if len(result.MusicArtists) > 0 {
		fmt.Fprintln(w, "\nArtist plan overview")
		for _, artist := range limitMusicArtists(result.MusicArtists, 40) {
			fmt.Fprintf(w, "  - %s [%s] albums=%d tracks=%d files=%d confidence=%.2f", artist.Artist, artist.Key, len(artist.Albums), countMusicArtistTracks(artist), len(artist.Files), artist.Confidence)
			if artist.ArtistDisambiguation != "" {
				fmt.Fprintf(w, " disambiguation=%q", artist.ArtistDisambiguation)
			}
			if len(artist.Issues) > 0 {
				fmt.Fprintf(w, " issues=%s", strings.Join(artist.Issues, ","))
			}
			fmt.Fprintln(w)
			for _, album := range limitMusicAlbums(artist.Albums, 6) {
				fmt.Fprintf(w, "      album: %s%s tracks=%d", album.Album, reportYear(album.Year), len(album.Tracks))
				if album.ReleaseKind != "" {
					fmt.Fprintf(w, " kind=%s", album.ReleaseKind)
				}
				if len(album.Aliases) > 0 {
					fmt.Fprintf(w, " aliases=%q", strings.Join(album.Aliases, ", "))
				}
				if len(album.Issues) > 0 {
					fmt.Fprintf(w, " issues=%s", strings.Join(album.Issues, ","))
				}
				fmt.Fprintln(w)
			}
			if len(artist.Albums) > 6 {
				fmt.Fprintf(w, "      ... %d more albums\n", len(artist.Albums)-6)
			}
		}
		if len(result.MusicArtists) > 40 {
			fmt.Fprintf(w, "  ... %d more artists\n", len(result.MusicArtists)-40)
		}
	}

	if len(nfoFailures) > 0 {
		fmt.Fprintln(w, "\nNFO parse failures")
		for _, ev := range limitEvents(nfoFailures, 20) {
			fmt.Fprintf(w, "  - %s\n", ev.RelPath)
		}
		if len(nfoFailures) > 20 {
			fmt.Fprintf(w, "  ... %d more\n", len(nfoFailures)-20)
		}
	}

	if len(searchDeferred) > 0 {
		fmt.Fprintf(w, "\n%s\n", searchDeferred[0].Message)
	} else if len(result.MusicApply) > 0 {
		fmt.Fprintln(w, "\nMusic apply completed for non-blocked materialization previews.")
	} else if len(result.MusicMaterialize) > 0 {
		fmt.Fprintln(w, "\nMusic materialization preview completed. Apply will write the non-blocked artist, album, track, and file plans.")
	} else if len(result.MusicMetadata) > 0 {
		fmt.Fprintln(w, "\nMusic metadata fetch completed. Materialization will turn these artist, album, and track mappings into database writes.")
	} else if len(result.MusicSearch) > 0 {
		fmt.Fprintln(w, "\nMusic search completed. Fetch will map local albums and tracks into each selected artist discography.")
	} else {
		fmt.Fprintln(w, "\nSearch was not run. Music matching will be artist-first because heya.media returns an artist discography blob.")
	}
}

func writeTVReport(w io.Writer, lib sqlc.Library, result Result, events []Event) {
	domain := "tv"
	header := "TV"
	episodePlanLabel := "TV episode plans"
	localIdentityLabel := "Local show identities"
	groupedPlanLabel := "Grouped show plans"
	overviewTitle := "Show plan overview"
	titleOnlyHeading := "Needs review: title-only show identities"
	issuesHeading := "Needs review: local identity issues"
	if lib.MediaType == sqlc.MediaTypeAnime {
		domain = "anime"
		header = "Anime"
		episodePlanLabel = "Anime episode plans"
		localIdentityLabel = "Local anime identities"
		groupedPlanLabel = "Grouped anime plans"
		overviewTitle = "Anime plan overview"
		titleOnlyHeading = "Needs review: title-only anime identities"
		issuesHeading = "Needs review: local anime identity issues"
	}
	unplanned := eventsByName(events, domain+".file.unplanned")
	nfoFailures := eventsByName(events, "nfo.parse_failed")
	localExtras := inventoryFilesByClass(result.Inventory, ClassExtraMedia)
	plexmatches := eventsByName(events, "plexmatch.parsed")
	manualDecisionByKey := scanManualDecisions(result)
	titleOnly := titleOnlyTVMatches(result.TVMatches, manualDecisionByKey)
	multiEpisode := multiEpisodeTVPlans(result.TVPlans)
	issues := tvMatchesWithIssues(result.TVMatches)
	grouped := groupedTVMatches(result.TVMatches)
	rejected, selected, suspicious := splitTVSearchResults(result.TVSearch)
	fetched, fetchFailed := splitTVMetadataPreviews(result.TVMetadata)
	materializeBlocked, materializeRepair, materializeCreate, materializeUpdate := splitTVMaterializePreviews(result.TVMaterialize)
	applyFailed, applySkipped, applyRepair, applyCreate, applyUpdate := splitTVApplyResults(result.TVApply)

	fmt.Fprintf(w, "%s scan report: %s (id=%d)\n", header, lib.Name, lib.ID)
	fmt.Fprintf(w, "Type: %s\n\n", lib.MediaType)

	fmt.Fprintln(w, "Summary")
	fmt.Fprintf(w, "  Classified files:      %d\n", countInventoryFiles(result.Inventory))
	if lib.MediaType == sqlc.MediaTypeAnime {
		fmt.Fprintf(w, "  %-22s %d\n", episodePlanLabel+":", len(result.TVPlans))
		fmt.Fprintf(w, "  %-22s %d\n", localIdentityLabel+":", len(result.TVMatches))
	} else {
		fmt.Fprintf(w, "  TV episode plans:      %d\n", len(result.TVPlans))
		fmt.Fprintf(w, "  Local show identities: %d\n", len(result.TVMatches))
	}
	fmt.Fprintf(w, "  Planned episodes:      %d\n", countTVPlannedEpisodes(result.TVPlans))
	fmt.Fprintf(w, "  Multi-episode files:   %d\n", len(multiEpisode))
	fmt.Fprintf(w, "  Title-only identities: %d\n", len(titleOnly))
	if lib.MediaType == sqlc.MediaTypeAnime {
		fmt.Fprintf(w, "  %-22s %d\n", groupedPlanLabel+":", len(grouped))
	} else {
		fmt.Fprintf(w, "  Grouped show plans:    %d\n", len(grouped))
	}
	fmt.Fprintf(w, "  Unplanned media:       %d\n", len(unplanned))
	fmt.Fprintf(w, "  Local extras:          %d\n", len(localExtras))
	fmt.Fprintf(w, "  NFO failures:          %d\n", len(nfoFailures))
	fmt.Fprintf(w, "  Plexmatch files:       %d\n", len(plexmatches))
	if len(result.TVSearch) > 0 {
		fmt.Fprintf(w, "  Search selected:       %d/%d\n", len(selected), len(result.TVSearch))
		fmt.Fprintf(w, "  Search review:         %d rejected, %d suspicious selected\n", len(rejected), len(suspicious))
	} else {
		fmt.Fprintln(w, "  Search selected:       not run")
	}
	if len(result.TVMetadata) > 0 {
		fmt.Fprintf(w, "  Metadata fetched:      %d/%d unique targets\n", len(fetched), len(result.TVMetadata))
	}
	if len(result.TVMaterialize) > 0 {
		fmt.Fprintf(w, "  Materialize:           %d create, %d update, %d repair, %d blocked\n", len(materializeCreate), len(materializeUpdate), len(materializeRepair), len(materializeBlocked))
	}
	if len(result.TVApply) > 0 {
		fmt.Fprintf(w, "  Applied:               %d create, %d update, %d repair, %d skipped, %d failed\n", len(applyCreate), len(applyUpdate), len(applyRepair), len(applySkipped), len(applyFailed))
	}

	if len(rejected) > 0 {
		fmt.Fprintln(w, "\nNeeds review: search rejected")
		for _, item := range rejected {
			writeTVSearchResult(w, item)
		}
	}

	if len(suspicious) > 0 {
		fmt.Fprintln(w, "\nNeeds review: selected but worth checking")
		for _, item := range suspicious {
			writeTVSearchResult(w, item)
		}
	}

	if len(titleOnly) > 0 {
		fmt.Fprintf(w, "\n%s\n", titleOnlyHeading)
		for _, match := range titleOnly {
			fmt.Fprintf(w, "  - %s [%s] plans=%d files=%d episodes=%s confidence=%.2f\n", match.Title, match.Key, len(match.Plans), len(match.Files), formatTVMatchEpisodes(match), match.Confidence)
			for _, file := range limitStrings(match.Files, 4) {
				fmt.Fprintf(w, "      file: %s\n", file)
			}
			if len(match.Files) > 4 {
				fmt.Fprintf(w, "      ... %d more files\n", len(match.Files)-4)
			}
		}
	}

	if len(issues) > 0 {
		fmt.Fprintf(w, "\n%s\n", issuesHeading)
		for _, match := range issues {
			fmt.Fprintf(w, "  - %s%s [%s]\n", match.Title, reportYear(match.Year), match.Key)
			for _, issue := range limitStrings(match.Issues, 6) {
				fmt.Fprintf(w, "      issue: %s\n", issue)
			}
		}
	}

	if len(multiEpisode) > 0 {
		fmt.Fprintln(w, "\nMulti-episode files")
		for _, plan := range multiEpisode {
			fmt.Fprintf(w, "  - %s%s %s files=%d\n", plan.Title, reportYear(plan.Year), formatTVPlanEpisodes(plan), len(plan.Files))
			for _, file := range limitStrings(plan.Files, 3) {
				fmt.Fprintf(w, "      file: %s\n", file)
			}
		}
	}

	if len(plexmatches) > 0 {
		fmt.Fprintln(w, "\nPlexmatch files")
		for _, ev := range limitEvents(plexmatches, 20) {
			fmt.Fprintf(w, "  - %s", ev.RelPath)
			if title, ok := ev.Data["title"].(string); ok && title != "" {
				fmt.Fprintf(w, " title=%q", title)
			}
			if year, ok := ev.Data["year"].(string); ok && year != "" {
				fmt.Fprintf(w, " year=%s", year)
			}
			fmt.Fprintln(w)
		}
	}

	if len(result.TVMetadata) > 0 && len(result.TVMaterialize) == 0 {
		fmt.Fprintln(w, "\nMetadata fetch preview")
		for _, item := range fetchFailed {
			fmt.Fprintf(w, "  - %s failed provider=%s error=%s\n", item.Key, item.ProviderID, item.Error)
		}
		for _, item := range fetched {
			fmt.Fprintf(w, "  - %s%s provider=%s would_apply=%s", item.Title, reportYear(item.Year), item.ProviderID, strings.Join(item.WouldApply, ","))
			if item.Seasons > 0 {
				fmt.Fprintf(w, " seasons=%d", item.Seasons)
			}
			if item.RemoteEpisodes > 0 {
				fmt.Fprintf(w, " remote_episodes=%d", item.RemoteEpisodes)
			}
			if item.PlannedEpisodes > 0 {
				fmt.Fprintf(w, " mapped=%d/%d", item.MappedEpisodes, item.PlannedEpisodes)
			}
			if item.PlannedFiles > 0 {
				fmt.Fprintf(w, " files=%d", item.PlannedFiles)
			}
			if item.LocalIdentities > 1 {
				fmt.Fprintf(w, " local_identities=%d", item.LocalIdentities)
			}
			if len(item.MissingEpisodes) > 0 {
				fmt.Fprintf(w, " missing=%s", formatTVEpisodeRefs(item.MissingEpisodes, 6))
			}
			if len(item.Networks) > 0 {
				fmt.Fprintf(w, " networks=%q", strings.Join(limitStrings(item.Networks, 3), ", "))
			}
			if item.Status != "" {
				fmt.Fprintf(w, " status=%q", item.Status)
			}
			if item.Artwork > 0 {
				fmt.Fprintf(w, " artwork=%d", item.Artwork)
			}
			if item.Cast > 0 {
				fmt.Fprintf(w, " cast=%d", item.Cast)
			}
			fmt.Fprintln(w)
		}
	}

	if len(result.TVMaterialize) > 0 {
		if len(materializeBlocked) > 0 {
			fmt.Fprintln(w, "\nMaterialization blocked")
			for _, item := range materializeBlocked {
				writeTVMaterializeResult(w, item)
			}
		}
		if len(materializeRepair) > 0 {
			fmt.Fprintln(w, "\nMaterialization repairs")
			for _, item := range materializeRepair {
				writeTVMaterializeResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nMaterialization preview")
		for _, item := range append(materializeCreate, materializeUpdate...) {
			writeTVMaterializeResult(w, item)
		}
	}

	if len(result.TVApply) > 0 {
		if len(applyFailed) > 0 || len(applySkipped) > 0 {
			fmt.Fprintln(w, "\nApply skipped or failed")
			for _, item := range append(applyFailed, applySkipped...) {
				writeTVApplyResult(w, item)
			}
		}
		fmt.Fprintln(w, "\nApply results")
		applyVisible := append(append(applyRepair, applyCreate...), applyUpdate...)
		for _, item := range applyVisible {
			writeTVApplyResult(w, item)
		}
	}

	if len(grouped) > 0 {
		fmt.Fprintf(w, "\n%s\n", overviewTitle)
		for _, match := range limitTVMatches(grouped, 30) {
			fmt.Fprintf(w, "  - %s%s [%s] plans=%d files=%d episodes=%s", match.Title, reportYear(match.Year), match.Key, len(match.Plans), len(match.Files), formatTVMatchEpisodes(match))
			if len(match.NFOs) > 0 {
				fmt.Fprintf(w, " nfo=%d", len(match.NFOs))
			}
			if len(match.Plexmatches) > 0 {
				fmt.Fprintf(w, " plexmatch=%d", len(match.Plexmatches))
			}
			if len(match.Assets) > 0 {
				fmt.Fprintf(w, " assets=%d", len(match.Assets))
			}
			if len(match.Subtitles) > 0 {
				fmt.Fprintf(w, " subtitles=%d", len(match.Subtitles))
			}
			fmt.Fprintln(w)
		}
		if len(grouped) > 30 {
			fmt.Fprintf(w, "  ... %d more shows\n", len(grouped)-30)
		}
	}

	if len(unplanned) > 0 {
		fmt.Fprintln(w, "\nUnplanned media")
		for _, ev := range limitEvents(unplanned, 30) {
			fmt.Fprintf(w, "  - %s", ev.RelPath)
			if ev.Reason != "" {
				fmt.Fprintf(w, " (%s)", ev.Reason)
			}
			if ev.Message != "" {
				fmt.Fprintf(w, ": %s", ev.Message)
			}
			fmt.Fprintln(w)
		}
		if len(unplanned) > 30 {
			fmt.Fprintf(w, "  ... %d more\n", len(unplanned)-30)
		}
	}

	if len(localExtras) > 0 {
		fmt.Fprintln(w, "\nLocal extras")
		writeLocalExtras(w, localExtras, 30)
	}

	if len(nfoFailures) > 0 {
		fmt.Fprintln(w, "\nNFO parse failures")
		for _, ev := range limitEvents(nfoFailures, 20) {
			fmt.Fprintf(w, "  - %s\n", ev.RelPath)
		}
		if len(nfoFailures) > 20 {
			fmt.Fprintf(w, "  ... %d more\n", len(nfoFailures)-20)
		}
	}

	if len(result.TVSearch) == 0 {
		fmt.Fprintln(w, "\nSearch was not run. Add --search to include heya.media candidate review.")
	}
}

func writeApplyResult(w io.Writer, item MovieApplyResult) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.MovieRowAction != "" {
		fmt.Fprintf(w, " movie=%s", item.MovieRowAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Error != "" {
		fmt.Fprintf(w, " error=%s", item.Error)
	}
	fileCounts := map[string]int{}
	if item.FilesCreated > 0 {
		fileCounts["created"] = item.FilesCreated
	}
	if item.FilesAttached > 0 {
		fileCounts["attached"] = item.FilesAttached
	}
	if item.FilesAlreadyAttached > 0 {
		fileCounts["already_attached"] = item.FilesAlreadyAttached
	}
	if item.FilesReassigned > 0 {
		fileCounts["reassigned"] = item.FilesReassigned
	}
	if counts := formatCounts(fileCounts); counts != "" {
		fmt.Fprintf(w, " files=%s", counts)
	}
	if item.LocalAssets > 0 || item.RemoteAssets > 0 {
		fmt.Fprintf(w, " assets=local:%d,remote:%d", item.LocalAssets, item.RemoteAssets)
	}
	if item.RichMetadata {
		fmt.Fprintf(w, " rich=true")
	}
	fmt.Fprintln(w)
}

func writeBookApplyResult(w io.Writer, item BookApplyResult) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.Author != "" {
		fmt.Fprintf(w, " by %s", item.Author)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.BookRowAction != "" {
		fmt.Fprintf(w, " book=%s", item.BookRowAction)
	}
	if item.Format != "" {
		fmt.Fprintf(w, " format=%s", item.Format)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Error != "" {
		fmt.Fprintf(w, " error=%s", item.Error)
	}
	fileCounts := map[string]int{}
	if item.FilesCreated > 0 {
		fileCounts["created"] = item.FilesCreated
	}
	if item.FilesAttached > 0 {
		fileCounts["attached"] = item.FilesAttached
	}
	if item.FilesAlreadyAttached > 0 {
		fileCounts["already_attached"] = item.FilesAlreadyAttached
	}
	if item.FilesReassigned > 0 {
		fileCounts["reassigned"] = item.FilesReassigned
	}
	if counts := formatCounts(fileCounts); counts != "" {
		fmt.Fprintf(w, " files=%s", counts)
	}
	if item.LocalAssets > 0 || item.RemoteAssets > 0 {
		fmt.Fprintf(w, " assets=local:%d,remote:%d", item.LocalAssets, item.RemoteAssets)
	}
	fmt.Fprintln(w)
}

func writeMaterializeResult(w io.Writer, item MovieMaterializePreview) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.MovieRowAction != "" {
		fmt.Fprintf(w, " movie=%s", item.MovieRowAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Collection != "" {
		fmt.Fprintf(w, " collection=%q", item.Collection)
	}
	if item.RemoteArtwork > 0 {
		fmt.Fprintf(w, " artwork=%d", item.RemoteArtwork)
	}
	if item.LocalAssets > 0 {
		fmt.Fprintf(w, " local_assets=%d", item.LocalAssets)
	}
	if item.Cast > 0 {
		fmt.Fprintf(w, " cast=%d", item.Cast)
	}
	if item.Crew > 0 {
		fmt.Fprintf(w, " crew=%d", item.Crew)
	}
	if len(item.FileActions) > 0 {
		counts := map[string]int{}
		for _, file := range item.FileActions {
			counts[file.Action]++
		}
		fmt.Fprintf(w, " files=%s", formatCounts(counts))
	}
	fmt.Fprintln(w)
	for _, issue := range limitStrings(item.Issues, 4) {
		fmt.Fprintf(w, "      issue: %s\n", issue)
	}
	if len(item.Issues) > 4 {
		fmt.Fprintf(w, "      ... %d more issues\n", len(item.Issues)-4)
	}
	for _, file := range item.FileActions {
		if file.Action != "reassign_library_file" {
			continue
		}
		fmt.Fprintf(w, "      repair: file=%d reassign from media_item=%d", file.FileID, file.ExistingMediaItemID)
		if file.ExistingItem != nil {
			fmt.Fprintf(w, " %s%s", file.ExistingItem.Title, reportYear(file.ExistingItem.Year))
			if ids := formatExternalIDs(file.ExistingItem.ExternalIDs); ids != "" {
				fmt.Fprintf(w, " ids=%s", ids)
			}
		}
		fmt.Fprintln(w)
	}
}

func writeBookMaterializeResult(w io.Writer, item BookMaterializePreview) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.Author != "" {
		fmt.Fprintf(w, " by %s", item.Author)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.BookRowAction != "" {
		fmt.Fprintf(w, " book=%s", item.BookRowAction)
	}
	if item.Format != "" {
		fmt.Fprintf(w, " format=%s", item.Format)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.PageCount > 0 {
		fmt.Fprintf(w, " pages=%d", item.PageCount)
	}
	if item.Subjects > 0 {
		fmt.Fprintf(w, " subjects=%d", item.Subjects)
	}
	if item.RemoteArtwork > 0 {
		fmt.Fprintf(w, " artwork=%d", item.RemoteArtwork)
	}
	if item.LocalAssets > 0 {
		fmt.Fprintf(w, " local_assets=%d", item.LocalAssets)
	}
	if len(item.FileActions) > 0 {
		counts := map[string]int{}
		for _, file := range item.FileActions {
			counts[file.Action]++
		}
		fmt.Fprintf(w, " files=%s", formatCounts(counts))
	}
	fmt.Fprintln(w)
	for _, issue := range limitStrings(item.Issues, 4) {
		fmt.Fprintf(w, "      issue: %s\n", issue)
	}
	if len(item.Issues) > 4 {
		fmt.Fprintf(w, "      ... %d more issues\n", len(item.Issues)-4)
	}
	for _, file := range item.FileActions {
		if file.Action != "reassign_library_file" {
			continue
		}
		fmt.Fprintf(w, "      repair: file=%d reassign from media_item=%d", file.FileID, file.ExistingMediaItemID)
		if file.ExistingItem != nil {
			fmt.Fprintf(w, " %s%s", file.ExistingItem.Title, reportYear(file.ExistingItem.Year))
			if ids := formatExternalIDs(file.ExistingItem.ExternalIDs); ids != "" {
				fmt.Fprintf(w, " ids=%s", ids)
			}
		}
		fmt.Fprintln(w)
	}
}

func writeSearchResult(w io.Writer, item MovieSearchMatch) {
	status := "rejected"
	if item.Accepted {
		status = "selected"
	}
	fmt.Fprintf(w, "  - %s%s [%s] %s", item.Query.Title, reportYear(item.Query.Year), item.Key, status)
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.Confidence > 0 {
		fmt.Fprintf(w, " score=%.2f", item.Confidence)
	}
	fmt.Fprintln(w)

	for i, c := range limitCandidates(item.Candidates, reportCandidateLimit) {
		fmt.Fprintf(w, "      %d. %s%s score=%.2f id=%s", i+1, c.Title, reportYear(c.Year), c.Confidence, c.ProviderID)
		if ids := formatExternalIDs(c.ExternalIDs); ids != "" {
			fmt.Fprintf(w, " ids=%s", ids)
		}
		fmt.Fprintln(w)
	}
	if len(item.Candidates) > reportCandidateLimit {
		fmt.Fprintf(w, "      ... %d more candidates\n", len(item.Candidates)-reportCandidateLimit)
	}
}

func writeBookSearchResult(w io.Writer, item BookSearchMatch) {
	status := "rejected"
	if item.Accepted {
		status = "selected"
	}
	fmt.Fprintf(w, "  - %s%s", item.Query.Title, reportYear(item.Query.Year))
	if item.Query.Author != "" {
		fmt.Fprintf(w, " by %s", item.Query.Author)
	}
	fmt.Fprintf(w, " [%s] %s", item.Key, status)
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.Confidence > 0 {
		fmt.Fprintf(w, " score=%.2f", item.Confidence)
	}
	fmt.Fprintln(w)
	for i, c := range limitBookCandidates(item.Candidates, reportCandidateLimit) {
		fmt.Fprintf(w, "      %d. %s%s score=%.2f id=%s", i+1, c.Title, reportYear(c.Year), c.Confidence, c.ProviderID)
		if c.Author != "" {
			fmt.Fprintf(w, " author=%q", c.Author)
		}
		if ids := formatExternalIDs(c.ExternalIDs); ids != "" {
			fmt.Fprintf(w, " ids=%s", ids)
		}
		fmt.Fprintln(w)
	}
	if len(item.Candidates) > reportCandidateLimit {
		fmt.Fprintf(w, "      ... %d more candidates\n", len(item.Candidates)-reportCandidateLimit)
	}
}

func writeTVSearchResult(w io.Writer, item TVSearchMatch) {
	status := "rejected"
	if item.Accepted {
		status = "selected"
	}
	fmt.Fprintf(w, "  - %s%s [%s] %s", item.Query.Title, reportYear(item.Query.Year), item.Key, status)
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.Confidence > 0 {
		fmt.Fprintf(w, " score=%.2f", item.Confidence)
	}
	fmt.Fprintln(w)

	for i, c := range limitTVCandidates(item.Candidates, reportCandidateLimit) {
		fmt.Fprintf(w, "      %d. %s%s score=%.2f id=%s", i+1, c.Title, reportYear(c.Year), c.Confidence, c.ProviderID)
		if ids := formatExternalIDs(c.ExternalIDs); ids != "" {
			fmt.Fprintf(w, " ids=%s", ids)
		}
		fmt.Fprintln(w)
	}
	if len(item.Candidates) > reportCandidateLimit {
		fmt.Fprintf(w, "      ... %d more candidates\n", len(item.Candidates)-reportCandidateLimit)
	}
}

func writeMusicSearchResult(w io.Writer, item MusicSearchMatch) {
	status := "rejected"
	if item.Accepted {
		status = "selected"
	}
	fmt.Fprintf(w, "  - %s [%s] %s", item.Query.Artist, item.Key, status)
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Error != "" {
		fmt.Fprintf(w, " error=%q", item.Error)
	}
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.Confidence > 0 {
		fmt.Fprintf(w, " score=%.2f", item.Confidence)
	}
	fmt.Fprintln(w)

	for i, c := range limitMusicCandidates(item.Candidates, reportCandidateLimit) {
		fmt.Fprintf(w, "      %d. %s score=%.2f id=%s", i+1, c.Artist, c.Confidence, c.ProviderID)
		if ids := formatExternalIDs(c.ExternalIDs); ids != "" {
			fmt.Fprintf(w, " ids=%s", ids)
		}
		fmt.Fprintln(w)
	}
	if len(item.Candidates) > reportCandidateLimit {
		fmt.Fprintf(w, "      ... %d more candidates\n", len(item.Candidates)-reportCandidateLimit)
	}
}

func writeTVMaterializeResult(w io.Writer, item TVMaterializePreview) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.TVSeriesAction != "" {
		fmt.Fprintf(w, " tv=%s", item.TVSeriesAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	seasonCounts := map[string]int{}
	if item.SeasonsCreate > 0 {
		seasonCounts["create"] = item.SeasonsCreate
	}
	if item.SeasonsExisting > 0 {
		seasonCounts["existing"] = item.SeasonsExisting
	}
	if counts := formatCounts(seasonCounts); counts != "" {
		fmt.Fprintf(w, " seasons=%s", counts)
	}
	episodeCounts := map[string]int{}
	if item.EpisodesCreate > 0 {
		episodeCounts["create"] = item.EpisodesCreate
	}
	if item.EpisodesExisting > 0 {
		episodeCounts["existing"] = item.EpisodesExisting
	}
	if counts := formatCounts(episodeCounts); counts != "" {
		fmt.Fprintf(w, " episodes=%s", counts)
	} else if item.RemoteEpisodes > 0 {
		fmt.Fprintf(w, " remote_episodes=%d", item.RemoteEpisodes)
	}
	if item.PlannedEpisodes > 0 {
		fmt.Fprintf(w, " mapped=%d/%d", item.MappedEpisodes, item.PlannedEpisodes)
	}
	if len(item.FileActions) > 0 {
		counts := map[string]int{}
		for _, file := range item.FileActions {
			counts[file.Action]++
		}
		fmt.Fprintf(w, " files=%s", formatCounts(counts))
	}
	if item.LocalAssets > 0 {
		fmt.Fprintf(w, " local_assets=%d", item.LocalAssets)
	}
	if item.RemoteArtwork > 0 {
		fmt.Fprintf(w, " artwork=%d", item.RemoteArtwork)
	}
	if item.Networks > 0 {
		fmt.Fprintf(w, " networks=%d", item.Networks)
	}
	if item.Cast > 0 {
		fmt.Fprintf(w, " cast=%d", item.Cast)
	}
	if item.Crew > 0 {
		fmt.Fprintf(w, " crew=%d", item.Crew)
	}
	fmt.Fprintln(w)
	for _, issue := range limitStrings(item.Issues, 4) {
		fmt.Fprintf(w, "      issue: %s\n", issue)
	}
	if len(item.Issues) > 4 {
		fmt.Fprintf(w, "      ... %d more issues\n", len(item.Issues)-4)
	}
	for _, file := range item.FileActions {
		if file.Action != "reassign_library_file" {
			continue
		}
		fmt.Fprintf(w, "      repair: file=%d reassign from media_item=%d", file.FileID, file.ExistingMediaItemID)
		if file.ExistingItem != nil {
			fmt.Fprintf(w, " %s%s", file.ExistingItem.Title, reportYear(file.ExistingItem.Year))
			if ids := formatExternalIDs(file.ExistingItem.ExternalIDs); ids != "" {
				fmt.Fprintf(w, " ids=%s", ids)
			}
		}
		fmt.Fprintln(w)
	}
}

func writeMusicMaterializeResult(w io.Writer, item MusicMaterializePreview) {
	fmt.Fprintf(w, "  - %s %s", item.Action, item.Artist)
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.ArtistID != 0 {
		fmt.Fprintf(w, " artist=%d", item.ArtistID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.ArtistRowAction != "" {
		fmt.Fprintf(w, " artist_row=%s", item.ArtistRowAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	albumCounts := map[string]int{}
	if item.AlbumsCreate > 0 {
		albumCounts["create"] = item.AlbumsCreate
	}
	if item.AlbumsUpdate > 0 {
		albumCounts["update"] = item.AlbumsUpdate
	}
	if counts := formatCounts(albumCounts); counts != "" {
		fmt.Fprintf(w, " albums=%s", counts)
	}
	trackCounts := map[string]int{}
	if item.TracksCreate > 0 {
		trackCounts["create"] = item.TracksCreate
	}
	if item.TracksUpdate > 0 {
		trackCounts["update"] = item.TracksUpdate
	}
	if counts := formatCounts(trackCounts); counts != "" {
		fmt.Fprintf(w, " tracks=%s", counts)
	}
	trackFileCounts := map[string]int{}
	if item.TrackFilesCreate > 0 {
		trackFileCounts["create"] = item.TrackFilesCreate
	}
	if item.TrackFilesUpdate > 0 {
		trackFileCounts["update"] = item.TrackFilesUpdate
	}
	if counts := formatCounts(trackFileCounts); counts != "" {
		fmt.Fprintf(w, " track_files=%s", counts)
	}
	if len(item.FileActions) > 0 {
		counts := map[string]int{}
		for _, file := range item.FileActions {
			counts[file.Action]++
		}
		fmt.Fprintf(w, " files=%s", formatCounts(counts))
	}
	if item.RemoteArtwork > 0 {
		fmt.Fprintf(w, " artwork=%d", item.RemoteArtwork)
	}
	if item.Tags > 0 {
		fmt.Fprintf(w, " tags=%d", item.Tags)
	}
	fmt.Fprintln(w)
	for _, issue := range limitStrings(item.Issues, 5) {
		fmt.Fprintf(w, "      issue: %s\n", issue)
	}
	if len(item.Issues) > 5 {
		fmt.Fprintf(w, "      ... %d more issues\n", len(item.Issues)-5)
	}
	for _, album := range limitMusicMaterializeAlbums(item.AlbumActions, 5) {
		fmt.Fprintf(w, "      album: %s", album.RemoteAlbum)
		if album.RemoteAlbum == "" {
			fmt.Fprintf(w, "%s", album.LocalAlbum)
		}
		if album.Year != "" {
			fmt.Fprintf(w, " (%s)", album.Year)
		}
		fmt.Fprintf(w, " %s", album.Action)
		if album.AlbumID != 0 {
			fmt.Fprintf(w, " id=%d", album.AlbumID)
		}
		counts := map[string]int{}
		if album.TracksCreate > 0 {
			counts["tracks_create"] = album.TracksCreate
		}
		if album.TracksUpdate > 0 {
			counts["tracks_update"] = album.TracksUpdate
		}
		if formatted := formatCounts(counts); formatted != "" {
			fmt.Fprintf(w, " %s", formatted)
		}
		fmt.Fprintln(w)
	}
	if len(item.AlbumActions) > 5 {
		fmt.Fprintf(w, "      ... %d more albums\n", len(item.AlbumActions)-5)
	}
	for _, file := range item.FileActions {
		if file.Action != "reassign_library_file" {
			continue
		}
		fmt.Fprintf(w, "      repair: file=%d reassign from media_item=%d", file.FileID, file.ExistingMediaItemID)
		if file.ExistingItem != nil {
			fmt.Fprintf(w, " %s%s", file.ExistingItem.Title, reportYear(file.ExistingItem.Year))
			if ids := formatExternalIDs(file.ExistingItem.ExternalIDs); ids != "" {
				fmt.Fprintf(w, " ids=%s", ids)
			}
		}
		fmt.Fprintln(w)
	}
}

func writeMusicApplyResult(w io.Writer, item MusicApplyResult) {
	fmt.Fprintf(w, "  - %s %s", item.Action, item.Artist)
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.ArtistID != 0 {
		fmt.Fprintf(w, " artist=%d", item.ArtistID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.ArtistRowAction != "" {
		fmt.Fprintf(w, " artist_row=%s", item.ArtistRowAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Error != "" {
		fmt.Fprintf(w, " error=%s", item.Error)
	}
	albumCounts := map[string]int{}
	if item.AlbumsCreated > 0 {
		albumCounts["created"] = item.AlbumsCreated
	}
	if item.AlbumsUpdated > 0 {
		albumCounts["updated"] = item.AlbumsUpdated
	}
	if counts := formatCounts(albumCounts); counts != "" {
		fmt.Fprintf(w, " albums=%s", counts)
	}
	trackCounts := map[string]int{}
	if item.TracksCreated > 0 {
		trackCounts["created"] = item.TracksCreated
	}
	if item.TracksUpdated > 0 {
		trackCounts["updated"] = item.TracksUpdated
	}
	if counts := formatCounts(trackCounts); counts != "" {
		fmt.Fprintf(w, " tracks=%s", counts)
	}
	trackFileCounts := map[string]int{}
	if item.TrackFilesCreated > 0 {
		trackFileCounts["created"] = item.TrackFilesCreated
	}
	if item.TrackFilesUpdated > 0 {
		trackFileCounts["updated"] = item.TrackFilesUpdated
	}
	if counts := formatCounts(trackFileCounts); counts != "" {
		fmt.Fprintf(w, " track_files=%s", counts)
	}
	fileCounts := map[string]int{}
	if item.FilesCreated > 0 {
		fileCounts["created"] = item.FilesCreated
	}
	if item.FilesAttached > 0 {
		fileCounts["attached"] = item.FilesAttached
	}
	if item.FilesAlreadyAttached > 0 {
		fileCounts["already_attached"] = item.FilesAlreadyAttached
	}
	if item.FilesReassigned > 0 {
		fileCounts["reassigned"] = item.FilesReassigned
	}
	if counts := formatCounts(fileCounts); counts != "" {
		fmt.Fprintf(w, " files=%s", counts)
	}
	fmt.Fprintln(w)
}

func writeTVApplyResult(w io.Writer, item TVApplyResult) {
	fmt.Fprintf(w, "  - %s %s%s", item.Action, item.Title, reportYear(item.Year))
	if item.ProviderID != "" {
		fmt.Fprintf(w, " provider=%s", item.ProviderID)
	}
	if item.MediaItemID != 0 {
		fmt.Fprintf(w, " media_item=%d", item.MediaItemID)
	}
	if item.TVSeriesID != 0 {
		fmt.Fprintf(w, " tv_series=%d", item.TVSeriesID)
	}
	if item.MediaItemAction != "" {
		fmt.Fprintf(w, " media=%s", item.MediaItemAction)
	}
	if item.TVSeriesAction != "" {
		fmt.Fprintf(w, " tv=%s", item.TVSeriesAction)
	}
	if item.Reason != "" {
		fmt.Fprintf(w, " reason=%s", item.Reason)
	}
	if item.Error != "" {
		fmt.Fprintf(w, " error=%s", item.Error)
	}
	fileCounts := map[string]int{}
	if item.FilesCreated > 0 {
		fileCounts["created"] = item.FilesCreated
	}
	if item.FilesAttached > 0 {
		fileCounts["attached"] = item.FilesAttached
	}
	if item.FilesAlreadyAttached > 0 {
		fileCounts["already_attached"] = item.FilesAlreadyAttached
	}
	if item.FilesReassigned > 0 {
		fileCounts["reassigned"] = item.FilesReassigned
	}
	if counts := formatCounts(fileCounts); counts != "" {
		fmt.Fprintf(w, " files=%s", counts)
	}
	if item.LocalAssets > 0 || item.RemoteAssets > 0 {
		fmt.Fprintf(w, " assets=local:%d,remote:%d", item.LocalAssets, item.RemoteAssets)
	}
	if item.RichMetadata {
		fmt.Fprintf(w, " rich=true")
	}
	fmt.Fprintln(w)
}

func splitSearchResults(search []MovieSearchMatch) (rejected, selected, suspicious []MovieSearchMatch) {
	for _, item := range search {
		if !item.Accepted {
			if item.ManualDecision == "" {
				rejected = append(rejected, item)
			}
			continue
		}
		selected = append(selected, item)
		if item.ManualDecision == "" && searchSelectionLooksSuspicious(item) {
			suspicious = append(suspicious, item)
		}
	}
	sortSearchResults(rejected)
	sortSearchResults(suspicious)
	return rejected, selected, suspicious
}

func searchSelectionLooksSuspicious(item MovieSearchMatch) bool {
	if item.Confidence < 0.95 {
		return true
	}
	if item.Query.Year != "" && item.Year != "" && item.Query.Year != item.Year {
		return true
	}
	selected := normalizeSearchTitle(item.Title)
	if normalizeSearchTitle(item.Query.Title) == selected {
		return false
	}
	for _, alias := range item.Query.Aliases {
		if normalizeSearchTitle(alias) == selected {
			return false
		}
	}
	return true
}

func sortSearchResults(items []MovieSearchMatch) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Query.Title == items[j].Query.Title {
			return items[i].Query.Year < items[j].Query.Year
		}
		return items[i].Query.Title < items[j].Query.Title
	})
}

func splitTVSearchResults(search []TVSearchMatch) (rejected, selected, suspicious []TVSearchMatch) {
	for _, item := range search {
		if !item.Accepted {
			if item.ManualDecision == "" {
				rejected = append(rejected, item)
			}
			continue
		}
		selected = append(selected, item)
		if item.ManualDecision == "" && tvSearchSelectionLooksSuspicious(item) {
			suspicious = append(suspicious, item)
		}
	}
	sortTVSearchResults(rejected)
	sortTVSearchResults(suspicious)
	return rejected, selected, suspicious
}

func tvSearchSelectionLooksSuspicious(item TVSearchMatch) bool {
	if item.Confidence < 0.95 {
		return true
	}
	if item.Query.Year != "" && item.Year != "" && item.Query.Year != item.Year {
		return true
	}
	selected := normalizeSearchTitle(item.Title)
	if normalizeSearchTitle(item.Query.Title) == selected {
		return false
	}
	for _, alias := range item.Query.Aliases {
		if normalizeSearchTitle(alias) == selected {
			return false
		}
	}
	return true
}

func sortTVSearchResults(items []TVSearchMatch) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Query.Title == items[j].Query.Title {
			return items[i].Query.Year < items[j].Query.Year
		}
		return items[i].Query.Title < items[j].Query.Title
	})
}

func splitMusicSearchResults(search []MusicSearchMatch) (rejected, errors, selected, suspicious []MusicSearchMatch) {
	for _, item := range search {
		if !item.Accepted {
			if item.Error != "" || item.Reason == "search_error" {
				errors = append(errors, item)
				continue
			}
			if item.ManualDecision == "" {
				rejected = append(rejected, item)
			}
			continue
		}
		selected = append(selected, item)
		if item.ManualDecision == "" && musicSearchSelectionLooksSuspicious(item) {
			suspicious = append(suspicious, item)
		}
	}
	sortMusicSearchResults(rejected)
	sortMusicSearchResults(errors)
	sortMusicSearchResults(suspicious)
	return rejected, errors, selected, suspicious
}

func splitBookSearchResults(search []BookSearchMatch) (rejected, selected, suspicious []BookSearchMatch) {
	for _, item := range search {
		if !item.Accepted {
			if item.ManualDecision == "" {
				rejected = append(rejected, item)
			}
			continue
		}
		selected = append(selected, item)
		if item.ManualDecision == "" && bookSearchSelectionLooksSuspicious(item) {
			suspicious = append(suspicious, item)
		}
	}
	sortBookSearchResults(rejected)
	sortBookSearchResults(suspicious)
	return rejected, selected, suspicious
}

func bookSearchSelectionLooksSuspicious(item BookSearchMatch) bool {
	if item.Confidence < 0.85 {
		return true
	}
	if item.Query.Year != "" && item.Year != "" && item.Query.Year != item.Year {
		return true
	}
	selected := normalizeSearchTitle(item.Title)
	if normalizeSearchTitle(item.Query.Title) == selected {
		return false
	}
	for _, alias := range item.Query.Aliases {
		if normalizeSearchTitle(alias) == selected {
			return false
		}
	}
	return true
}

func sortBookSearchResults(items []BookSearchMatch) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Query.Author == items[j].Query.Author {
			if items[i].Query.Title == items[j].Query.Title {
				return items[i].Query.Year < items[j].Query.Year
			}
			return items[i].Query.Title < items[j].Query.Title
		}
		return items[i].Query.Author < items[j].Query.Author
	})
}

func musicSearchSelectionLooksSuspicious(item MusicSearchMatch) bool {
	if item.Confidence < 0.95 {
		return true
	}
	return normalizeMusicKeyPart(item.Query.Artist) != normalizeMusicKeyPart(item.Artist)
}

func musicTracksWithIssues(tracks []MusicTrackPlan) []MusicTrackPlan {
	out := make([]MusicTrackPlan, 0)
	for _, track := range tracks {
		if len(track.Issues) > 0 {
			out = append(out, track)
		}
	}
	sortMusicTracks(out)
	return out
}

func musicAlbumsWithIssues(albums []MusicAlbumPlan) []MusicAlbumPlan {
	out := make([]MusicAlbumPlan, 0)
	for _, album := range albums {
		if len(album.Issues) > 0 {
			out = append(out, album)
		}
	}
	sortMusicAlbums(out)
	return out
}

func countMusicArtistTracks(artist MusicArtistPlan) int {
	n := 0
	for _, album := range artist.Albums {
		n += len(album.Tracks)
	}
	return n
}

func groupedMovieMatches(matches []MovieMatch) []MovieMatch {
	out := make([]MovieMatch, 0)
	for _, match := range matches {
		if len(match.Plans) > 1 {
			out = append(out, match)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Title == out[j].Title {
			return out[i].Year < out[j].Year
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func splitMetadataPreviews(previews []MovieFetchPreview) (fetched, failed []MovieFetchPreview) {
	for _, preview := range previews {
		if preview.Error != "" {
			failed = append(failed, preview)
			continue
		}
		fetched = append(fetched, preview)
	}
	sort.Slice(fetched, func(i, j int) bool {
		if fetched[i].Title == fetched[j].Title {
			return fetched[i].Year < fetched[j].Year
		}
		return fetched[i].Title < fetched[j].Title
	})
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].Key < failed[j].Key
	})
	return fetched, failed
}

func splitTVMetadataPreviews(previews []TVFetchPreview) (fetched, failed []TVFetchPreview) {
	for _, preview := range previews {
		if preview.Error != "" {
			failed = append(failed, preview)
			continue
		}
		fetched = append(fetched, preview)
	}
	sort.Slice(fetched, func(i, j int) bool {
		if fetched[i].Title == fetched[j].Title {
			return fetched[i].Year < fetched[j].Year
		}
		return fetched[i].Title < fetched[j].Title
	})
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].Key < failed[j].Key
	})
	return fetched, failed
}

func splitMusicMetadataPreviews(previews []MusicFetchPreview) (fetched, failed []MusicFetchPreview) {
	for _, preview := range previews {
		if preview.Error != "" {
			failed = append(failed, preview)
			continue
		}
		fetched = append(fetched, preview)
	}
	sort.Slice(fetched, func(i, j int) bool {
		return strings.ToLower(fetched[i].Artist) < strings.ToLower(fetched[j].Artist)
	})
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].Key < failed[j].Key
	})
	return fetched, failed
}

func splitBookMetadataPreviews(previews []BookFetchPreview) (fetched, failed []BookFetchPreview) {
	for _, preview := range previews {
		if preview.Error != "" {
			failed = append(failed, preview)
			continue
		}
		fetched = append(fetched, preview)
	}
	sort.Slice(fetched, func(i, j int) bool {
		if fetched[i].Author == fetched[j].Author {
			if fetched[i].Title == fetched[j].Title {
				return fetched[i].Year < fetched[j].Year
			}
			return fetched[i].Title < fetched[j].Title
		}
		return fetched[i].Author < fetched[j].Author
	})
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].Key < failed[j].Key
	})
	return fetched, failed
}

func musicMetadataWithIssues(previews []MusicFetchPreview) []MusicFetchPreview {
	var out []MusicFetchPreview
	for _, preview := range previews {
		if len(preview.Issues) > 0 {
			out = append(out, preview)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Artist) < strings.ToLower(out[j].Artist)
	})
	return out
}

func countMappedMusicAlbums(previews []MusicFetchPreview) int {
	n := 0
	for _, preview := range previews {
		n += preview.MappedAlbums
	}
	return n
}

func countLocalMusicAlbums(previews []MusicFetchPreview) int {
	n := 0
	for _, preview := range previews {
		n += preview.LocalAlbums
	}
	return n
}

func countMappedMusicTracks(previews []MusicFetchPreview) int {
	n := 0
	for _, preview := range previews {
		n += preview.MappedTracks
	}
	return n
}

func countLocalMusicTracks(previews []MusicFetchPreview) int {
	n := 0
	for _, preview := range previews {
		n += preview.LocalTracks
	}
	return n
}

func splitTVMaterializePreviews(previews []TVMaterializePreview) (blocked, repair, create, update []TVMaterializePreview) {
	for _, preview := range previews {
		switch preview.Action {
		case "blocked":
			blocked = append(blocked, preview)
		case "repair":
			repair = append(repair, preview)
		case "create":
			create = append(create, preview)
		case "update":
			update = append(update, preview)
		default:
			update = append(update, preview)
		}
	}
	sortTVMaterializePreviews(blocked)
	sortTVMaterializePreviews(repair)
	sortTVMaterializePreviews(create)
	sortTVMaterializePreviews(update)
	return blocked, repair, create, update
}

func splitMusicMaterializePreviews(previews []MusicMaterializePreview) (blocked, repair, create, update []MusicMaterializePreview) {
	for _, preview := range previews {
		switch preview.Action {
		case "blocked":
			blocked = append(blocked, preview)
		case "repair":
			repair = append(repair, preview)
		case "create":
			create = append(create, preview)
		case "update":
			update = append(update, preview)
		default:
			update = append(update, preview)
		}
	}
	sortMusicMaterializePreviews(blocked)
	sortMusicMaterializePreviews(repair)
	sortMusicMaterializePreviews(create)
	sortMusicMaterializePreviews(update)
	return blocked, repair, create, update
}

func splitBookMaterializePreviews(previews []BookMaterializePreview) (blocked, repair, create, update []BookMaterializePreview) {
	for _, preview := range previews {
		switch preview.Action {
		case "blocked":
			blocked = append(blocked, preview)
		case "repair":
			repair = append(repair, preview)
		case "create":
			create = append(create, preview)
		case "update":
			update = append(update, preview)
		default:
			update = append(update, preview)
		}
	}
	sortBookMaterializePreviews(blocked)
	sortBookMaterializePreviews(repair)
	sortBookMaterializePreviews(create)
	sortBookMaterializePreviews(update)
	return blocked, repair, create, update
}

func splitTVApplyResults(results []TVApplyResult) (failed, skipped, repair, create, update []TVApplyResult) {
	for _, result := range results {
		switch result.Action {
		case "failed":
			failed = append(failed, result)
		case "skipped":
			skipped = append(skipped, result)
		case "repair":
			repair = append(repair, result)
		case "create":
			create = append(create, result)
		case "update":
			update = append(update, result)
		default:
			update = append(update, result)
		}
	}
	sortTVApplyResults(failed)
	sortTVApplyResults(skipped)
	sortTVApplyResults(repair)
	sortTVApplyResults(create)
	sortTVApplyResults(update)
	return failed, skipped, repair, create, update
}

func splitMusicApplyResults(results []MusicApplyResult) (failed, skipped, repair, create, update []MusicApplyResult) {
	for _, result := range results {
		switch result.Action {
		case "failed":
			failed = append(failed, result)
		case "skipped":
			skipped = append(skipped, result)
		case "repair":
			repair = append(repair, result)
		case "create":
			create = append(create, result)
		case "update":
			update = append(update, result)
		default:
			update = append(update, result)
		}
	}
	sortMusicApplyResults(failed)
	sortMusicApplyResults(skipped)
	sortMusicApplyResults(repair)
	sortMusicApplyResults(create)
	sortMusicApplyResults(update)
	return failed, skipped, repair, create, update
}

func splitBookApplyResults(results []BookApplyResult) (failed, skipped, repair, create, update []BookApplyResult) {
	for _, result := range results {
		switch result.Action {
		case "failed":
			failed = append(failed, result)
		case "skipped":
			skipped = append(skipped, result)
		case "repair":
			repair = append(repair, result)
		case "create":
			create = append(create, result)
		case "update":
			update = append(update, result)
		default:
			update = append(update, result)
		}
	}
	sortBookApplyResults(failed)
	sortBookApplyResults(skipped)
	sortBookApplyResults(repair)
	sortBookApplyResults(create)
	sortBookApplyResults(update)
	return failed, skipped, repair, create, update
}

func sortTVMaterializePreviews(items []TVMaterializePreview) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Title == items[j].Title {
			return items[i].Year < items[j].Year
		}
		return items[i].Title < items[j].Title
	})
}

func sortMusicMaterializePreviews(items []MusicMaterializePreview) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Artist < items[j].Artist
	})
}

func sortBookMaterializePreviews(items []BookMaterializePreview) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Author == items[j].Author {
			if items[i].Title == items[j].Title {
				return items[i].Year < items[j].Year
			}
			return items[i].Title < items[j].Title
		}
		return items[i].Author < items[j].Author
	})
}

func splitMaterializePreviews(previews []MovieMaterializePreview) (blocked, repair, create, update []MovieMaterializePreview) {
	for _, preview := range previews {
		switch preview.Action {
		case "blocked":
			blocked = append(blocked, preview)
		case "repair":
			repair = append(repair, preview)
		case "create":
			create = append(create, preview)
		case "update":
			update = append(update, preview)
		default:
			update = append(update, preview)
		}
	}
	sortMaterializePreviews(blocked)
	sortMaterializePreviews(repair)
	sortMaterializePreviews(create)
	sortMaterializePreviews(update)
	return blocked, repair, create, update
}

func splitMovieApplyResults(results []MovieApplyResult) (failed, skipped, repair, create, update []MovieApplyResult) {
	for _, result := range results {
		switch result.Action {
		case "failed":
			failed = append(failed, result)
		case "skipped":
			skipped = append(skipped, result)
		case "repair":
			repair = append(repair, result)
		case "create":
			create = append(create, result)
		case "update":
			update = append(update, result)
		default:
			update = append(update, result)
		}
	}
	sortMovieApplyResults(failed)
	sortMovieApplyResults(skipped)
	sortMovieApplyResults(repair)
	sortMovieApplyResults(create)
	sortMovieApplyResults(update)
	return failed, skipped, repair, create, update
}

func sortMaterializePreviews(items []MovieMaterializePreview) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Title == items[j].Title {
			return items[i].Year < items[j].Year
		}
		return items[i].Title < items[j].Title
	})
}

func eventsByName(events []Event, name string) []Event {
	var out []Event
	for _, ev := range events {
		if ev.Event == name {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RelPath < out[j].RelPath
	})
	return out
}

func reportYear(year string) string {
	if year == "" {
		return ""
	}
	return " (" + year + ")"
}

func formatExternalIDs(ids map[string]string) string {
	if len(ids) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if ids[key] == "" {
			continue
		}
		parts = append(parts, key+":"+ids[key])
	}
	return strings.Join(parts, ",")
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func limitCandidates(candidates []MovieSearchCandidate, n int) []MovieSearchCandidate {
	if len(candidates) <= n {
		return candidates
	}
	return candidates[:n]
}

func limitTVCandidates(candidates []TVSearchCandidate, n int) []TVSearchCandidate {
	if len(candidates) <= n {
		return candidates
	}
	return candidates[:n]
}

func limitMusicCandidates(candidates []MusicSearchCandidate, n int) []MusicSearchCandidate {
	if len(candidates) <= n {
		return candidates
	}
	return candidates[:n]
}

func limitBookCandidates(candidates []BookSearchCandidate, n int) []BookSearchCandidate {
	if len(candidates) <= n {
		return candidates
	}
	return candidates[:n]
}

func limitMusicTracks(tracks []MusicTrackPlan, n int) []MusicTrackPlan {
	if len(tracks) <= n {
		return tracks
	}
	return tracks[:n]
}

func limitMusicAlbums(albums []MusicAlbumPlan, n int) []MusicAlbumPlan {
	if len(albums) <= n {
		return albums
	}
	return albums[:n]
}

func limitMusicAlbumFetchMatches(albums []MusicAlbumFetchMatch, n int) []MusicAlbumFetchMatch {
	if len(albums) <= n {
		return albums
	}
	return albums[:n]
}

func limitMusicMaterializeAlbums(albums []MusicMaterializeAlbumAction, n int) []MusicMaterializeAlbumAction {
	if len(albums) <= n {
		return albums
	}
	return albums[:n]
}

func limitMusicCandidateFetchEvaluations(candidates []MusicCandidateFetchEvaluation, n int) []MusicCandidateFetchEvaluation {
	if len(candidates) <= n {
		return candidates
	}
	return candidates[:n]
}

func limitMusicArtists(artists []MusicArtistPlan, n int) []MusicArtistPlan {
	if len(artists) <= n {
		return artists
	}
	return artists[:n]
}

func limitEvents(events []Event, n int) []Event {
	if len(events) <= n {
		return events
	}
	return events[:n]
}

func limitStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func inventoryFilesByClass(inv Inventory, class FileClass) []InventoryFile {
	var out []InventoryFile
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if file.Class == class {
				out = append(out, file)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RelPath < out[j].RelPath
	})
	return out
}

func writeLocalExtras(w io.Writer, extras []InventoryFile, limit int) {
	for _, file := range limitInventoryFiles(extras, limit) {
		fmt.Fprintf(w, "  - %s", file.RelPath)
		if file.Kind != "" {
			fmt.Fprintf(w, " type=%s", file.Kind)
		}
		fmt.Fprintln(w)
	}
	if len(extras) > limit {
		fmt.Fprintf(w, "  ... %d more\n", len(extras)-limit)
	}
}

func limitInventoryFiles(values []InventoryFile, n int) []InventoryFile {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func titleOnlyTVMatches(matches []TVMatch, manualDecisionByKey map[string]string) []TVMatch {
	var out []TVMatch
	for _, match := range matches {
		if match.KeyType == "title" && manualDecisionByKey[match.Key] == "" {
			out = append(out, match)
		}
	}
	sortTVMatches(out)
	return out
}

func tvMatchesWithIssues(matches []TVMatch) []TVMatch {
	var out []TVMatch
	for _, match := range matches {
		if len(match.Issues) > 0 {
			out = append(out, match)
		}
	}
	sortTVMatches(out)
	return out
}

func groupedTVMatches(matches []TVMatch) []TVMatch {
	out := make([]TVMatch, 0)
	for _, match := range matches {
		if len(match.Plans) > 1 {
			out = append(out, match)
		}
	}
	sortTVMatches(out)
	return out
}

func sortTVMatches(items []TVMatch) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Title == items[j].Title {
			if items[i].Year == items[j].Year {
				return items[i].Key < items[j].Key
			}
			return items[i].Year < items[j].Year
		}
		return items[i].Title < items[j].Title
	})
}

func multiEpisodeTVPlans(plans []TVPlan) []TVPlan {
	var out []TVPlan
	for _, plan := range plans {
		if len(tvEpisodeRefsForPlan(plan)) > 1 {
			out = append(out, plan)
		}
	}
	sortTVPlans(out)
	return out
}

func countTVPlannedEpisodes(plans []TVPlan) int {
	n := 0
	for _, plan := range plans {
		n += len(tvEpisodeRefsForPlan(plan))
	}
	return n
}

func formatTVPlanEpisodes(plan TVPlan) string {
	if len(plan.AbsoluteEpisodes) > 0 && len(plan.Episodes) == 0 {
		return "absolute=" + joinInts(plan.AbsoluteEpisodes)
	}
	if len(plan.Episodes) == 0 {
		return "episode=unknown"
	}
	return fmt.Sprintf("S%02dE%s", plan.Season, joinIntsPadded(plan.Episodes, 2, ",E"))
}

func formatTVMatchEpisodes(match TVMatch) string {
	bySeason := map[int][]int{}
	var absolute []int
	for _, ref := range match.Episodes {
		if ref.Absolute > 0 && ref.Episode == 0 {
			absolute = append(absolute, ref.Absolute)
			continue
		}
		bySeason[ref.Season] = append(bySeason[ref.Season], ref.Episode)
	}
	var seasons []int
	for season := range bySeason {
		seasons = append(seasons, season)
	}
	sort.Ints(seasons)
	var parts []string
	for _, season := range seasons {
		episodes := uniqueInts(bySeason[season])
		parts = append(parts, fmt.Sprintf("S%02d=%d", season, len(episodes)))
	}
	if len(absolute) > 0 {
		parts = append(parts, fmt.Sprintf("absolute=%d", len(uniqueInts(absolute))))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

func formatTVEpisodeRefs(refs []TVEpisodeRef, limit int) string {
	if len(refs) == 0 {
		return ""
	}
	visible := refs
	if len(visible) > limit {
		visible = visible[:limit]
	}
	parts := make([]string, 0, len(visible)+1)
	for _, ref := range visible {
		switch {
		case ref.Absolute > 0 && ref.Episode == 0:
			parts = append(parts, fmt.Sprintf("absolute=%d", ref.Absolute))
		case ref.Episode > 0:
			parts = append(parts, fmt.Sprintf("S%02dE%02d", ref.Season, ref.Episode))
		default:
			parts = append(parts, "unknown")
		}
	}
	if len(refs) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(refs)-limit))
	}
	return strings.Join(parts, ",")
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ",")
}

func joinIntsPadded(values []int, width int, sep string) string {
	if len(values) == 0 {
		return ""
	}
	format := fmt.Sprintf("%%0%dd", width)
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf(format, value)
	}
	return strings.Join(parts, sep)
}

func limitTVMatches(values []TVMatch, n int) []TVMatch {
	if len(values) <= n {
		return values
	}
	return values[:n]
}
