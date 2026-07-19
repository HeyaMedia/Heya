package scanner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/titlematch"
)

const musicArtistAutoMatchThreshold = 0.85
const musicQueryOnlyCanonicalConfidence = 0.80
const musicArtistSearchTimeout = 3 * time.Minute
const musicArtistSearchConcurrency = 4
const musicArtistDiscoveryReleaseHintLimit = 3

// Literal artist identity is always searched first. These separators are used
// only for the second-chance primary-credit lookup after that literal lookup
// fails, so a fixed-name group such as "Above & Beyond" is never split while
// its canonical identity is available. Keep this aligned with HeyaMetadata's
// retained-credit parser; the deliberately compact production regression
// corpus covers every form seen in the real music library.
var musicCollaborationSeparatorRE = regexp.MustCompile(`(?i)(?:\s+(?:&|and|with|w/|feat\.?|featuring|ft\.?|f/|x|×|vs\.?|versus|presents|meets|/)\s+|\s+f\.\s*|\s*;\s+|\s+:\s+)`)

type MusicSearchProvider interface {
	Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error)
}

// MusicFingerprintEvidenceProvider supplies recording-level evidence for a
// local file. Implementations own fingerprint storage and provider lookups;
// scanner owns the conservative artist-level acceptance policy.
type MusicFingerprintEvidenceProvider interface {
	MatchTrack(context.Context, MusicTrackPlan) ([]MusicRecordingEvidence, error)
}

type MusicRecordingEvidence struct {
	RecordingMBID        string
	CanonicalRecordingID string
	Title                string
	FingerprintScore     float64
	SourceDuration       int
	RecordingDuration    int
	Artists              []MusicRecordingArtistEvidence
}

type MusicRecordingArtistEvidence struct {
	CanonicalID string
	Name        string
	MBID        string
}

type MusicSearchMatch struct {
	Key               string                           `json:"key"`
	Query             MusicSearchQuery                 `json:"query"`
	Accepted          bool                             `json:"accepted"`
	Reason            string                           `json:"reason,omitempty"`
	Error             string                           `json:"error,omitempty"`
	ProviderID        string                           `json:"provider_id,omitempty"`
	Provider          string                           `json:"provider,omitempty"`
	Artist            string                           `json:"artist,omitempty"`
	Confidence        float64                          `json:"confidence"`
	Candidates        []MusicSearchCandidate           `json:"candidates,omitempty"`
	ExternalIDs       map[string]string                `json:"external_ids,omitempty"`
	RecordingEvidence []MusicAcceptedRecordingEvidence `json:"recording_evidence,omitempty"`
	ManualDecision    string                           `json:"manual_decision,omitempty"`
}

// MusicAcceptedRecordingEvidence is the durable, per-file recording identity
// proved while AcoustID is resolving an otherwise ambiguous artist. RelPath is
// intentionally kept verbatim: apply may use this evidence only for the exact
// file that produced the fingerprint.
type MusicAcceptedRecordingEvidence struct {
	RelPath              string  `json:"rel_path"`
	RecordingMBID        string  `json:"recording_mbid"`
	CanonicalRecordingID string  `json:"canonical_recording_id"`
	Confidence           float64 `json:"confidence"`
	SourceDuration       int     `json:"source_duration"`
	RecordingDuration    int     `json:"recording_duration"`
}

type MusicSearchQuery struct {
	Artist   string                 `json:"artist"`
	Aliases  []string               `json:"aliases,omitempty"`
	Releases []metadata.ReleaseHint `json:"releases,omitempty"`
}

type MusicSearchCandidate struct {
	ProviderID     string                    `json:"provider_id"`
	Provider       string                    `json:"provider"`
	Artist         string                    `json:"artist"`
	Description    string                    `json:"description,omitempty"`
	PosterURL      string                    `json:"poster_url,omitempty"`
	HeyaSlug       string                    `json:"heya_slug,omitempty"`
	Confidence     float64                   `json:"confidence"`
	Recommendation string                    `json:"recommendation,omitempty"`
	Evidence       []metadata.SearchEvidence `json:"evidence,omitempty"`
	RequiresReview bool                      `json:"requires_review,omitempty"`
	ExternalIDs    map[string]string         `json:"external_ids,omitempty"`
}

func SearchMusicArtists(ctx context.Context, artists []MusicArtistPlan, provider MusicSearchProvider, emit Emitter, threshold float64, decisionsOpt ...SearchDecisions) ([]MusicSearchMatch, error) {
	return SearchMusicArtistsWithFingerprints(ctx, artists, provider, nil, emit, threshold, decisionsOpt...)
}

func SearchMusicArtistsWithFingerprints(ctx context.Context, artists []MusicArtistPlan, provider MusicSearchProvider, fingerprints MusicFingerprintEvidenceProvider, emit Emitter, threshold float64, decisionsOpt ...SearchDecisions) ([]MusicSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("music search provider is required")
	}
	if threshold <= 0 {
		threshold = musicArtistAutoMatchThreshold
	}

	decisions := optionalSearchDecisions(decisionsOpt)
	results := make([]MusicSearchMatch, len(artists))
	sem := make(chan struct{}, musicArtistSearchConcurrency)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var runErr error
	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		defer errMu.Unlock()
		if runErr == nil {
			runErr = err
		}
	}

	// On cancellation we must still fall through to wg.Wait() — returning
	// early would hand the caller a slice that in-flight goroutines are
	// still writing into.
fanout:
	for i, artist := range artists {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			setErr(ctx.Err())
			break fanout
		}
		wg.Add(1)
		go func(i int, artist MusicArtistPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				setErr(err)
				return
			}
			result, err := searchOneMusicArtist(ctx, artist, provider, fingerprints, emit, threshold, decisions)
			if err != nil {
				setErr(err)
				return
			}
			results[i] = result
		}(i, artist)
	}
	wg.Wait()
	if runErr != nil {
		return results, runErr
	}

	accepted := 0
	for _, result := range results {
		if result.Accepted {
			accepted++
		}
	}
	emit.Emit(Event{Event: "match.search_summary", Data: map[string]any{"domain": "music", "matches": len(results), "accepted": accepted}})
	return results, nil
}

func searchOneMusicArtist(ctx context.Context, artist MusicArtistPlan, provider MusicSearchProvider, fingerprints MusicFingerprintEvidenceProvider, emit Emitter, threshold float64, decisions SearchDecisions) (MusicSearchMatch, error) {
	releases := musicDiscoveryReleaseHints(artist.Albums)
	identifiers := musicDiscoveryArtistIdentifiers(artist.ExternalIDs)
	if identifiers["mbid"] != "" {
		// A consistent MusicBrainz artist ID is the authoritative artist spine.
		// Release identifiers remain useful when no artist spine is known, but
		// submitting them alongside an exact MBID lets one stale album/NFO turn
		// an otherwise exact artist lookup into conflicting-identifiers review.
		releases = musicReleaseHintsWithoutIdentifiers(releases)
	}
	query := metadata.SearchQuery{Title: artist.Artist, Identifiers: identifiers, Releases: releases}
	search := MusicSearchMatch{
		Key:   artist.Key,
		Query: MusicSearchQuery{Artist: artist.Artist, Releases: releases},
	}
	emit.Emit(Event{
		Event: "match.search",
		Kind:  "music",
		Data: map[string]any{
			"key":    artist.Key,
			"artist": artist.Artist,
		},
	})

	if decision, ok := decisions[artist.Key]; ok {
		if applied, handled := applyMusicSearchDecision(artist, search, decision, emit); handled {
			return applied, nil
		}
	}

	searchCtx, cancel := context.WithTimeout(ctx, musicArtistSearchTimeout)
	candidates, err := provider.Search(searchCtx, metadata.KindMusic, query)
	searchCtxErr := searchCtx.Err()
	cancel()
	if err != nil {
		if terminal := providerContextTermination(searchCtxErr, err); terminal != nil {
			return search, terminal
		}
		if _, deferred := metadata.DeferredWorkRetryAfter(err); deferred {
			return search, err
		}
		search.Reason = "search_error"
		search.Error = err.Error()
		emit.Emit(Event{
			Event:    "match.search_failed",
			Severity: SeverityWarn,
			Kind:     "music",
			Reason:   search.Reason,
			Message:  err.Error(),
			Data: map[string]any{
				"key":    artist.Key,
				"artist": artist.Artist,
			},
		})
		return search, nil
	}

	selectionArtist := artist
	scored := scoreMusicSearchResults(artist, candidates)
	if !musicSearchCanAutoAccept(scored, artist.Artist, threshold) {
		for _, fallbackName := range musicSearchFallbackArtists(artist) {
			fallbackArtist := artist
			fallbackArtist.Artist = fallbackName
			fallbackArtist.ExternalIDs = nil
			fallbackQuery := metadata.SearchQuery{Title: fallbackName, Releases: releases}
			fallbackCtx, fallbackCancel := context.WithTimeout(ctx, musicArtistSearchTimeout)
			fallbackCandidates, fallbackErr := provider.Search(fallbackCtx, metadata.KindMusic, fallbackQuery)
			fallbackCtxErr := fallbackCtx.Err()
			fallbackCancel()
			if fallbackErr != nil {
				if terminal := providerContextTermination(fallbackCtxErr, fallbackErr); terminal != nil {
					return search, terminal
				}
				if _, deferred := metadata.DeferredWorkRetryAfter(fallbackErr); deferred {
					return search, fallbackErr
				}
				emit.Emit(Event{
					Event: "match.collaboration_fallback_failed", Severity: SeverityInfo, Kind: "music",
					Message: fallbackErr.Error(),
					Data:    map[string]any{"key": artist.Key, "artist": artist.Artist, "fallback_artist": fallbackName},
				})
			} else {
				selectionArtist = fallbackArtist
				if !contains(search.Query.Aliases, artist.Artist) {
					search.Query.Aliases = append(search.Query.Aliases, artist.Artist)
				}
				scored = mergeScoredMusicSearchResults(scored, scoreMusicSearchResults(fallbackArtist, fallbackCandidates))
				sortMusicSearchCandidates(scored, selectionArtist)
				emit.Emit(Event{
					Event: "match.collaboration_fallback", Kind: "music",
					Data: map[string]any{
						"key": artist.Key, "artist": artist.Artist, "fallback_artist": fallbackName,
						"candidates": len(fallbackCandidates),
					},
				})
				if musicSearchCanAutoAccept(scored, selectionArtist.Artist, threshold) {
					break
				}
			}
		}
	}

	if !musicSearchCanAutoAccept(scored, selectionArtist.Artist, threshold) {
		converged, ok, err := resolveConvergedMusicCandidates(ctx, selectionArtist, scored, provider, threshold, emit)
		if err != nil {
			return search, err
		}
		if ok {
			scored = []metadata.SearchResult{converged}
		}
	}

	var acceptedRecordingEvidence []MusicAcceptedRecordingEvidence
	if !musicSearchCanAutoAccept(scored, selectionArtist.Artist, threshold) && fingerprints != nil {
		// Acoustic consensus is evaluated against the literal local identity,
		// never whichever collaboration component happened to be searched last.
		// Otherwise a credit such as "Jax Jones, Ado" can silently collapse onto
		// Ado merely because that owner fallback was tried before fingerprinting.
		fingerprintMatch, recordingEvidence, ok, fingerprintErr := resolveMusicArtistByFingerprint(ctx, artist, fingerprints, threshold, emit)
		if fingerprintErr != nil {
			if terminal := providerContextTermination(ctx.Err(), fingerprintErr); terminal != nil {
				return search, terminal
			}
			if _, deferred := metadata.DeferredWorkRetryAfter(fingerprintErr); deferred {
				return search, fingerprintErr
			}
			if musicFingerprintConfigurationFailure(fingerprintErr) {
				return search, fingerprintErr
			}
			emit.Emit(Event{Event: "match.fingerprint_failed", Severity: SeverityInfo, Kind: "music", Message: fingerprintErr.Error(), Data: map[string]any{
				"key": artist.Key, "artist": selectionArtist.Artist,
			}})
		} else if ok {
			scored = []metadata.SearchResult{fingerprintMatch}
			acceptedRecordingEvidence = recordingEvidence
		}
	}

	for _, candidate := range scored {
		providerID := musicPreferredProviderID(candidate)
		search.Candidates = append(search.Candidates, MusicSearchCandidate{
			ProviderID:     providerID,
			Provider:       candidate.ProviderName,
			Artist:         candidate.Title,
			Description:    candidate.Description,
			PosterURL:      candidate.PosterURL,
			HeyaSlug:       candidate.HeyaSlug,
			Confidence:     candidate.Confidence,
			Recommendation: candidate.Recommendation,
			Evidence:       candidate.Evidence,
			RequiresReview: candidate.RequiresReview,
			ExternalIDs:    candidate.ExternalIDs,
		})
		emit.Emit(Event{
			Event: "match.candidate",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  providerID,
				"artist":       candidate.Title,
				"confidence":   candidate.Confidence,
				"external_ids": candidate.ExternalIDs,
			},
		})
	}

	if len(scored) == 0 {
		search.Reason = "no_candidates"
		emit.Emit(Event{Event: "match.unresolved", Kind: "music", Reason: search.Reason, Data: map[string]any{"key": artist.Key, "artist": artist.Artist}})
		return search, nil
	}

	top := scored[0]
	clearGap := musicSearchClearGap(scored, selectionArtist.Artist)
	if !top.RequiresReview && top.Confidence >= threshold && clearGap {
		providerID := musicPreferredProviderID(top)
		search.Accepted = true
		search.ProviderID = providerID
		search.Provider = top.ProviderName
		search.Artist = top.Title
		search.Confidence = top.Confidence
		search.ExternalIDs = top.ExternalIDs
		if top.Recommendation == "fingerprint_match" {
			search.RecordingEvidence = append([]MusicAcceptedRecordingEvidence{}, acceptedRecordingEvidence...)
		}
		emit.Emit(Event{
			Event: "match.selected",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  providerID,
				"artist":       top.Title,
				"confidence":   top.Confidence,
				"external_ids": top.ExternalIDs,
			},
		})
	} else {
		search.Reason = "ambiguous_or_low_confidence"
		search.Confidence = top.Confidence
		emit.Emit(Event{
			Event:  "match.rejected",
			Kind:   "music",
			Reason: search.Reason,
			Data: map[string]any{
				"key":        artist.Key,
				"top_artist": top.Title,
				"confidence": top.Confidence,
				"clear_gap":  clearGap,
			},
		})
	}
	return search, nil
}

type musicConfigurationError interface {
	IsConfigurationError() bool
}

func musicFingerprintConfigurationFailure(err error) bool {
	var configurationErr musicConfigurationError
	return errors.As(err, &configurationErr) && configurationErr.IsConfigurationError()
}

const (
	musicFingerprintTrackLimit           = 3
	musicFingerprintMinimumScore         = .90
	musicFingerprintOverrideMinimumScore = .95
	musicFingerprintOverrideMinimumTitle = .90
)

func resolveMusicArtistByFingerprint(ctx context.Context, artist MusicArtistPlan, provider MusicFingerprintEvidenceProvider, threshold float64, emit Emitter) (metadata.SearchResult, []MusicAcceptedRecordingEvidence, bool, error) {
	tracks := representativeFingerprintTracks(artist)
	if len(tracks) == 0 {
		return metadata.SearchResult{}, nil, false, nil
	}
	type support struct {
		artist             MusicRecordingArtistEvidence
		files              int
		totalScore         float64
		artistNameOverride bool
		recordings         []string
		recordingIDs       map[string]bool
		evidence           []MusicAcceptedRecordingEvidence
	}
	byArtist := map[string]*support{}
	decisiveFiles := 0
	for _, track := range tracks {
		values, err := provider.MatchTrack(ctx, track)
		if err != nil {
			return metadata.SearchResult{}, nil, false, err
		}
		best, ok := bestMusicRecordingEvidence(artist, track, values)
		if !ok {
			continue
		}
		decisiveFiles++
		entry := byArtist[best.artist.CanonicalID]
		if entry == nil {
			entry = &support{artist: best.artist, recordingIDs: map[string]bool{}}
			byArtist[best.artist.CanonicalID] = entry
		}
		entry.files++
		entry.totalScore += best.score
		entry.artistNameOverride = entry.artistNameOverride || best.artistNameOverride
		entry.recordings = append(entry.recordings, best.recordingMBID)
		if best.recordingIdentityAccepted {
			entry.recordingIDs[strings.ToLower(strings.TrimSpace(best.recordingMBID))] = true
			entry.evidence = append(entry.evidence, MusicAcceptedRecordingEvidence{
				RelPath: track.RelPath, RecordingMBID: best.recordingMBID,
				CanonicalRecordingID: best.canonicalRecordingID, Confidence: best.score,
				SourceDuration: best.sourceDuration, RecordingDuration: best.recordingDuration,
			})
		}
	}
	if decisiveFiles == 0 || len(byArtist) != 1 {
		return metadata.SearchResult{}, nil, false, nil
	}
	var winner *support
	for _, value := range byArtist {
		winner = value
	}
	required := 1
	if len(tracks) > 1 {
		required = min(2, len(tracks))
	}
	if winner.files < required || winner.files != decisiveFiles {
		return metadata.SearchResult{}, nil, false, nil
	}
	nameOverrideAllowed := musicFingerprintAllowsArtistNameOverride(artist)
	if !nameOverrideAllowed && !musicSearchArtistExact(artist, winner.artist.Name) {
		return metadata.SearchResult{}, nil, false, nil
	}
	if winner.artistNameOverride && (!nameOverrideAllowed || winner.files < 2 || len(winner.recordingIDs) < 2) {
		return metadata.SearchResult{}, nil, false, nil
	}
	average := winner.totalScore / float64(winner.files)
	if average < musicFingerprintMinimumScore {
		return metadata.SearchResult{}, nil, false, nil
	}
	confidence := math.Min(.99, math.Max(threshold, average))
	evidence := []metadata.SearchEvidence{
		{Field: "chromaprint", Outcome: fmt.Sprintf("%d_of_%d", winner.files, len(tracks)), Weight: confidence, Detail: "AcoustID recording matches converge on one canonical artist"},
		{Field: "musicbrainz_recordings", Outcome: "matched", Weight: confidence, Detail: strings.Join(cleanSortedStrings(winner.recordings), ",")},
	}
	if winner.artistNameOverride {
		evidence = append(evidence, metadata.SearchEvidence{
			Field: "artist_name", Outcome: "overridden_by_recordings", Weight: confidence,
			Detail: "At least two independent recording fingerprints converge despite the local artist name",
		})
	}
	result := metadata.SearchResult{
		ProviderID: heyametadata.EncodeEntityProviderID(winner.artist.CanonicalID), ProviderName: "heya",
		Title: winner.artist.Name, Confidence: confidence, Recommendation: "fingerprint_match",
		RequiresReview: false, Enriched: true, Evidence: evidence,
		ExternalIDs: map[string]string{"mbid": winner.artist.MBID}, HeyaSlug: winner.artist.CanonicalID,
	}
	emit.Emit(Event{Event: "match.fingerprint_selected", Kind: "music", Data: map[string]any{
		"key": artist.Key, "artist": artist.Artist, "canonical_id": winner.artist.CanonicalID,
		"recordings": winner.files, "confidence": confidence, "artist_name_override": winner.artistNameOverride,
	}})
	sort.Slice(winner.evidence, func(i, j int) bool { return winner.evidence[i].RelPath < winner.evidence[j].RelPath })
	return result, winner.evidence, true, nil
}

type scoredMusicRecordingEvidence struct {
	artist                    MusicRecordingArtistEvidence
	recordingMBID             string
	canonicalRecordingID      string
	sourceDuration            int
	recordingDuration         int
	artistNameOverride        bool
	recordingIdentityAccepted bool
	score                     float64
}

func bestMusicRecordingEvidence(artist MusicArtistPlan, track MusicTrackPlan, values []MusicRecordingEvidence) (scoredMusicRecordingEvidence, bool) {
	var candidates []scoredMusicRecordingEvidence
	for _, value := range values {
		if value.FingerprintScore < musicFingerprintMinimumScore {
			continue
		}
		if _, err := uuid.Parse(strings.TrimSpace(value.RecordingMBID)); err != nil {
			continue
		}
		_, canonicalRecordingErr := uuid.Parse(strings.TrimSpace(value.CanonicalRecordingID))
		recordingIdentityAccepted := canonicalRecordingErr == nil
		titleScore := musicNameSimilarity(track.TrackTitle, value.Title)
		if titleScore < musicArtistAutoMatchThreshold || !musicDurationsCompatible(value.SourceDuration, value.RecordingDuration) {
			continue
		}
		validCredits := musicUniqueRecordingArtistCredits(value.Artists)
		matchedArtist := false
		for _, credit := range validCredits {
			if musicNameSimilarity(artist.Artist, credit.Name) < musicArtistAutoMatchThreshold {
				continue
			}
			matchedArtist = true
			durationScore := musicDurationScore(value.SourceDuration, value.RecordingDuration)
			score := value.FingerprintScore*.60 + titleScore*.25 + durationScore*.15
			candidates = append(candidates, scoredMusicRecordingEvidence{
				artist: credit, recordingMBID: value.RecordingMBID, canonicalRecordingID: value.CanonicalRecordingID,
				sourceDuration: value.SourceDuration, recordingDuration: value.RecordingDuration,
				recordingIdentityAccepted: recordingIdentityAccepted, score: score,
			})
		}
		// Name evidence may itself be poisoned. Only expose a name-override
		// candidate when the recording has one unambiguous canonical artist;
		// the resolver still requires two distinct recordings to converge before
		// it can auto-accept that candidate.
		if !matchedArtist && len(validCredits) == 1 && value.FingerprintScore >= musicFingerprintOverrideMinimumScore && titleScore >= musicFingerprintOverrideMinimumTitle {
			credit := validCredits[0]
			durationScore := musicDurationScore(value.SourceDuration, value.RecordingDuration)
			score := value.FingerprintScore*.60 + titleScore*.25 + durationScore*.15
			candidates = append(candidates, scoredMusicRecordingEvidence{
				artist: credit, recordingMBID: value.RecordingMBID, canonicalRecordingID: value.CanonicalRecordingID,
				sourceDuration: value.SourceDuration, recordingDuration: value.RecordingDuration,
				artistNameOverride: true, recordingIdentityAccepted: recordingIdentityAccepted, score: score,
			})
		}
	}
	if len(candidates) == 0 {
		return scoredMusicRecordingEvidence{}, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	best := candidates[0]
	if len(candidates) > 1 && best.score-candidates[1].score < .05 && !musicSameRecordingEvidence(best, candidates[1]) {
		if best.artist.CanonicalID != candidates[1].artist.CanonicalID || best.artistNameOverride {
			return scoredMusicRecordingEvidence{}, false
		}
		// Both candidates still prove the same artist, but not one exact local
		// recording. Keep the artist vote and deliberately omit per-file binding.
		best.recordingIdentityAccepted = false
	}
	return best, true
}

func musicSameRecordingEvidence(left, right scoredMusicRecordingEvidence) bool {
	return strings.EqualFold(strings.TrimSpace(left.recordingMBID), strings.TrimSpace(right.recordingMBID)) &&
		strings.EqualFold(strings.TrimSpace(left.canonicalRecordingID), strings.TrimSpace(right.canonicalRecordingID))
}

func musicUniqueRecordingArtistCredits(values []MusicRecordingArtistEvidence) []MusicRecordingArtistEvidence {
	seen := map[string]bool{}
	out := make([]MusicRecordingArtistEvidence, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value.CanonicalID))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func musicFingerprintAllowsArtistNameOverride(artist MusicArtistPlan) bool {
	if len(musicSearchFallbackArtists(artist)) > 0 {
		return false
	}
	value := strings.TrimSpace(artist.Artist)
	switch normalizeMusicKeyPart(value) {
	case "various artists", "various", "va", "unknown artist", "unknown", "soundtrack", "original soundtrack":
		return false
	}
	if musicPrimaryCollaborationArtist(value) != "" {
		return false
	}
	parts := strings.Split(value, ",")
	nonEmpty := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			nonEmpty++
		}
	}
	return nonEmpty <= 1
}

func representativeFingerprintTracks(artist MusicArtistPlan) []MusicTrackPlan {
	var tracks []MusicTrackPlan
	seenAlbums := map[string]bool{}
	for _, album := range artist.Albums {
		for _, track := range album.Tracks {
			if strings.TrimSpace(track.RelPath) == "" || strings.TrimSpace(track.TrackTitle) == "" {
				continue
			}
			albumKey := normalizeMusicKeyPart(album.Album)
			if seenAlbums[albumKey] && len(tracks) < musicFingerprintTrackLimit-1 {
				continue
			}
			seenAlbums[albumKey] = true
			tracks = append(tracks, track)
			break
		}
		if len(tracks) == musicFingerprintTrackLimit {
			return tracks
		}
	}
	// A single album can still contribute multiple independent recordings.
	if len(tracks) < musicFingerprintTrackLimit {
		seenPaths := map[string]bool{}
		for _, track := range tracks {
			seenPaths[track.RelPath] = true
		}
		for _, album := range artist.Albums {
			for _, track := range album.Tracks {
				if seenPaths[track.RelPath] || track.RelPath == "" || track.TrackTitle == "" {
					continue
				}
				tracks = append(tracks, track)
				seenPaths[track.RelPath] = true
				if len(tracks) == musicFingerprintTrackLimit {
					return tracks
				}
			}
		}
	}
	return tracks
}

func musicDurationsCompatible(left, right int) bool {
	if left <= 0 || right <= 0 {
		return false
	}
	delta := absInt(left - right)
	return delta <= max(8, int(math.Round(float64(max(left, right))*.05)))
}

func musicDurationScore(left, right int) float64 {
	if !musicDurationsCompatible(left, right) {
		return 0
	}
	delta := float64(absInt(left - right))
	return math.Max(0, 1-delta/float64(max(left, right)))
}

func cleanSortedStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func scoreMusicSearchResults(artist MusicArtistPlan, candidates []metadata.SearchResult) []metadata.SearchResult {
	scored := append([]metadata.SearchResult(nil), candidates...)
	for i := range scored {
		scored[i].Confidence = scoreMusicSearchCandidate(artist, scored[i])
	}
	sortMusicSearchCandidates(scored, artist)
	return scored
}

func sortMusicSearchCandidates(scored []metadata.SearchResult, artist MusicArtistPlan) {
	sort.Slice(scored, func(i, j int) bool {
		// Query-only canonical hits are deliberately review-only when artist
		// discovery has structured release evidence. Never let their exact-name
		// score tie place them ahead of the provider-approved discovery result.
		if scored[i].RequiresReview != scored[j].RequiresReview {
			return !scored[i].RequiresReview
		}
		if scored[i].Confidence == scored[j].Confidence {
			iExact := musicSearchArtistExact(artist, scored[i].Title)
			jExact := musicSearchArtistExact(artist, scored[j].Title)
			if iExact != jExact {
				return iExact
			}
			iCase := strings.TrimSpace(scored[i].Title) == strings.TrimSpace(artist.Artist)
			jCase := strings.TrimSpace(scored[j].Title) == strings.TrimSpace(artist.Artist)
			if iCase != jCase {
				return iCase
			}
			return scored[i].Title < scored[j].Title
		}
		return scored[i].Confidence > scored[j].Confidence
	})
}

func musicSearchCanAutoAccept(scored []metadata.SearchResult, queryArtist string, threshold float64) bool {
	if len(scored) == 0 {
		return false
	}
	top := scored[0]
	return !top.RequiresReview && top.Confidence >= threshold && musicSearchClearGap(scored, queryArtist)
}

func musicPrimaryCollaborationArtist(value string) string {
	parts := musicCollaborationSeparatorRE.Split(strings.TrimSpace(value), 2)
	if len(parts) != 2 {
		return ""
	}
	primary := strings.TrimSpace(parts[0])
	if primary == "" || strings.TrimSpace(parts[1]) == "" || normalizeMusicKeyPart(primary) == normalizeMusicKeyPart(value) {
		return ""
	}
	return primary
}

// musicSearchFallbackArtists uses the physical owner folder as the first
// second-chance identity. A release credited to "Jax Jones, Ado" inside the
// Ado owner scope should try Ado before heuristically splitting the credit;
// likewise "Above & Beyond ft Zoe Johnston" should try the storage owner
// "Above and Beyond", not the meaningless first token "Above". We only use
// this after the literal identity failed, preserving real fixed-name groups.
func musicSearchFallbackArtists(artist MusicArtistPlan) []string {
	var fallbacks []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || normalizeMusicKeyPart(value) == normalizeMusicKeyPart(artist.Artist) {
			return
		}
		for _, existing := range fallbacks {
			if normalizeMusicKeyPart(existing) == normalizeMusicKeyPart(value) {
				return
			}
		}
		fallbacks = append(fallbacks, value)
	}
	// The physical owner is only evidence when it is actually part of the
	// credited identity. Music libraries often have staging/miscellaneous
	// owners whose children are correctly tagged as unrelated artists. Using
	// such an owner as a generic fallback turns a failed MINNIE lookup under an
	// "Explo" directory into an Explo candidate and pollutes the review result.
	// Collaboration credits such as "Jax Jones, Ado" under Ado still get the
	// intended owner fallback.
	owner := musicStorageOwnerArtist(artist.Files)
	if musicCreditContainsArtist(artist.Artist, owner) {
		add(owner)
	}
	add(musicPrimaryCollaborationArtist(artist.Artist))
	return fallbacks
}

func musicStorageOwnerArtist(files []string) string {
	owners := map[string]string{}
	for _, path := range files {
		segments := splitRelPath(path)
		if len(segments) < 2 {
			continue
		}
		owner, _ := splitMusicArtistFolder(segments[0])
		key := normalizeMusicKeyPart(owner)
		if key != "" {
			owners[key] = owner
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for _, owner := range owners {
		return owner
	}
	return ""
}

func mergeScoredMusicSearchResults(groups ...[]metadata.SearchResult) []metadata.SearchResult {
	indices := map[string]int{}
	var merged []metadata.SearchResult
	for _, group := range groups {
		for _, candidate := range group {
			if index, ok := indices[candidate.ProviderID]; ok {
				current := merged[index]
				if (current.RequiresReview && !candidate.RequiresReview) ||
					(current.RequiresReview == candidate.RequiresReview && candidate.Confidence > current.Confidence) {
					merged[index] = candidate
				}
				continue
			}
			indices[candidate.ProviderID] = len(merged)
			merged = append(merged, candidate)
		}
	}
	return merged
}

func musicDiscoveryReleaseHints(albums []MusicAlbumPlan) []metadata.ReleaseHint {
	if len(albums) == 0 {
		return nil
	}
	candidates := append([]MusicAlbumPlan(nil), albums...)
	sort.SliceStable(candidates, func(i, j int) bool {
		iPriority := musicDiscoveryReleasePriority(candidates[i].ReleaseKind)
		jPriority := musicDiscoveryReleasePriority(candidates[j].ReleaseKind)
		if iPriority != jPriority {
			return iPriority > jPriority
		}
		if len(candidates[i].Tracks) != len(candidates[j].Tracks) {
			return len(candidates[i].Tracks) > len(candidates[j].Tracks)
		}
		if candidates[i].Year != candidates[j].Year {
			return candidates[i].Year < candidates[j].Year
		}
		return candidates[i].Album < candidates[j].Album
	})

	seen := make(map[string]struct{}, musicArtistDiscoveryReleaseHintLimit)
	hints := make([]metadata.ReleaseHint, 0, musicArtistDiscoveryReleaseHintLimit)
	for _, album := range candidates {
		title := strings.TrimSpace(album.Album)
		key := normalizeMusicKeyPart(title)
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		hints = append(hints, metadata.ReleaseHint{
			Title:       title,
			Year:        album.Year,
			Type:        album.ReleaseKind,
			Identifiers: musicReleaseHintIdentifiers(album.ExternalIDs),
		})
		if len(hints) == musicArtistDiscoveryReleaseHintLimit {
			break
		}
	}
	return hints
}

func musicDiscoveryArtistIdentifiers(values map[string]string) map[string]string {
	if mbid := strings.TrimSpace(values["mbid"]); mbid != "" {
		return map[string]string{"mbid": mbid}
	}
	if apple := strings.TrimSpace(values["apple"]); apple != "" {
		return map[string]string{"apple": apple}
	}
	return nil
}

func musicReleaseHintsWithoutIdentifiers(values []metadata.ReleaseHint) []metadata.ReleaseHint {
	if len(values) == 0 {
		return nil
	}
	result := append([]metadata.ReleaseHint(nil), values...)
	for i := range result {
		result[i].Identifiers = nil
	}
	return result
}

// resolveConvergedMusicCandidates handles the safe subset of duplicate
// review candidates: opaque conflict candidates which all resolve (including
// redirects) to one canonical Heya entity. Same labels alone are never enough;
// genuinely distinct same-name artists retain their separate canonical IDs
// and stay in review.
func resolveConvergedMusicCandidates(ctx context.Context, artist MusicArtistPlan, scored []metadata.SearchResult, provider MusicSearchProvider, threshold float64, emit Emitter) (metadata.SearchResult, bool, error) {
	if len(scored) < 2 || !musicCandidateRecommendationCanConverge(scored[0].Recommendation) ||
		scored[0].Confidence < threshold || !musicSearchArtistExact(artist, scored[0].Title) {
		return metadata.SearchResult{}, false, nil
	}
	detailProvider, ok := provider.(MusicDetailProvider)
	if !ok {
		return metadata.SearchResult{}, false, nil
	}

	top := scored[0]
	topTitle := normalizeMusicKeyPart(top.Title)
	var duplicates []metadata.SearchResult
	for _, candidate := range scored {
		if candidate.Confidence < threshold || top.Confidence-candidate.Confidence > 0.10 {
			continue
		}
		if candidate.Recommendation != top.Recommendation || candidate.RequiresReview != top.RequiresReview {
			continue
		}
		if normalizeMusicKeyPart(candidate.Title) != topTitle {
			return metadata.SearchResult{}, false, nil
		}
		duplicates = append(duplicates, candidate)
	}
	if len(duplicates) < 2 || len(duplicates) > musicFetchCandidateLimit {
		return metadata.SearchResult{}, false, nil
	}

	canonicalID := ""
	var canonical *metadata.MediaDetail
	for _, candidate := range duplicates {
		fetchCtx, cancel := context.WithTimeout(ctx, musicMetadataFetchTimeout)
		detail, err := detailProvider.GetDetail(fetchCtx, candidate.ProviderID, nil)
		fetchCtxErr := fetchCtx.Err()
		cancel()
		if err != nil {
			if terminal := providerContextTermination(fetchCtxErr, err); terminal != nil {
				return metadata.SearchResult{}, false, terminal
			}
			if _, deferred := metadata.DeferredWorkRetryAfter(err); deferred {
				return metadata.SearchResult{}, false, err
			}
			emit.Emit(Event{Event: "match.candidate_convergence_failed", Severity: SeverityInfo, Kind: "music", Message: err.Error(), Data: map[string]any{
				"key": artist.Key, "provider_id": candidate.ProviderID,
			}})
			return metadata.SearchResult{}, false, nil
		}
		if detail == nil || strings.TrimSpace(detail.CanonicalID) == "" {
			return metadata.SearchResult{}, false, nil
		}
		if canonicalID == "" {
			canonicalID = detail.CanonicalID
			canonical = detail
		} else if canonicalID != detail.CanonicalID {
			return metadata.SearchResult{}, false, nil
		}
	}

	result := top
	result.ProviderID = heyametadata.EncodeEntityProviderID(canonicalID)
	result.Title = firstNonEmpty(canonical.ArtistName, canonical.Title, top.Title)
	result.Description = firstNonEmpty(canonical.ArtistDisambiguation, top.Description)
	result.ExternalIDs = cloneStringMap(canonical.ExternalIDs)
	result.HeyaSlug = canonicalID
	result.Recommendation = "canonical_convergence"
	result.RequiresReview = false
	result.Enriched = true
	emit.Emit(Event{Event: "match.candidates_converged", Kind: "music", Data: map[string]any{
		"key": artist.Key, "artist": artist.Artist, "canonical_id": canonicalID, "candidates": len(duplicates),
	}})
	return result, true, nil
}

func musicCandidateRecommendationCanConverge(recommendation string) bool {
	switch recommendation {
	case "ambiguous", "conflicting_identifiers":
		return true
	default:
		return false
	}
}

// musicReleaseHintIdentifiers keeps exact release/catalog identifiers while
// excluding artist-level evidence that happens to be carried by the album
// plan. HeyaMetadata owns provider routing and namespace normalization; Heya
// merely preserves the structured evidence it already parsed from tags/NFOs.
func musicReleaseHintIdentifiers(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"apple_album": {}, "apple_music_album": {}, "itunes_album": {},
		"deezer_album": {}, "discogs_release": {}, "discogs_master": {},
		"musicbrainz_album": {}, "musicbrainz_release_group": {},
		"spotify_album": {}, "audiodb_album": {},
	}
	result := make(map[string]string)
	for key, value := range values {
		if _, ok := allowed[key]; ok && strings.TrimSpace(value) != "" {
			result[key] = strings.TrimSpace(value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func musicDiscoveryReleasePriority(releaseType string) int {
	switch normalizeMusicReleaseKind(releaseType) {
	case "album":
		return 4
	case "ep":
		return 3
	case "single":
		return 2
	case "compilation":
		return 1
	default:
		return 0
	}
}

func applyMusicSearchDecision(artist MusicArtistPlan, search MusicSearchMatch, decision SearchDecision, emit Emitter) (MusicSearchMatch, bool) {
	switch decision.Status {
	case "accepted":
		if decision.ProviderID == "" {
			return search, false
		}
		candidate := MusicSearchCandidate{
			ProviderID:  decision.ProviderID,
			Provider:    firstNonEmpty(decision.Provider, "heya"),
			Artist:      firstNonEmpty(decision.Title, artist.Artist),
			Confidence:  decision.Confidence,
			ExternalIDs: decision.ExternalIDs,
		}
		if candidate.Confidence == 0 {
			candidate.Confidence = 1
		}
		search.Accepted = true
		search.ProviderID = candidate.ProviderID
		search.Provider = candidate.Provider
		search.Artist = candidate.Artist
		search.Confidence = candidate.Confidence
		search.ExternalIDs = candidate.ExternalIDs
		search.Candidates = []MusicSearchCandidate{candidate}
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event: "match.manual_selected",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  candidate.ProviderID,
				"artist":       candidate.Artist,
				"confidence":   candidate.Confidence,
				"external_ids": candidate.ExternalIDs,
			},
		})
		return search, true
	case "rejected", "ignored":
		search.Reason = "manual_" + decision.Status
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event:  "match.manual_blocked",
			Kind:   "music",
			Reason: search.Reason,
			Data: map[string]any{
				"key":    artist.Key,
				"status": decision.Status,
				"artist": artist.Artist,
			},
		})
		return search, true
	default:
		return search, false
	}
}

func scoreMusicSearchCandidate(artist MusicArtistPlan, candidate metadata.SearchResult) float64 {
	// A hard identifier is a constraint, not merely another positive feature.
	// One shared provider ID can never cancel a contradictory ID in another
	// namespace (for example Apple A paired with local MBID X and remote MBID Y),
	// and an exact label cannot override that contradiction either.
	if musicArtistHardIDsConflict(artist.ExternalIDs, candidate.ExternalIDs) {
		return 0
	}
	if musicArtistHardIDsCanonicalEquivalent(artist.ExternalIDs, candidate.ExternalIDs) {
		return 1
	}
	primary := musicNameSimilarity(artist.Artist, candidate.Title)
	best := primary
	for _, alt := range candidate.AltTitles {
		if alt == "" {
			continue
		}
		if score := musicNameSimilarity(artist.Artist, alt); score > best {
			if musicShortAliasNeedsPrimarySupport(artist.Artist, candidate.Title) && score >= musicArtistAutoMatchThreshold {
				score = maxFloat(primary, 0.80)
			}
			best = score
		}
	}

	// HeyaMetadata's discovery confidence already combines provider quality,
	// name/alias similarity, and structured release evidence. Replacing it
	// with the local name score made every exact-name identity a perfect 1.0,
	// producing enormous artificial ties for common and reused artist names.
	// The local score remains a safety ceiling so an upstream fuzzy result can
	// never outrank what the submitted artist label actually supports.
	if candidate.Confidence > 0 {
		best = minFloat(best, candidate.Confidence)
	}

	// Canonical index summaries use confidence=1 to order text search results,
	// but when HeyaMetadata explicitly marks one review-only and supplies no
	// evidence, that number is not identity confidence. Keep the useful known
	// entity visible without allowing it to masquerade as corroboration.
	if candidate.Enriched && candidate.RequiresReview && len(candidate.Evidence) == 0 {
		best = minFloat(best, musicQueryOnlyCanonicalConfidence)
	}
	return best
}

func musicArtistHardIDsConflict(a, b map[string]string) bool {
	for _, compare := range []musicIDCompare{
		{Local: []string{"mbid", "musicbrainz", "musicbrainz_artist", "musicbrainz_album_artist"}, Remote: []string{"mbid", "musicbrainz", "musicbrainz_artist", "musicbrainz_album_artist"}},
		{Local: []string{"apple", "itunes_artist", "apple_artist"}, Remote: []string{"apple", "itunes_artist", "apple_artist"}},
		{Local: []string{"deezer", "deezer_artist"}, Remote: []string{"deezer", "deezer_artist"}},
		{Local: []string{"discogs", "discogs_artist"}, Remote: []string{"discogs", "discogs_artist"}},
		{Local: []string{"spotify", "spotify_artist"}, Remote: []string{"spotify", "spotify_artist"}},
		{Local: []string{"audiodb", "audiodb_artist"}, Remote: []string{"audiodb", "audiodb_artist"}},
	} {
		_, left := firstMusicID(a, compare.Local)
		_, right := firstMusicID(b, compare.Remote)
		if left != "" && right != "" && !strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
			return true
		}
	}
	return false
}

func musicShortAliasNeedsPrimarySupport(query, primaryTitle string) bool {
	nq := normalizeMusicKeyPart(query)
	if len(strings.Fields(nq)) != 1 || len(nq) >= 5 {
		return false
	}
	return nq != normalizeMusicKeyPart(primaryTitle)
}

func musicNameSimilarity(a, b string) float64 {
	na := normalizeMusicKeyPart(a)
	nb := normalizeMusicKeyPart(b)
	if na == nb && na != "" {
		return 1
	}
	// normalizeMusicKeyPart drops punctuation, so an ampersand disappears
	// while the word "and" remains. Preserve the conjunction before generic
	// normalization so storage folder "Above and Beyond" is exact-equivalent
	// to canonical "Above & Beyond" without making leading "And" optional.
	if ca, cb := musicConjunctionKey(a), musicConjunctionKey(b); ca != "" && ca == cb {
		return 1
	}
	if na == "" || nb == "" {
		return 0
	}
	if musicNumberedDisambiguationMismatch(a, b) {
		return musicNormalizedSimilarity(na, nb)
	}
	if titlematch.FuzzyEqual(a, b) && musicFuzzyMatchSafe(na, nb) {
		return 1
	}
	return musicNormalizedSimilarity(na, nb)
}

func musicConjunctionKey(value string) string {
	value = strings.NewReplacer("&", " and ", "＆", " and ").Replace(value)
	return normalizeMusicKeyPart(value)
}

func musicFuzzyMatchSafe(na, nb string) bool {
	aFields := strings.Fields(na)
	bFields := strings.Fields(nb)
	if len(aFields) == 0 || len(bFields) == 0 {
		return false
	}
	if len(aFields) == len(bFields) {
		if len(aFields) == 1 && minInt(len(na), len(nb)) < 5 {
			return false
		}
		return true
	}
	shorterLen := minInt(len(na), len(nb))
	return absInt(len(aFields)-len(bFields)) <= 1 && shorterLen >= 8
}

func musicNormalizedSimilarity(na, nb string) float64 {
	if na == nb && na != "" {
		return 1
	}
	if na == "" || nb == "" {
		return 0
	}
	d := levenshteinDistance(na, nb)
	maxLen := len(na)
	if len(nb) > maxLen {
		maxLen = len(nb)
	}
	score := 1 - float64(d)/float64(maxLen)
	if substringSearchTitleMatch(na, nb) && score < 0.80 {
		score = 0.80
	}
	return score
}

func musicNumberedDisambiguationMismatch(a, b string) bool {
	sa := strings.TrimSpace(musicNumberedDisambigRE.ReplaceAllString(a, ""))
	sb := strings.TrimSpace(musicNumberedDisambigRE.ReplaceAllString(b, ""))
	if sa == a && sb == b {
		return false
	}
	return normalizeMusicKeyPart(sa) == normalizeMusicKeyPart(sb)
}

func musicSearchClearGap(results []metadata.SearchResult, queryArtist string) bool {
	if len(results) == 1 {
		return true
	}
	top := results[0]
	secondIdentity := -1
	for i := 1; i < len(results); i++ {
		if !top.RequiresReview && results[i].RequiresReview {
			// Provider-approved discovery evidence outranks query-only canonical
			// suggestions which the provider explicitly marked for review. The
			// identity-aware gap below applies among auto-acceptable candidates.
			continue
		}
		if musicSearchSameCanonicalIdentity(top, results[i]) {
			// Multiple provider projections of the same hard identity are not
			// independent alternatives. A same-name result with a different
			// provider/canonical identity is, however, a real competitor.
			continue
		}
		secondIdentity = i
		break
	}
	if secondIdentity == -1 {
		return true
	}
	// An exact spelling is useful scoring evidence, not proof that two
	// same-name canonical artists are the same person. Always require the
	// ordinary runner-up margin between distinct identities.
	return top.Confidence-results[secondIdentity].Confidence > 0.10
}

func musicSearchSameCanonicalIdentity(a, b metadata.SearchResult) bool {
	if a.ProviderID != "" && b.ProviderID != "" && strings.EqualFold(strings.TrimSpace(a.ProviderID), strings.TrimSpace(b.ProviderID)) {
		return true
	}
	return musicArtistHardIDsCanonicalEquivalent(a.ExternalIDs, b.ExternalIDs)
}

func musicSearchArtistExact(artist MusicArtistPlan, candidate string) bool {
	return normalizeMusicKeyPart(artist.Artist) == normalizeMusicKeyPart(candidate)
}

func musicPreferredProviderID(result metadata.SearchResult) string {
	return result.ProviderID
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

func sortMusicSearchResults(items []MusicSearchMatch) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Query.Artist) < strings.ToLower(items[j].Query.Artist)
	})
}
