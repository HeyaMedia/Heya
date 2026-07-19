package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/acoustid"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
)

const (
	acoustIDLookupProvider = "acoustid"
	acoustIDMatchTTL       = 30 * 24 * time.Hour
	acoustIDNoMatchTTL     = 7 * 24 * time.Hour
	acoustIDFailureBackoff = time.Hour
	acoustIDResolveLimit   = 3
	acoustIDMinimumScore   = .90
)

var errAmbiguousMusicFingerprintFile = errors.New("music fingerprint source is ambiguous across library roots")

type recordingMBIDResolver interface {
	ResolveRecordingMBID(context.Context, string) (metadata.RecordingMetadata, error)
}

type musicFingerprintMatcher struct {
	db       *pgxpool.Pool
	library  sqlc.Library
	acoustID *acoustid.Client
	resolver recordingMBIDResolver
}

func newMusicFingerprintMatcher(db *pgxpool.Pool, library sqlc.Library, client *acoustid.Client, resolver recordingMBIDResolver) scanner.MusicFingerprintEvidenceProvider {
	if db == nil || client == nil || !client.Enabled() || resolver == nil || library.MediaType != sqlc.MediaTypeMusic {
		return nil
	}
	return &musicFingerprintMatcher{db: db, library: library, acoustID: client, resolver: resolver}
}

func (m *musicFingerprintMatcher) MatchTrack(ctx context.Context, track scanner.MusicTrackPlan) ([]scanner.MusicRecordingEvidence, error) {
	lf, err := m.libraryFile(ctx, track.RelPath)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errAmbiguousMusicFingerprintFile) {
		// RelPath does not encode the inventory root. Acoustic evidence is far
		// too strong to guess when the same relative path exists under multiple
		// roots; leave the artist for ordinary evidence/user review instead.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q := sqlc.New(m.db)
	// Name and release evidence has already declined the artist before this is
	// called. Generate the representative track fingerprint inline when the
	// background corpus sweep has not reached it yet.
	fingerprint, err := ensureLibraryFileFingerprint(ctx, q, lf, 0)
	if err != nil {
		return nil, err
	}

	matches, err := m.lookup(ctx, q, fingerprint)
	if err != nil {
		return nil, err
	}
	return resolveAcoustIDRecordingEvidence(ctx, matches, fingerprint, m.resolver)
}

func resolveAcoustIDRecordingEvidence(ctx context.Context, matches []acoustid.Match, fingerprint sqlc.LibraryFileFingerprint, resolver recordingMBIDResolver) ([]scanner.MusicRecordingEvidence, error) {
	result := make([]scanner.MusicRecordingEvidence, 0, min(len(matches), acoustIDResolveLimit))
	var firstResolveErr error
	for index, match := range matches {
		if index == acoustIDResolveLimit || match.Score < acoustIDMinimumScore {
			break
		}
		recording, resolveErr := resolver.ResolveRecordingMBID(ctx, match.RecordingMBID)
		if resolveErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if firstResolveErr == nil {
				firstResolveErr = resolveErr
			}
			continue
		}
		evidence := scanner.MusicRecordingEvidence{
			RecordingMBID: match.RecordingMBID, CanonicalRecordingID: recording.CanonicalID, Title: recording.Title,
			FingerprintScore: match.Score, SourceDuration: int(fingerprint.SourceDurationSecs),
			RecordingDuration: recording.Duration,
		}
		for _, credit := range recording.ArtistCredits {
			if credit.Slug == "" {
				continue
			}
			evidence.Artists = append(evidence.Artists, scanner.MusicRecordingArtistEvidence{
				CanonicalID: credit.Slug, Name: credit.Name, MBID: credit.MBID,
			})
		}
		if strings.TrimSpace(evidence.Title) == "" || evidence.RecordingDuration <= 0 || len(evidence.Artists) == 0 {
			continue
		}
		result = append(result, evidence)
	}
	// One lower-ranked MusicBrainz recording failing to resolve must not erase
	// already-valid, higher-ranked acoustic evidence. If none resolve, retain
	// the first error so transient metadata failures can still be deferred.
	if len(result) == 0 && firstResolveErr != nil {
		return nil, firstResolveErr
	}
	return result, nil
}

func (m *musicFingerprintMatcher) libraryFile(ctx context.Context, relPath string) (sqlc.LibraryFile, error) {
	q := sqlc.New(m.db)
	candidates := make(map[int64]sqlc.LibraryFile, len(m.library.Paths))
	for _, root := range m.library.Paths {
		file, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
			LibraryID: m.library.ID, Path: filepath.Join(root, relPath),
		})
		if err == nil {
			if !file.DeletedAt.Valid {
				candidates[file.ID] = file
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.LibraryFile{}, err
		}
	}
	if len(candidates) == 0 {
		return sqlc.LibraryFile{}, pgx.ErrNoRows
	}
	if len(candidates) != 1 {
		return sqlc.LibraryFile{}, errAmbiguousMusicFingerprintFile
	}
	for _, file := range candidates {
		return file, nil
	}
	panic("unreachable")
}

func (m *musicFingerprintMatcher) lookup(ctx context.Context, q *sqlc.Queries, fingerprint sqlc.LibraryFileFingerprint) ([]acoustid.Match, error) {
	evidenceKey := fingerprintEvidenceKey(fingerprint)
	cached, err := q.GetLibraryFileFingerprintLookup(ctx, sqlc.GetLibraryFileFingerprintLookupParams{
		LibraryFileID: fingerprint.LibraryFileID, Provider: acoustIDLookupProvider,
	})
	if err == nil && cached.EvidenceKey == evidenceKey {
		now := time.Now()
		age := now.Sub(cached.ObservedAt.Time)
		switch {
		case cached.State == "matched" && cached.ObservedAt.Valid && age < acoustIDMatchTTL:
			return decodeAcoustIDMatches(cached.Results)
		case cached.State == "no_match" && cached.ObservedAt.Valid && age < acoustIDNoMatchTTL:
			return nil, nil
		case cached.State == "failed" && cached.RetryAfter.Valid && cached.RetryAfter.Time.After(now):
			if cachedErr := cachedAcoustIDLookupError(cached.ErrorMessage, cached.Results, cached.RetryAfter.Time.Sub(now)); cachedErr != nil {
				return nil, cachedErr
			}
			// Legacy failures predate typed result payloads. Retry them now so
			// old HTTP-400 bad-key rows do not keep poisoning artists after the
			// global key has been corrected.
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	matches, lookupErr := m.acoustID.Lookup(ctx, fingerprint.Fingerprint, int(fingerprint.SourceDurationSecs))
	if lookupErr != nil {
		if errors.Is(lookupErr, context.Canceled) || errors.Is(lookupErr, context.DeadlineExceeded) {
			return nil, lookupErr
		}
		if acoustid.IsConfiguration(lookupErr) {
			// Configuration is global, not fingerprint-specific. Surface it to
			// fail the durable stage and never leave hundreds of per-file cache
			// rows that survive after an operator fixes the application key.
			return nil, lookupErr
		}
		retryDelay := acoustid.ErrorRetryAfter(lookupErr)
		if retryDelay <= 0 {
			retryDelay = acoustIDFailureBackoff
		}
		failureResults, _ := json.Marshal(acoustIDFailureRecord{Class: acoustIDErrorClass(lookupErr)})
		_, _ = q.UpsertLibraryFileFingerprintLookup(context.WithoutCancel(ctx), sqlc.UpsertLibraryFileFingerprintLookupParams{
			LibraryFileID: fingerprint.LibraryFileID, Provider: acoustIDLookupProvider,
			EvidenceKey: evidenceKey, State: "failed", Results: failureResults, ErrorMessage: lookupErr.Error(),
			RetryAfter: pgtype.Timestamptz{Time: time.Now().Add(retryDelay), Valid: true},
		})
		return nil, scannerAcoustIDLookupError(lookupErr, retryDelay)
	}
	body, err := json.Marshal(matches)
	if err != nil {
		return nil, err
	}
	state := "matched"
	if len(matches) == 0 {
		state = "no_match"
	}
	if _, err := q.UpsertLibraryFileFingerprintLookup(ctx, sqlc.UpsertLibraryFileFingerprintLookupParams{
		LibraryFileID: fingerprint.LibraryFileID, Provider: acoustIDLookupProvider,
		EvidenceKey: evidenceKey, State: state, Results: body,
	}); err != nil {
		return nil, err
	}
	return matches, nil
}

type acoustIDFailureRecord struct {
	Class acoustid.ErrorClass `json:"class"`
}

func acoustIDErrorClass(err error) acoustid.ErrorClass {
	switch {
	case acoustid.IsTransient(err):
		return acoustid.ErrorTransient
	case acoustid.IsConfiguration(err):
		return acoustid.ErrorConfiguration
	default:
		return acoustid.ErrorPermanent
	}
}

func scannerAcoustIDLookupError(err error, retryAfter time.Duration) error {
	if !acoustid.IsTransient(err) {
		return err
	}
	return &metadata.DeferredWorkError{
		Operation:  "AcoustID lookup after " + err.Error(),
		RetryAfter: retryAfter,
	}
}

func cachedAcoustIDLookupError(message string, body []byte, retryAfter time.Duration) error {
	var record acoustIDFailureRecord
	_ = json.Unmarshal(body, &record)
	if record.Class == "" {
		return nil
	}
	if strings.TrimSpace(message) == "" {
		message = "cached AcoustID lookup failure"
	}
	err := &acoustid.LookupError{Class: record.Class, Message: message, RetryAfter: retryAfter}
	if record.Class == acoustid.ErrorTransient {
		return scannerAcoustIDLookupError(err, retryAfter)
	}
	return err
}

func fingerprintEvidenceKey(value sqlc.LibraryFileFingerprint) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s", value.Algorithm, value.SourceDurationSecs, value.Fingerprint)))
	return hex.EncodeToString(sum[:])
}

func decodeAcoustIDMatches(body []byte) ([]acoustid.Match, error) {
	var matches []acoustid.Match
	if len(body) == 0 {
		return matches, nil
	}
	if err := json.Unmarshal(body, &matches); err != nil {
		return nil, fmt.Errorf("decode cached AcoustID matches: %w", err)
	}
	return matches, nil
}
