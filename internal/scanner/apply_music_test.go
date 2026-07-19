package scanner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestApplyMusicArtistAdoptsExistingNameDisambiguation(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-music-artist-adopt-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music-artist-adopt"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	name := fmt.Sprintf("Scanner Duplicate Artist %d", time.Now().UnixNano())
	disambig := "scanner concurrency regression"
	canonicalItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    sqlc.MediaTypeMusic,
		Title:        name,
		SortTitle:    name,
		ProviderKind: "heya",
	})
	require.NoError(t, err)
	canonicalArtist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:    canonicalItem.ID,
		MusicbrainzID:  "scanner-canonical-mbid",
		Name:           name,
		SortName:       name,
		Disambiguation: disambig,
	})
	require.NoError(t, err)

	duplicateItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    sqlc.MediaTypeMusic,
		Title:        name,
		SortTitle:    name,
		ProviderKind: "heya",
	})
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	qtx := sqlc.New(tx)
	artist, artistAction, err := applyMusicArtist(ctx, qtx, duplicateItem.ID, MusicMaterializePreview{Artist: name}, &metadata.MediaDetail{
		ArtistName:           name,
		ArtistSortName:       name,
		ArtistDisambiguation: disambig,
		ExternalIDs:          map[string]string{"mbid": "scanner-canonical-mbid"},
		ProviderKind:         "heya",
	})
	require.NoError(t, err)
	require.Equal(t, canonicalArtist.ID, artist.ID)
	require.Equal(t, canonicalItem.ID, artist.MediaItemID)
	require.Equal(t, "adopt_artist_row", artistAction)

	item, mediaAction, err := applyMusicCanonicalArtistMediaItem(ctx, qtx, duplicateItem, "create_media_item", artist, &metadata.MediaDetail{
		ArtistName:           name,
		ArtistSortName:       name,
		ArtistDisambiguation: disambig,
		ExternalIDs:          map[string]string{"mbid": "scanner-canonical-mbid"},
		ProviderKind:         "heya",
	})
	require.NoError(t, err)
	require.Equal(t, canonicalItem.ID, item.ID)
	require.Equal(t, "adopt_media_item", mediaAction)
	require.NoError(t, tx.Commit(ctx))

	_, err = q.GetMediaItemByID(ctx, duplicateItem.ID)
	require.True(t, errors.Is(err, pgx.ErrNoRows), "duplicate media item should be removed after adopting canonical artist")
}

func TestApplyMusicRunsCommitGuardBeforeWritesAndCommit(t *testing.T) {
	pool := testutil.SetupDB(t)
	sentinel := errors.New("scanner source changed")
	calls := 0
	ctx := WithScannerApplyCommitGuard(context.Background(), func(context.Context) error {
		calls++
		if calls == 2 {
			return sentinel
		}
		return nil
	})

	_, err := ApplyMusicMaterialization(ctx, sqlc.Library{MediaType: sqlc.MediaTypeMusic}, Result{
		MusicMaterialize: []MusicMaterializePreview{{Key: "artist:test", Action: "blocked"}},
	}, pool, &captureEmitter{})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 2, calls)
}

func TestApplyMusicCreatesNamesakeWhenStalePreviewSharesOnlyWeakerProviderID(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: fmt.Sprintf("scanner-music-namesake-test-%d", time.Now().UnixNano()), MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/tmp/music-namesake"}, ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	poisonedIDs := map[string]string{"mbid": "10000000-0000-4000-8000-000000000001", "apple": "shared-apple-id"}
	poisoned, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Binary", SortTitle: "Binary",
		ExternalIds: mustJSONBytes(poisonedIDs), ProviderKind: "heya",
	})
	require.NoError(t, err)
	_, err = q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: poisoned.ID, MusicbrainzID: poisonedIDs["mbid"], Name: "Binary", SortName: "Binary",
	})
	require.NoError(t, err)

	targetIDs := map[string]string{"mbid": "20000000-0000-4000-8000-000000000002", "apple": "shared-apple-id"}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(tx)
	store := NewSQLMusicMaterializeStore(tx)
	matched, found, err := store.FindMediaItemByExternalIDs(ctx, lib.ID, targetIDs)
	require.NoError(t, err)
	require.False(t, found, "shared weaker provider ID must not override a contradictory MBID")
	require.Zero(t, matched.ID)

	created, action, err := applyMusicMediaItem(ctx, qtx, store, lib.ID,
		MusicMaterializePreview{MediaItemID: poisoned.ID, Artist: "Binary"},
		&metadata.MediaDetail{ArtistName: "Binary", Title: "Binary", ExternalIDs: targetIDs, ProviderKind: "heya"},
	)
	require.NoError(t, err)
	require.Equal(t, "create_media_item", action)
	require.NotEqual(t, poisoned.ID, created.ID)
	require.JSONEq(t, string(mustJSONBytes(targetIDs)), string(created.ExternalIds))

	unchanged, err := qtx.GetMediaItemByID(ctx, poisoned.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(mustJSONBytes(poisonedIDs)), string(unchanged.ExternalIds))
}

func TestApplyMusicAlbumPrefersExistingTupleOverSiblingMBID(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         fmt.Sprintf("scanner-music-album-identity-test-%d", time.Now().UnixNano()),
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music-album-identity"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Scanner Wilkinson", SortTitle: "Scanner Wilkinson",
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Scanner Wilkinson"})
	require.NoError(t, err)

	parent, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Afterglow", Year: "2013", MusicbrainzID: "scanner-parent-release-group",
		Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	edition, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Afterglow (remixes)", Year: "2013", MusicbrainzID: "scanner-edition-release-group",
		Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)

	// Reproduce production evidence: the remixes folder carries the parent
	// release-group MBID even though its exact local tuple already has a row.
	mapping := MusicAlbumFetchMatch{
		LocalAlbum: "Afterglow (Remixes)", LocalYear: "2013",
		LocalExternalIDs: map[string]string{"musicbrainz_release_group": parent.MusicbrainzID},
	}
	got, action, err := applyMusicAlbum(ctx, q, artist.ID, mapping, musicAlbumEntryForApply(nil, mapping))
	require.NoError(t, err)
	require.Equal(t, "update", action)
	require.Equal(t, edition.ID, got.ID, "exact title/year owner must win over a sibling MBID")
	require.Equal(t, edition.MusicbrainzID, got.MusicbrainzID, "conflicting sibling MBID must not move onto the edition")

	unchangedParent, err := q.GetAlbumByID(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, "Afterglow", unchangedParent.Title)
	require.Equal(t, parent.MusicbrainzID, unchangedParent.MusicbrainzID)
}

func TestApplyMusicFingerprintRecordingEvidenceFillsUnlistedTrack(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:      fmt.Sprintf("scanner-music-fingerprint-recording-test-%d", time.Now().UnixNano()),
		MediaType: sqlc.MediaTypeMusic, Paths: []string{"/tmp/music-fingerprint-recording"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Ado", SortTitle: "Ado"})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Ado"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{ArtistID: artist.ID, Title: "Kyougen", Year: "2022", Genres: []string{}, Tags: []string{}})
	require.NoError(t, err)
	track, err := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "Readymade", Duration: 244})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(context.Background(), "DELETE FROM metadata_entity_bindings WHERE local_kind = 'track' AND local_id = $1", track.ID)
		require.NoError(t, cleanupErr)
	})

	externalIDs := mustJSONBytes(map[string]string{"local": "readymade"})
	artistCredits := mustJSONBytes([]metadata.ArtistCreditEntry{{Name: "Ado"}})
	require.NoError(t, q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
		ID: track.ID, ExternalIds: externalIDs, Column3: "JPXXX2200001", Column5: "https://example.test/preview",
		Explicit: true, ArtistCredits: artistCredits, LyricsAvailable: true,
	}))

	const relPath = "Ado/Kyougen/01 - Readymade.flac"
	evidenceByRel := musicRecordingEvidenceByRelPath([]MusicAcceptedRecordingEvidence{{
		RelPath:              relPath,
		RecordingMBID:        "10000000-0000-4000-8000-000000000001",
		CanonicalRecordingID: "20000000-0000-4000-8000-000000000001",
		Confidence:           .98, SourceDuration: 244, RecordingDuration: 243,
	}})
	evidence, ok := evidenceByRel[relPath]
	require.True(t, ok)
	_, caseFolded := evidenceByRel["Ado/Kyougen/01 - READYMADE.flac"]
	require.False(t, caseFolded, "recording evidence lookup must be relpath-exact")
	require.NoError(t, applyMusicFingerprintRecordingEvidence(ctx, q, track.ID, metadata.TrackDetail{}, evidence))

	updated, err := q.GetTrackByID(ctx, track.ID)
	require.NoError(t, err)
	require.Equal(t, evidence.RecordingMBID, updated.RecordingMbid)
	require.JSONEq(t, string(externalIDs), string(updated.ExternalIds))
	require.Equal(t, "JPXXX2200001", updated.Isrc)
	require.Equal(t, "https://example.test/preview", updated.PreviewUrl)
	require.True(t, updated.Explicit)
	require.True(t, updated.LyricsAvailable)
	require.JSONEq(t, string(artistCredits), string(updated.ArtistCredits))

	binding, err := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: track.ID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(evidence.CanonicalRecordingID), binding.EntityID)
	require.Equal(t, "recording", binding.EntityKind)
}

func TestPruneReassignedMusicTrackRemovesPoisonedReleaseAndPreservesRatings(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:      fmt.Sprintf("scanner-music-reassign-prune-test-%d", time.Now().UnixNano()),
		MediaType: sqlc.MediaTypeMusic, Paths: []string{"/tmp/music-reassign-prune"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	makeTrack := func(name, disambiguation, albumTitle string) (sqlc.Album, sqlc.Track) {
		item, createErr := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: name, SortTitle: name})
		require.NoError(t, createErr)
		artist, createErr := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: name, Disambiguation: disambiguation})
		require.NoError(t, createErr)
		album, createErr := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{ArtistID: artist.ID, Title: albumTitle, Genres: []string{}, Tags: []string{}})
		require.NoError(t, createErr)
		track, createErr := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "Track"})
		require.NoError(t, createErr)
		return album, track
	}
	sourceAlbum, sourceTrack := makeTrack("LiSA", "Japanese pop/rock singer", "Poisoned LISA Release")
	targetAlbum, targetTrack := makeTrack("LISA", "BLACKPINK", "Correct LISA Release")
	_, err = pool.Exec(ctx, `INSERT INTO user_track_ratings(user_id,track_id,rating) VALUES($1,$2,8)`, userID, sourceTrack.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO user_album_ratings(user_id,album_id,rating) VALUES($1,$2,9)`, userID, sourceAlbum.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO user_favorites(user_id,entity_type,entity_id) VALUES($1,'album',$2)`, userID, sourceAlbum.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO user_favorites(user_id,entity_type,entity_id) VALUES($1,'track',$2)`, userID, sourceTrack.ID)
	require.NoError(t, err)
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "track", LocalID: sourceTrack.ID, EntityID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), EntityKind: "recording", SchemaVersion: 1,
	})
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, pruneReassignedMusicTrack(ctx, sqlc.New(tx), sourceTrack.ID, targetTrack.ID))
	require.NoError(t, tx.Commit(ctx))

	_, err = q.GetTrackByID(ctx, sourceTrack.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetAlbumByID(ctx, sourceAlbum.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var trackRating, albumRating int16
	require.NoError(t, pool.QueryRow(ctx, `SELECT rating FROM user_track_ratings WHERE user_id=$1 AND track_id=$2`, userID, targetTrack.ID).Scan(&trackRating))
	require.NoError(t, pool.QueryRow(ctx, `SELECT rating FROM user_album_ratings WHERE user_id=$1 AND album_id=$2`, userID, targetAlbum.ID).Scan(&albumRating))
	require.Equal(t, int16(8), trackRating)
	require.Equal(t, int16(9), albumRating)
	var favorite bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_favorites WHERE user_id=$1 AND entity_type='album' AND entity_id=$2)`, userID, targetAlbum.ID).Scan(&favorite))
	require.True(t, favorite)
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_favorites WHERE user_id=$1 AND entity_type='track' AND entity_id=$2)`, userID, targetTrack.ID).Scan(&favorite))
	require.True(t, favorite)
	_, err = q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: sourceTrack.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestApplyMusicFingerprintRecordingEvidencePreservesHardIdentityConflicts(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:      fmt.Sprintf("scanner-music-fingerprint-conflict-test-%d", time.Now().UnixNano()),
		MediaType: sqlc.MediaTypeMusic, Paths: []string{"/tmp/music-fingerprint-conflict"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Conflict Artist", SortTitle: "Conflict Artist"})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Conflict Artist"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{ArtistID: artist.ID, Title: "Conflict Album", Genres: []string{}, Tags: []string{}})
	require.NoError(t, err)
	mbidTrack, err := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "MBID Conflict"})
	require.NoError(t, err)
	bindingTrack, err := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 2, Title: "Binding Conflict"})
	require.NoError(t, err)
	remoteTrack, err := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 3, Title: "Remote Conflict"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(context.Background(), "DELETE FROM metadata_entity_bindings WHERE local_kind = 'track' AND local_id = ANY($1)", []int64{mbidTrack.ID, bindingTrack.ID, remoteTrack.ID})
		require.NoError(t, cleanupErr)
	})

	const existingMBID = "30000000-0000-4000-8000-000000000001"
	require.NoError(t, q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
		ID: mbidTrack.ID, ExternalIds: []byte("{}"), Column4: existingMBID, ArtistCredits: []byte("[]"),
	}))
	const existingCanonical = "40000000-0000-4000-8000-000000000001"
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "track", LocalID: bindingTrack.ID, EntityID: uuid.MustParse(existingCanonical), EntityKind: "recording", SchemaVersion: 1,
	})
	require.NoError(t, err)

	evidence := MusicAcceptedRecordingEvidence{
		RelPath:              "Conflict/track.flac",
		RecordingMBID:        "50000000-0000-4000-8000-000000000001",
		CanonicalRecordingID: "60000000-0000-4000-8000-000000000001",
		Confidence:           .99, SourceDuration: 180, RecordingDuration: 180,
	}
	require.NoError(t, applyMusicFingerprintRecordingEvidence(ctx, q, mbidTrack.ID, metadata.TrackDetail{}, evidence))
	require.NoError(t, applyMusicFingerprintRecordingEvidence(ctx, q, bindingTrack.ID, metadata.TrackDetail{}, evidence))
	require.NoError(t, applyMusicFingerprintRecordingEvidence(ctx, q, remoteTrack.ID, metadata.TrackDetail{
		CanonicalID: "70000000-0000-4000-8000-000000000001",
	}, evidence))

	unchangedMBIDTrack, err := q.GetTrackByID(ctx, mbidTrack.ID)
	require.NoError(t, err)
	require.Equal(t, existingMBID, unchangedMBIDTrack.RecordingMbid)
	_, err = q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: mbidTrack.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	unchangedBindingTrack, err := q.GetTrackByID(ctx, bindingTrack.ID)
	require.NoError(t, err)
	require.Empty(t, unchangedBindingTrack.RecordingMbid)
	binding, err := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: bindingTrack.ID})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(existingCanonical), binding.EntityID)

	unchangedRemoteTrack, err := q.GetTrackByID(ctx, remoteTrack.ID)
	require.NoError(t, err)
	require.Empty(t, unchangedRemoteTrack.RecordingMbid)
	_, err = q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: remoteTrack.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
