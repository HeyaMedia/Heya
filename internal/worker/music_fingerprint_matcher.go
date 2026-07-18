package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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
	if errors.Is(err, pgx.ErrNoRows) {
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
	result := make([]scanner.MusicRecordingEvidence, 0, min(len(matches), acoustIDResolveLimit))
	for index, match := range matches {
		if index == acoustIDResolveLimit || match.Score < acoustIDMinimumScore {
			break
		}
		recording, resolveErr := m.resolver.ResolveRecordingMBID(ctx, match.RecordingMBID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		evidence := scanner.MusicRecordingEvidence{
			RecordingMBID: match.RecordingMBID, Title: recording.Title,
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
		result = append(result, evidence)
	}
	return result, nil
}

func (m *musicFingerprintMatcher) libraryFile(ctx context.Context, relPath string) (sqlc.LibraryFile, error) {
	q := sqlc.New(m.db)
	for _, root := range m.library.Paths {
		file, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
			LibraryID: m.library.ID, Path: filepath.Join(root, relPath),
		})
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.LibraryFile{}, err
		}
	}
	return sqlc.LibraryFile{}, pgx.ErrNoRows
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
			return nil, nil
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	matches, lookupErr := m.acoustID.Lookup(ctx, fingerprint.Fingerprint, int(fingerprint.SourceDurationSecs))
	if lookupErr != nil {
		_, _ = q.UpsertLibraryFileFingerprintLookup(context.WithoutCancel(ctx), sqlc.UpsertLibraryFileFingerprintLookupParams{
			LibraryFileID: fingerprint.LibraryFileID, Provider: acoustIDLookupProvider,
			EvidenceKey: evidenceKey, State: "failed", Results: []byte("[]"), ErrorMessage: lookupErr.Error(),
			RetryAfter: pgtype.Timestamptz{Time: time.Now().Add(acoustIDFailureBackoff), Valid: true},
		})
		return nil, lookupErr
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
