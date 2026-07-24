package scanner

import (
	"context"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
)

// The acoustic-validation gate: issue-triggered validation must be satisfied
// by a discovery-RESOLVED canonical entity (existing_entity /
// corroborated_identity) when no distinct-canonical namesake competes —
// otherwise artists without fingerprint coverage sat in needs_review forever
// with a perfect confidence-1.0 candidate (2026-07-24 prod audit: 482 of
// 1,671 stuck artists). Namesake collisions must keep demanding acoustics.
func TestMusicFingerprintValidationRequired(t *testing.T) {
	ambiguous := MusicArtistPlan{
		Artist: "Les Rallizes Dénudés",
		Issues: []string{"ambiguous_artist_identity_missing_album_artist_mbid"},
	}
	resolved := metadata.SearchResult{
		Title: "Les Rallizes Dénudés", ProviderID: "heya:entity:abc", HeyaSlug: "abc",
		Confidence: 1, Recommendation: "existing_entity",
	}
	unresolvedExact := metadata.SearchResult{
		Title: "Les Rallizes Dénudés", ProviderID: "heya:candidate:x",
		Confidence: 0.9, Recommendation: "ambiguous",
	}
	namesake := metadata.SearchResult{
		Title: "Les Rallizes Dénudés", ProviderID: "heya:candidate:other",
		Confidence: 0.88, Recommendation: "ambiguous",
	}

	cases := []struct {
		name       string
		artist     MusicArtistPlan
		candidates []metadata.SearchResult
		want       bool
	}{
		{"issue with resolved canonical is satisfied", ambiguous, []metadata.SearchResult{resolved}, false},
		{"issue with corroborated_identity is satisfied", ambiguous, []metadata.SearchResult{{
			Title: "Les Rallizes Dénudés", ProviderID: "heya:entity:abc", HeyaSlug: "abc",
			Confidence: 1, Recommendation: "corroborated_identity",
		}}, false},
		{"issue without resolution still validates", ambiguous, []metadata.SearchResult{unresolvedExact}, true},
		{"namesake collision overrides resolution", ambiguous, []metadata.SearchResult{resolved, namesake}, true},
		{"review-flagged resolution does not satisfy", ambiguous, []metadata.SearchResult{{
			Title: "Les Rallizes Dénudés", ProviderID: "heya:entity:abc", HeyaSlug: "abc",
			Confidence: 1, Recommendation: "existing_entity", RequiresReview: true,
		}}, true},
		{"different-name resolution does not satisfy", ambiguous, []metadata.SearchResult{{
			Title: "Someone Else", ProviderID: "heya:entity:abc", HeyaSlug: "abc",
			Confidence: 1, Recommendation: "existing_entity",
		}}, true},
		{"untrusted_track_artist_mbid behaves the same", MusicArtistPlan{
			Artist: "GPF", Issues: []string{"untrusted_track_artist_mbid"},
		}, []metadata.SearchResult{{
			Title: "GPF", ProviderID: "heya:entity:gpf", HeyaSlug: "gpf",
			Confidence: 1, Recommendation: "existing_entity",
		}}, false},
		{"no issues and no collision never validates", MusicArtistPlan{Artist: "GPF"}, []metadata.SearchResult{unresolvedExact}, false},
		{"local mbid always skips validation", MusicArtistPlan{
			Artist: "GPF", Issues: []string{"untrusted_track_artist_mbid"},
			ExternalIDs: map[string]string{"mbid": "1234"},
		}, []metadata.SearchResult{unresolvedExact}, false},
	}
	for _, tc := range cases {
		if got := musicFingerprintValidationRequired(tc.artist, tc.candidates); got != tc.want {
			t.Errorf("%s: musicFingerprintValidationRequired = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Compilation containers (Various Artists and friends) are trusted by
// nature: no findings draft under them, and they never force needs_review —
// the 2026-07 audit had 7.3k findings piled under one VA directory that no
// search could ever resolve.
func TestCompilationContainerFindingsAndReview(t *testing.T) {
	result := Result{
		MusicArtists: []MusicArtistPlan{{
			Key: musicArtistKey("Various Artists", "add compilations to this artist"), Artist: "Various Artists",
			ArtistDisambiguation: "add compilations to this artist",
			Issues:               []string{"conflicting_musicbrainz_artist_ids"},
		}},
		MusicTracks: []MusicTrackPlan{{
			Key: "track:va", Artist: "Various Artists", ArtistDisambiguation: "add compilations to this artist",
			RelPath: "Various Artists (add compilations to this artist)/Comp/01.mp3",
			Issues:  []string{"album_overridden_by_folder_consensus", "missing_track_number"},
		}},
	}

	for _, finding := range scanFindingDrafts(result, nil, musicArtistAutoMatchThreshold) {
		t.Errorf("compilation container drafted finding %s (%s)", finding.Code, finding.Message)
	}
	if status := scanIdentityReviewStatuses(result, musicArtistAutoMatchThreshold)[result.MusicArtists[0].Key]; status != "" {
		t.Errorf("compilation container review status = %q, want none (accepted default)", status)
	}
	if !musicCompilationContainerIdentity("Various Artists") || !musicCompilationContainerIdentity("VA") ||
		!musicCompilationContainerIdentity("Original Soundtrack") || musicCompilationContainerIdentity("Taylor Swift") {
		t.Error("musicCompilationContainerIdentity set membership wrong")
	}
}

// Folder-consensus telemetry: per-track and artist-level copies are
// suppressed, the album-level aggregate survives at info severity, and
// actionable issues keep drafting at warn everywhere.
func TestConsensusTelemetryFindingDiet(t *testing.T) {
	result := Result{
		MusicArtists: []MusicArtistPlan{{
			Key: musicArtistKey("Messy Artist", ""), Artist: "Messy Artist",
			Issues: []string{"album_overridden_by_folder_consensus", "untrusted_track_artist_mbid"},
		}},
		MusicAlbums: []MusicAlbumPlan{{
			Key: musicAlbumKey("Messy Artist", "Messy Album", "2020"), Artist: "Messy Artist", Album: "Messy Album",
			Issues: []string{"album_overridden_by_folder_consensus", "conflicting_musicbrainz_artist_ids"},
		}},
		MusicTracks: []MusicTrackPlan{{
			Key: "track:messy", Artist: "Messy Artist", Album: "Messy Album",
			RelPath: "Messy Artist/Messy Album/01.mp3",
			Issues:  []string{"album_overridden_by_folder_consensus", "missing_track_number"},
		}},
	}

	type draftKey struct{ code, message, severity string }
	var got []draftKey
	for _, finding := range scanFindingDrafts(result, nil, musicArtistAutoMatchThreshold) {
		got = append(got, draftKey{finding.Code, finding.Message, finding.Severity})
	}

	assertHas := func(code, messagePart, severity string) {
		for _, d := range got {
			if d.code == code && d.severity == severity && strings.Contains(d.message, messagePart) {
				return
			}
		}
		t.Errorf("missing draft %s/%s (%s) in %v", code, messagePart, severity, got)
	}
	assertNot := func(code, messagePart string) {
		for _, d := range got {
			if d.code == code && strings.Contains(d.message, messagePart) {
				t.Errorf("unexpected draft %s/%s in %v", code, messagePart, got)
			}
		}
	}

	assertHas("music_album_issue", "album_overridden_by_folder_consensus", string(SeverityInfo))
	assertHas("music_album_issue", "conflicting_musicbrainz_artist_ids", string(SeverityWarn))
	assertHas("music_track_issue", "missing_track_number", string(SeverityWarn))
	assertHas("local_identity_issue", "untrusted_track_artist_mbid", string(SeverityWarn))
	assertNot("music_track_issue", "album_overridden_by_folder_consensus")
	assertNot("local_identity_issue", "album_overridden_by_folder_consensus")
}

type fakeOverlapProvider struct {
	details map[string]*metadata.MediaDetail
}

func (f *fakeOverlapProvider) Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error) {
	return nil, nil
}

func (f *fakeOverlapProvider) GetDetail(_ context.Context, providerID string, _ *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	if detail, ok := f.details[providerID]; ok {
		return detail, nil
	}
	return &metadata.MediaDetail{}, nil
}

// Release-overlap corroboration: the on-disk discography picks between
// same-named candidates — a single candidate matching ≥2 local albums while
// competitors match none is settled identity; shared or single-album overlap
// stays in review.
func TestResolveMusicArtistByReleaseOverlap(t *testing.T) {
	artist := MusicArtistPlan{
		Key: "artist:orbit", Artist: "Orbit",
		Albums: []MusicAlbumPlan{
			{Artist: "Orbit", Album: "Lunar Cycles", Year: "2019"},
			{Artist: "Orbit", Album: "Perigee", Year: "2021"},
			{Artist: "Orbit", Album: "Apogee", Year: "2023"},
		},
	}
	candidateA := metadata.SearchResult{ProviderID: "cand:a", Title: "Orbit", Confidence: 0.8, Recommendation: "ambiguous"}
	candidateB := metadata.SearchResult{ProviderID: "cand:b", Title: "Orbit", Confidence: 0.79, Recommendation: "ambiguous"}
	matchingDetail := &metadata.MediaDetail{
		CanonicalID: "11111111-1111-4111-8111-111111111111", ArtistName: "Orbit",
		ExternalIDs: map[string]string{"mbid": "abc"},
		Albums: []metadata.AlbumEntry{
			{Title: "Lunar Cycles", Year: 2019}, {Title: "Perigee", Year: 2021}, {Title: "Elsewhere", Year: 2010},
		},
	}
	strangerDetail := &metadata.MediaDetail{
		CanonicalID: "22222222-2222-4222-8222-222222222222", ArtistName: "Orbit",
		Albums: []metadata.AlbumEntry{{Title: "Completely Different", Year: 1999}},
	}

	t.Run("unique winner corroborates", func(t *testing.T) {
		provider := &fakeOverlapProvider{details: map[string]*metadata.MediaDetail{
			"cand:a": matchingDetail, "cand:b": strangerDetail,
		}}
		result, ok, err := resolveMusicArtistByReleaseOverlap(context.Background(), artist, artist,
			[]metadata.SearchResult{candidateA, candidateB}, provider, &captureEmitter{})
		if err != nil || !ok {
			t.Fatalf("overlap resolution = ok %v err %v", ok, err)
		}
		if result.Recommendation != "release_corroborated" || result.RequiresReview || result.Confidence < 0.95 {
			t.Fatalf("winner not corroborated: %#v", result)
		}
		if result.HeyaSlug != matchingDetail.CanonicalID {
			t.Fatalf("winner slug = %q", result.HeyaSlug)
		}
		if !musicSearchDiscoveryResolvedCanonical(artist, []metadata.SearchResult{result}) {
			t.Fatal("release-corroborated winner must satisfy the validation gate")
		}
	})

	t.Run("shared overlap stays ambiguous", func(t *testing.T) {
		provider := &fakeOverlapProvider{details: map[string]*metadata.MediaDetail{
			"cand:a": matchingDetail, "cand:b": matchingDetail,
		}}
		if _, ok, _ := resolveMusicArtistByReleaseOverlap(context.Background(), artist, artist,
			[]metadata.SearchResult{candidateA, candidateB}, provider, &captureEmitter{}); ok {
			t.Fatal("two overlapping candidates must not corroborate")
		}
	})

	t.Run("single-album overlap is insufficient", func(t *testing.T) {
		provider := &fakeOverlapProvider{details: map[string]*metadata.MediaDetail{
			"cand:a": {CanonicalID: "3", Albums: []metadata.AlbumEntry{{Title: "Perigee", Year: 2021}}},
			"cand:b": strangerDetail,
		}}
		if _, ok, _ := resolveMusicArtistByReleaseOverlap(context.Background(), artist, artist,
			[]metadata.SearchResult{candidateA, candidateB}, provider, &captureEmitter{}); ok {
			t.Fatal("one matching album must not corroborate")
		}
	})

	t.Run("small local discography bails", func(t *testing.T) {
		oneAlbum := artist
		oneAlbum.Albums = artist.Albums[:1]
		provider := &fakeOverlapProvider{details: map[string]*metadata.MediaDetail{"cand:a": matchingDetail}}
		if _, ok, _ := resolveMusicArtistByReleaseOverlap(context.Background(), oneAlbum, oneAlbum,
			[]metadata.SearchResult{candidateA}, provider, &captureEmitter{}); ok {
			t.Fatal("sub-minimum local discography must not corroborate")
		}
	})
}
