package scanner

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestAcceptedCanonicalTVSearchClearsTitleOnlyReview(t *testing.T) {
	const key = "title:the bear"
	result := Result{
		TVMatches: []TVMatch{{Key: key, KeyType: "title", Title: "The Bear"}},
		TVSearch: []TVSearchMatch{{
			Key: key, Query: TVSearchQuery{Title: "The Bear"}, Accepted: true,
			ProviderID: "heyametadata:v2:entity:2a48be9f-f363-4f0c-be5c-26627da07e10",
			Title:      "The Bear", Year: "2022", Confidence: tvAutoMatchThreshold,
		}},
	}

	if status := scanIdentityReviewStatuses(result)[key]; status != "" {
		t.Fatalf("accepted canonical title-only review status = %q", status)
	}
	for _, finding := range scanFindingDrafts(result, nil) {
		if finding.Key == key && (finding.Code == "title_only_identity" || finding.Code == "search_suspicious") {
			t.Fatalf("accepted canonical title-only finding = %#v", finding)
		}
	}
}

func TestScanIdentityTargetsPromotesResolvedCandidateToCanonicalEntity(t *testing.T) {
	const (
		key       = "artist:daft punk"
		entityID  = "27cf4a80-dfd4-4e36-a262-f457e9671861"
		candidate = "heyametadata:v2:candidate:artist:0ef2e00c-cbbb-4717-992b-60ffcc1b70ff"
	)
	providers, _ := scanIdentityTargets(Result{
		MusicSearch:      []MusicSearchMatch{{Key: key, ProviderID: candidate}},
		MusicMaterialize: []MusicMaterializePreview{{Key: key, ProviderID: candidate}},
		MusicApply:       []MusicApplyResult{{Key: key, ProviderID: candidate}},
		MusicMetadata: []MusicFetchPreview{{
			Key: key,
			Detail: &metadata.MediaDetail{
				CanonicalID: entityID, CanonicalKind: "artist",
			},
		}},
	})
	require.Equal(t, "heyametadata:v2:entity:"+entityID, providers[key])
}

func TestPersistScanResultPersistsMusicScannerReviewState(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-music-persistence-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	matchedItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    lib.MediaType,
		Title:        "Ado",
		SortTitle:    "Ado",
		ExternalIds:  []byte(`{"mbid":"ado-artist"}`),
		ProviderKind: "mbid",
	})
	require.NoError(t, err)

	result := Result{
		MusicTracks: []MusicTrackPlan{
			{
				Key:        "track:broken",
				Artist:     "Broken Artist",
				Album:      "Broken Album",
				TrackTitle: "Untitled",
				RelPath:    "Broken Artist/Broken Album/track.mp3",
				Issues:     []string{"missing_track_number"},
			},
		},
		MusicAlbums: []MusicAlbumPlan{
			{
				Key:    musicAlbumKey("Broken Artist", "Broken Album", "2026"),
				Artist: "Broken Artist",
				Album:  "Broken Album",
				Year:   "2026",
				Issues: []string{"duplicate_album_identity"},
			},
		},
		MusicArtists: []MusicArtistPlan{
			{Key: "artist:ado", Artist: "Ado", Confidence: 0.99},
			{Key: "artist:broken artist", Artist: "Broken Artist", Confidence: 0.45},
			{Key: "artist:mapping artist", Artist: "Mapping Artist", Confidence: 0.9},
			{Key: "artist:local only", Artist: "Local Only", Confidence: 0.88},
		},
		MusicSearch: []MusicSearchMatch{
			{
				Key:        "artist:ado",
				Query:      MusicSearchQuery{Artist: "Ado"},
				Accepted:   true,
				ProviderID: "heya:artist:mbid:ado-artist",
				Provider:   "heya",
				Artist:     "Ado",
				Confidence: 0.99,
				Candidates: musicCandidates("ado", "Ado", 25),
				ExternalIDs: map[string]string{
					"mbid": "ado-artist",
				},
			},
			{
				Key:        "artist:broken artist",
				Query:      MusicSearchQuery{Artist: "Broken Artist"},
				Accepted:   false,
				Reason:     "ambiguous_or_low_confidence",
				Confidence: 0.44,
				Candidates: musicCandidates("broken", "Broken Artist", 2),
			},
			{
				Key:        "artist:mapping artist",
				Query:      MusicSearchQuery{Artist: "Mapping Artist"},
				Accepted:   true,
				ProviderID: "heya:artist:mbid:mapping-artist",
				Provider:   "heya",
				Artist:     "Mapping Artist",
				Confidence: 0.9,
				Candidates: musicCandidates("mapping", "Mapping Artist", 1),
			},
		},
		MusicMetadata: []MusicFetchPreview{
			{
				Key:          "artist:mapping artist",
				ProviderID:   "heya:artist:mbid:mapping-artist",
				Artist:       "Mapping Artist",
				LocalAlbums:  2,
				MappedAlbums: 1,
				LocalTracks:  4,
				MappedTracks: 2,
				Issues:       []string{"Some Album: remote_album_not_found"},
			},
		},
		MusicApply: []MusicApplyResult{
			{
				Key:         "artist:ado",
				Action:      "update",
				Artist:      "Ado",
				ProviderID:  "heya:artist:mbid:ado-artist",
				MediaItemID: matchedItem.ID,
			},
		},
	}
	events := []Event{{Event: "nfo.parse_failed", Severity: SeverityWarn, RelPath: "Broken Artist/Broken Album/album.nfo"}}

	scanRunID, err := PersistScanResult(ctx, lib, result, events, Options{
		Apply:              true,
		FetchPreview:       true,
		MaterializePreview: true,
		RemoteSearch:       true,
	}, pool, map[string]any{"music_artists": len(result.MusicArtists)})
	require.NoError(t, err)
	require.NotZero(t, scanRunID)

	identities := listScannerIdentitiesForTest(t, ctx, q, lib.ID)
	require.Len(t, identities, 4)
	byKey := scannerIdentitiesByKey(identities)

	require.Equal(t, "accepted", byKey["artist:ado"].ReviewStatus)
	require.True(t, byKey["artist:ado"].MediaItemID.Valid)
	require.Equal(t, matchedItem.ID, byKey["artist:ado"].MediaItemID.Int64)
	require.Equal(t, "needs_review", byKey["artist:broken artist"].ReviewStatus)
	require.Equal(t, "accepted", byKey["artist:mapping artist"].ReviewStatus)
	require.Equal(t, "accepted", byKey["artist:local only"].ReviewStatus)

	require.Len(t, listScannerCandidatesForTest(t, ctx, q, lib.ID, byKey["artist:ado"].ID), 20)
	require.Len(t, listScannerCandidatesForTest(t, ctx, q, lib.ID, byKey["artist:broken artist"].ID), 2)
	require.Len(t, listScannerCandidatesForTest(t, ctx, q, lib.ID, byKey["artist:mapping artist"].ID), 1)
	require.Empty(t, listScannerCandidatesForTest(t, ctx, q, lib.ID, byKey["artist:local only"].ID))

	require.Equal(t, map[string]int{
		"music_album_issue": 1,
		"music_track_issue": 1,
		"nfo_parse_failed":  1,
		"search_rejected":   1,
	}, scannerOpenFindingCounts(t, ctx, q, lib.ID))

	// Stage retries and force scans can replay the same path-scoped parse
	// event. It should replace the prior open finding instead of accumulating
	// another scan-level issue with no identity.
	_, err = PersistScanResult(ctx, lib, result, events, Options{
		Apply:              true,
		FetchPreview:       true,
		MaterializePreview: true,
		RemoteSearch:       true,
	}, pool, map[string]any{"music_artists": len(result.MusicArtists)})
	require.NoError(t, err)
	require.Equal(t, 1, scannerOpenFindingCounts(t, ctx, q, lib.ID)["nfo_parse_failed"])
	brokenCandidates := listScannerCandidatesForTest(t, ctx, q, lib.ID, byKey["artist:broken artist"].ID)
	require.NotEmpty(t, brokenCandidates)

	approved, err := q.ApproveScannerCandidate(ctx, sqlc.ApproveScannerCandidateParams{
		LibraryID:   lib.ID,
		IdentityID:  byKey["artist:broken artist"].ID,
		CandidateID: brokenCandidates[0].ID,
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", approved.ReviewStatus)
	require.False(t, approved.MediaItemID.Valid, "manual approval should wait for a follow-up apply to attach media")
}

func TestPersistScannerSearchEntitiesStoresNarrowArtifacts(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-entity-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    "/media/movies/Dune (2021)/Dune.mkv",
				RelPath: "Dune (2021)/Dune.mkv",
				Name:    "Dune.mkv",
				Class:   ClassPrimaryMedia,
			}, {
				Root:    "/media/movies",
				Path:    "/media/movies/The Matrix (1999)/The Matrix.mkv",
				RelPath: "The Matrix (1999)/The Matrix.mkv",
				Name:    "The Matrix.mkv",
				Class:   ClassPrimaryMedia,
			}},
		}}},
		MovieMatches: []MovieMatch{{
			Key:   "title_year:dune|2021",
			Title: "Dune",
			Year:  "2021",
			Files: []string{"Dune (2021)/Dune.mkv"},
		}, {
			Key:   "title_year:matrix|1999",
			Title: "The Matrix",
			Year:  "1999",
			Files: []string{"The Matrix (1999)/The Matrix.mkv"},
		}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Query:      MovieSearchQuery{Title: "Dune", Year: "2021"},
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
			Confidence: 1.0,
		}, {
			Key:      "title_year:matrix|1999",
			Query:    MovieSearchQuery{Title: "The Matrix", Year: "1999"},
			Accepted: false,
			Reason:   "ambiguous_or_low_confidence",
			Title:    "The Matrix",
			Year:     "1999",
		}},
	}

	analysisResult := result
	analysisResult.MovieSearch = nil
	analysisRefs, err := PersistScannerAnalysisEntities(ctx, pool, lib, Options{ScopePaths: []string{"/media/movies"}}, analysisResult)
	require.NoError(t, err)
	require.Len(t, analysisRefs, 2)
	for _, ref := range analysisRefs {
		require.Equal(t, "discovered", ref.Entity.Status)
		require.False(t, ref.Entity.SearchArtifactID.Valid, "analysis is not a completed search artifact")
		require.Equal(t, scanArtifactKindAnalyze, ref.Artifact.Stage)
		_, loaded, loadErr := LoadScannerEntityArtifactResult(ctx, pool, ref.Artifact.ID)
		require.NoError(t, loadErr)
		require.Len(t, loaded.MovieMatches, 1, "analysis hand-off is narrow per entity")
		require.Empty(t, loaded.MovieSearch)
	}

	refs := persistTestScannerSearchEntities(t, ctx, pool, lib, Options{ScopePaths: []string{"/media/movies"}}, analysisRefs, result, 0)
	require.Len(t, refs, 2)

	var duneArtifactID int64
	for _, ref := range refs {
		switch ref.IdentityKey {
		case "title_year:dune|2021":
			duneArtifactID = ref.Artifact.ID
			require.True(t, ref.Accepted)
			require.Equal(t, "matched", ref.Entity.Status)
		case "title_year:matrix|1999":
			require.False(t, ref.Accepted)
			require.Equal(t, "needs_review", ref.Entity.Status)
		}
	}
	require.NotZero(t, duneArtifactID)

	_, loaded, err := LoadScannerEntityArtifactResult(ctx, pool, duneArtifactID)
	require.NoError(t, err)
	require.Len(t, loaded.MovieMatches, 1)
	require.Equal(t, "title_year:dune|2021", loaded.MovieMatches[0].Key)
	require.Len(t, loaded.MovieSearch, 1)
	require.Equal(t, "heya:movie:tmdb:438631", loaded.MovieSearch[0].ProviderID)
	require.Len(t, loaded.Inventory.Roots, 1)
	require.Len(t, loaded.Inventory.Roots[0].Files, 1)
	require.Equal(t, "Dune (2021)/Dune.mkv", loaded.Inventory.Roots[0].Files[0].RelPath)
}

func TestMusicReviewRematchReusesRetainedAnalysisArtifact(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "music-review-rematch-test", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/media/music"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	const key = "artist:uncertain"
	result := Result{Inventory: Inventory{Roots: []InventoryRoot{{Root: "/media/music", Files: []InventoryFile{{
		Root: "/media/music", Path: "/media/music/Uncertain/01 - Example.flac", RelPath: "Uncertain/01 - Example.flac", Class: ClassPrimaryMedia,
	}}}}}, MusicArtists: []MusicArtistPlan{{
		Key: key, Artist: "Uncertain", Files: []string{"Uncertain/01 - Example.flac"},
		Albums: []MusicAlbumPlan{{Album: "Example", Tracks: []MusicTrackPlan{{
			Key: "track:example", Artist: "Uncertain", Album: "Example", TrackTitle: "Example", RelPath: "Uncertain/01 - Example.flac",
		}}}},
	}}}
	scope := []string{"/media/music/Uncertain"}
	analysisRefs, err := PersistScannerAnalysisEntities(ctx, pool, lib, Options{ScopePaths: scope}, result)
	require.NoError(t, err)
	require.Len(t, analysisRefs, 1)

	result.MusicSearch = []MusicSearchMatch{{Key: key, Query: MusicSearchQuery{Artist: "Uncertain"}, Reason: "ambiguous_or_low_confidence"}}
	searchRefs := persistTestScannerSearchEntities(t, ctx, pool, lib, Options{ScopePaths: scope}, analysisRefs, result, 0)
	require.Len(t, searchRefs, 1)
	require.Equal(t, "needs_review", searchRefs[0].Entity.Status)

	rows, err := q.ListScannerReviewsForRematch(ctx, sqlc.ListScannerReviewsForRematchParams{
		LibraryID: lib.ID,
		MediaType: sqlc.MediaTypeMusic,
		RowLimit:  10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, searchRefs[0].Entity.ID, rows[0].ScannerEntityID)
	require.Equal(t, analysisRefs[0].Artifact.ID, rows[0].AnalysisArtifactID)
	require.Equal(t, scope, rows[0].ScopePaths)

	_, err = pool.Exec(ctx, `
		INSERT INTO scanner_metadata_continuations (
			kind, library_id, scanner_entity_id, artifact_id, args, next_attempt_at
		) VALUES ('search_metadata', $1, $2, $3, '{}'::jsonb, now() + interval '1 hour')
	`, lib.ID, searchRefs[0].Entity.ID, analysisRefs[0].Artifact.ID)
	require.NoError(t, err)
	rows, err = q.ListScannerReviewsForRematch(ctx, sqlc.ListScannerReviewsForRematchParams{
		LibraryID: lib.ID,
		MediaType: sqlc.MediaTypeMusic,
		RowLimit:  10,
	})
	require.NoError(t, err)
	require.Empty(t, rows, "parked upstream work must not be submitted twice")
}

func TestScannerEntityArtifactsCarryGenerationAndSourceLineage(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-artifact-cleanup-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	scopePaths := []string{"/media/movies/Dune (2021)"}

	searchRun := createFinishedTestScanRun(t, ctx, q, lib, "search")

	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    "/media/movies/Dune (2021)/Dune.mkv",
				RelPath: "Dune (2021)/Dune.mkv",
				Name:    "Dune.mkv",
				Class:   ClassPrimaryMedia,
			}},
		}}},
		MovieMatches: []MovieMatch{{
			Key:   "title_year:dune|2021",
			Title: "Dune",
			Year:  "2021",
			Files: []string{"Dune (2021)/Dune.mkv"},
		}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Query:      MovieSearchQuery{Title: "Dune", Year: "2021"},
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
			Confidence: 1.0,
		}},
	}
	analysisResult := result
	analysisResult.MovieSearch = nil
	analysisRefs, err := PersistScannerAnalysisEntities(ctx, pool, lib, Options{ScopePaths: scopePaths}, analysisResult)
	require.NoError(t, err)
	require.Len(t, analysisRefs, 1)
	refs := persistTestScannerSearchEntities(t, ctx, pool, lib, Options{ScopePaths: scopePaths}, analysisRefs, result, searchRun.ID)
	require.Len(t, refs, 1)
	entityID := refs[0].Entity.ID
	searchArtifactID := refs[0].Artifact.ID

	fetchRun := createFinishedTestScanRun(t, ctx, q, lib, "fetch")
	result.MovieMetadata = []MovieFetchPreview{{
		Key:        "title_year:dune|2021",
		ProviderID: "heya:movie:tmdb:438631",
		Title:      "Dune",
		Year:       "2021",
		Detail:     &metadata.MediaDetail{Title: "Dune", Year: "2021"},
	}}
	current, err := BeginScannerEntityFetch(ctx, pool, entityID, searchArtifactID)
	require.NoError(t, err)
	require.True(t, current)
	fetchArtifact, current, err := PersistScannerFetchEntity(ctx, pool, entityID, searchArtifactID, result, fetchRun.ID)
	require.NoError(t, err)
	require.True(t, current)

	applyRun := createFinishedTestScanRun(t, ctx, q, lib, "apply")
	result.MovieApply = []MovieApplyResult{{
		Key:         "title_year:dune|2021",
		Action:      "create",
		Title:       "Dune",
		Year:        "2021",
		ProviderID:  "heya:movie:tmdb:438631",
		MediaItemID: 123,
	}}
	current, err = BeginScannerEntityApply(ctx, pool, entityID, fetchArtifact.ID)
	require.NoError(t, err)
	require.True(t, current)
	applyArtifact, current, err := PersistScannerApplyEntity(ctx, pool, entityID, fetchArtifact.ID, result, applyRun.ID)
	require.NoError(t, err)
	require.True(t, current)

	entity, err := q.GetScannerEntity(ctx, entityID)
	require.NoError(t, err)
	require.Equal(t, "applied", entity.Status)
	require.True(t, entity.SearchArtifactID.Valid)
	require.True(t, entity.MetadataArtifactID.Valid)
	require.True(t, entity.ApplyArtifactID.Valid)

	require.Equal(t, entity.PipelineGeneration, analysisRefs[0].Artifact.PipelineGeneration)
	require.Equal(t, entity.PipelineGeneration, refs[0].Artifact.PipelineGeneration)
	require.Equal(t, entity.PipelineGeneration, fetchArtifact.PipelineGeneration)
	require.Equal(t, entity.PipelineGeneration, applyArtifact.PipelineGeneration)
	require.False(t, analysisRefs[0].Artifact.SourceArtifactID.Valid)
	require.Equal(t, analysisRefs[0].Artifact.ID, refs[0].Artifact.SourceArtifactID.Int64)
	require.Equal(t, refs[0].Artifact.ID, fetchArtifact.SourceArtifactID.Int64)
	require.Equal(t, fetchArtifact.ID, applyArtifact.SourceArtifactID.Int64)
}

func TestPersistScannerSearchEntitiesHonorsDottedSceneDirectoryScope(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-entity-scoped-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	sceneScope := "/media/movies/Anora.2024.1080p.BluRay.x264-PiGNUS"
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    sceneScope + "/Anora.2024.1080p.BluRay.x264-PiGNUS.mkv",
				RelPath: "Anora.2024.1080p.BluRay.x264-PiGNUS/Anora.2024.1080p.BluRay.x264-PiGNUS.mkv",
				Name:    "Anora.2024.1080p.BluRay.x264-PiGNUS.mkv",
				Class:   ClassPrimaryMedia,
			}, {
				Root:    "/media/movies",
				Path:    "/media/movies/Kill Bill Vol. 1 (2003)/Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv",
				RelPath: "Kill Bill Vol. 1 (2003)/Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv",
				Name:    "Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv",
				Class:   ClassPrimaryMedia,
			}},
		}}},
		MovieMatches: []MovieMatch{{
			Key:   "title_year:anora|2024",
			Title: "Anora",
			Year:  "2024",
			Files: []string{"Anora.2024.1080p.BluRay.x264-PiGNUS/Anora.2024.1080p.BluRay.x264-PiGNUS.mkv"},
		}, {
			Key:   "title_year:kill bill vol 1|2003",
			Title: "Kill Bill Vol 1",
			Year:  "2003",
			Files: []string{"Kill Bill Vol. 1 (2003)/Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv"},
		}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:anora|2024",
			Query:      MovieSearchQuery{Title: "Anora", Year: "2024"},
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:1064213",
			Title:      "Anora",
			Year:       "2024",
			Confidence: 1.0,
		}, {
			Key:        "title_year:kill bill vol 1|2003",
			Query:      MovieSearchQuery{Title: "Kill Bill Vol 1", Year: "2003"},
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:24",
			Title:      "Kill Bill: Vol. 1",
			Year:       "2003",
			Confidence: 0.95,
		}},
	}

	analysisResult := result
	analysisResult.MovieSearch = nil
	analysisRefs, err := PersistScannerAnalysisEntities(ctx, pool, lib, Options{ScopePaths: []string{sceneScope}}, analysisResult)
	require.NoError(t, err)
	refs := persistTestScannerSearchEntities(t, ctx, pool, lib, Options{ScopePaths: []string{sceneScope}}, analysisRefs, result, 0)
	require.Len(t, refs, 1)
	require.Equal(t, "title_year:anora|2024", refs[0].IdentityKey)

	_, loaded, err := LoadScannerEntityArtifactResult(ctx, pool, refs[0].Artifact.ID)
	require.NoError(t, err)
	require.Len(t, loaded.MovieMatches, 1)
	require.Equal(t, "title_year:anora|2024", loaded.MovieMatches[0].Key)
	require.Len(t, loaded.Inventory.Roots, 1)
	require.Len(t, loaded.Inventory.Roots[0].Files, 1)
	require.Equal(t, "Anora.2024.1080p.BluRay.x264-PiGNUS/Anora.2024.1080p.BluRay.x264-PiGNUS.mkv", loaded.Inventory.Roots[0].Files[0].RelPath)
}

func persistTestScannerSearchEntities(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lib sqlc.Library, opts Options, analysisRefs []ScannerEntityRef, result Result, scanRunID int64) []ScannerEntityRef {
	t.Helper()
	refs := make([]ScannerEntityRef, 0, len(analysisRefs))
	for _, analysisRef := range analysisRefs {
		ref, current, err := PersistScannerSearchEntity(ctx, pool, lib, opts, analysisRef.Entity.ID, analysisRef.Artifact.ID, result, scanRunID)
		require.NoError(t, err)
		require.True(t, current)
		refs = append(refs, ref)
	}
	return refs
}

func createFinishedTestScanRun(t *testing.T, ctx context.Context, q *sqlc.Queries, lib sqlc.Library, mode string) sqlc.ScanRun {
	t.Helper()
	run, err := q.CreateScanRun(ctx, sqlc.CreateScanRunParams{
		LibraryID:      lib.ID,
		MediaType:      lib.MediaType,
		ScannerVersion: "scanner-test",
		Mode:           mode,
		Status:         "running",
		Summary:        []byte("{}"),
	})
	require.NoError(t, err)
	err = q.FinishScanRun(ctx, sqlc.FinishScanRunParams{
		ID:           run.ID,
		Status:       "complete",
		Summary:      []byte("{}"),
		ErrorMessage: "",
	})
	require.NoError(t, err)
	return run
}

func musicCandidates(prefix string, artist string, n int) []MusicSearchCandidate {
	out := make([]MusicSearchCandidate, 0, n)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("%s-%02d", prefix, i)
		out = append(out, MusicSearchCandidate{
			ProviderID:  "heya:artist:mbid:" + id,
			Provider:    "heya",
			Artist:      artist,
			Confidence:  1 - float64(i-1)*0.01,
			ExternalIDs: map[string]string{"mbid": id},
		})
	}
	return out
}

// The production API only serves the review dataset in pages / per identity;
// these helpers rebuild the whole-library views the assertions below need.
func listScannerIdentitiesForTest(t *testing.T, ctx context.Context, q *sqlc.Queries, libraryID int64) []sqlc.ListScannerIdentitiesPageByLibraryRow {
	t.Helper()
	rows, err := q.ListScannerIdentitiesPageByLibrary(ctx, sqlc.ListScannerIdentitiesPageByLibraryParams{
		LibraryID: libraryID,
		PageLimit: 1000,
	})
	require.NoError(t, err)
	return rows
}

func listScannerCandidatesForTest(t *testing.T, ctx context.Context, q *sqlc.Queries, libraryID, identityID int64) []sqlc.ListScannerCandidatesByIdentityRow {
	t.Helper()
	rows, err := q.ListScannerCandidatesByIdentity(ctx, sqlc.ListScannerCandidatesByIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	require.NoError(t, err)
	return rows
}

func scannerIdentitiesByKey(rows []sqlc.ListScannerIdentitiesPageByLibraryRow) map[string]sqlc.ListScannerIdentitiesPageByLibraryRow {
	out := make(map[string]sqlc.ListScannerIdentitiesPageByLibraryRow, len(rows))
	for _, row := range rows {
		out[row.IdentityKey] = row
	}
	return out
}

// scannerOpenFindingCounts tallies every open finding for the library by code:
// identity-attached findings via the per-identity query plus orphan findings
// via the issues page.
func scannerOpenFindingCounts(t *testing.T, ctx context.Context, q *sqlc.Queries, libraryID int64) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, identity := range listScannerIdentitiesForTest(t, ctx, q, libraryID) {
		rows, err := q.ListScannerFindingsByIdentity(ctx, sqlc.ListScannerFindingsByIdentityParams{
			LibraryID:  libraryID,
			IdentityID: pgtype.Int8{Int64: identity.ID, Valid: true},
			PageLimit:  1000,
		})
		require.NoError(t, err)
		for _, row := range rows {
			out[row.Code]++
		}
	}
	orphans, err := q.ListScannerIssuesPageByLibrary(ctx, sqlc.ListScannerIssuesPageByLibraryParams{
		LibraryID: libraryID,
		PageLimit: 1000,
	})
	require.NoError(t, err)
	for _, row := range orphans {
		out[row.Code]++
	}
	return out
}
