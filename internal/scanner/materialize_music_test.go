package scanner

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/require"
)

type emptyMusicMaterializeStore struct{}

func (emptyMusicMaterializeStore) FindMediaItemByExternalIDs(context.Context, int64, map[string]string) (sqlc.MediaItemCard, bool, error) {
	return sqlc.MediaItemCard{}, false, nil
}

func (emptyMusicMaterializeStore) FindMediaItemByIdentity(context.Context, int64, string) (sqlc.MediaItemCard, bool, error) {
	return sqlc.MediaItemCard{}, false, nil
}

func (emptyMusicMaterializeStore) GetMediaItemByID(context.Context, int64) (sqlc.MediaItemCard, bool, error) {
	return sqlc.MediaItemCard{}, false, nil
}

func (emptyMusicMaterializeStore) GetArtistByMediaItemID(context.Context, int64) (sqlc.Artist, bool, error) {
	return sqlc.Artist{}, false, nil
}

func (emptyMusicMaterializeStore) GetAlbumByMusicBrainzID(context.Context, string) (sqlc.Album, bool, error) {
	return sqlc.Album{}, false, nil
}

func (emptyMusicMaterializeStore) GetAlbumByArtistTitleYear(context.Context, int64, string, string) (sqlc.Album, bool, error) {
	return sqlc.Album{}, false, nil
}

func (emptyMusicMaterializeStore) GetTrackByAlbumDiscTrack(context.Context, int64, int32, int32) (sqlc.Track, bool, error) {
	return sqlc.Track{}, false, nil
}

func (emptyMusicMaterializeStore) GetLibraryFileByPath(context.Context, int64, string) (sqlc.LibraryFile, bool, error) {
	return sqlc.LibraryFile{}, false, nil
}

func (emptyMusicMaterializeStore) GetTrackFileByLibraryFileID(context.Context, int64) (sqlc.TrackFile, bool, error) {
	return sqlc.TrackFile{}, false, nil
}

type trackingMusicMaterializeStore struct {
	emptyMusicMaterializeStore
	identityCalls int
	identityItem  sqlc.MediaItemCard
}

func (s *trackingMusicMaterializeStore) FindMediaItemByIdentity(context.Context, int64, string) (sqlc.MediaItemCard, bool, error) {
	s.identityCalls++
	return s.identityItem, s.identityItem.ID != 0, nil
}

func TestMusicMaterializeDoesNotNameFallbackAfterStrongIDMiss(t *testing.T) {
	store := &trackingMusicMaterializeStore{identityItem: sqlc.MediaItemCard{ID: 42, MediaType: sqlc.MediaTypeMusic, Title: "LiSA"}}
	item, found, err := findMusicMaterializeMediaItem(context.Background(), store, 7,
		map[string]string{"mbid": "30aeb57f-bb16-47fa-86ca-79fc57b4d12c"}, "LISA")
	require.NoError(t, err)
	require.False(t, found)
	require.Zero(t, item.ID)
	require.Zero(t, store.identityCalls)
}

func TestMusicMaterializeAllowsNameFallbackWithoutStrongID(t *testing.T) {
	store := &trackingMusicMaterializeStore{identityItem: sqlc.MediaItemCard{ID: 42, MediaType: sqlc.MediaTypeMusic, Title: "Example"}}
	item, found, err := findMusicMaterializeMediaItem(context.Background(), store, 7,
		map[string]string{"lastfm": "Example"}, "Example")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(42), item.ID)
	require.Equal(t, 1, store.identityCalls)
}

func TestMusicMaterializeRepairsSameNameContradictoryArtistIDs(t *testing.T) {
	existing := sqlc.MediaItemCard{
		ID: 42, MediaType: sqlc.MediaTypeMusic, Title: "LiSA",
		ExternalIds: mustJSONBytes(map[string]string{"musicbrainz:artist": "85d76093-9865-4605-97fa-8c910929d366"}),
	}
	require.True(t, canRepairMusicFileAttachment(existing, "LISA", map[string]string{
		"mbid": "30aeb57f-bb16-47fa-86ca-79fc57b4d12c",
	}))
	require.False(t, canRepairMusicFileAttachment(existing, "LISA", map[string]string{
		"musicbrainz_artist": "85D76093-9865-4605-97FA-8C910929D366",
	}))
}

func TestMusicMaterializeRepairsContradictoryMBIDDespitePollutedProviderID(t *testing.T) {
	existing := sqlc.MediaItemCard{
		ID: 42, MediaType: sqlc.MediaTypeMusic, Title: "Binary",
		ExternalIds: mustJSONBytes(map[string]string{
			"mbid":  "88b010d7-af58-4498-8aac-025a466be90c",
			"apple": "160783513",
		}),
	}
	require.True(t, canRepairMusicFileAttachment(existing, "Binary", map[string]string{
		"mbid":  "402073dc-d562-4661-8b3e-974edfa76687",
		"apple": "160783513",
	}))
}

func TestPlanMusicMaterializationCarriesOnlyExactRecordingEvidence(t *testing.T) {
	const relPath = "Ado/Kyougen/01 - Readymade.flac"
	evidence := MusicAcceptedRecordingEvidence{
		RelPath:              relPath,
		RecordingMBID:        "10000000-0000-4000-8000-000000000001",
		CanonicalRecordingID: "20000000-0000-4000-8000-000000000001",
		Confidence:           .98, SourceDuration: 244, RecordingDuration: 243,
	}
	artist := MusicArtistPlan{
		Key: "artist:ado", Artist: "Ado",
		Albums: []MusicAlbumPlan{{
			Key: "album:kyougen", Artist: "Ado", Album: "Kyougen",
			Tracks: []MusicTrackPlan{{
				Key: "track:readymade", Artist: "Ado", Album: "Kyougen", TrackTitle: "Readymade",
				DiscNumber: 1, TrackNumber: 1, RelPath: relPath, Confidence: .99,
			}},
		}},
	}
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/music",
			Files: []InventoryFile{{
				Root: "/music", Path: "/music/" + relPath, RelPath: relPath, Class: ClassPrimaryMedia,
			}},
		}}},
		MusicTracks: []MusicTrackPlan{artist.Albums[0].Tracks[0]}, MusicArtists: []MusicArtistPlan{artist},
		MusicSearch: []MusicSearchMatch{{
			Key: artist.Key, Accepted: true, Artist: "Ado",
			ProviderID: "heyametadata:v2:entity:30000000-0000-4000-8000-000000000001",
			RecordingEvidence: []MusicAcceptedRecordingEvidence{
				evidence,
				{RelPath: "Ado/Kyougen/01 - READYMADE.flac", RecordingMBID: "40000000-0000-4000-8000-000000000001", CanonicalRecordingID: "50000000-0000-4000-8000-000000000001", Confidence: .99, SourceDuration: 244, RecordingDuration: 244},
			},
		}},
		MusicMetadata: []MusicFetchPreview{{
			Key: artist.Key, ProviderID: "heyametadata:v2:entity:30000000-0000-4000-8000-000000000001",
			Artist: "Ado", Detail: &metadata.MediaDetail{ArtistName: "Ado"},
		}},
	}

	previews, err := PlanMusicMaterialization(context.Background(), sqlc.Library{ID: 7, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/music"}}, result, emptyMusicMaterializeStore{}, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, previews, 1)
	require.Equal(t, []MusicAcceptedRecordingEvidence{evidence}, previews[0].RecordingEvidence)
	require.Equal(t, relPath, previews[0].AlbumMappings[0].TrackMappings[0].RelPath)
}

func TestMusicMaterializeRecordingEvidenceDropsConflictingDuplicatePath(t *testing.T) {
	const relPath = "Ado/Kyougen/01.flac"
	local := MusicArtistPlan{Albums: []MusicAlbumPlan{{Tracks: []MusicTrackPlan{{RelPath: relPath}}}}}
	values := []MusicAcceptedRecordingEvidence{
		{RelPath: relPath, RecordingMBID: "10000000-0000-4000-8000-000000000001", CanonicalRecordingID: "20000000-0000-4000-8000-000000000001", Confidence: .98, SourceDuration: 100, RecordingDuration: 100},
		{RelPath: relPath, RecordingMBID: "30000000-0000-4000-8000-000000000001", CanonicalRecordingID: "40000000-0000-4000-8000-000000000001", Confidence: .99, SourceDuration: 100, RecordingDuration: 100},
	}
	require.Empty(t, musicMaterializeRecordingEvidence(local, values))
}
