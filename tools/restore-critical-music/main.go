// Command restore-critical-music restores path-keyed analysis data from a
// heya-critical-music-jsonl-v1 export into a rebuilt Heya database.
//
// It is deliberately a dry-run unless --apply is supplied. Existing analysis
// values win over backup values, so rerunning the command is safe.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type options struct {
	exportDir   string
	databaseURL string
	apply       bool
}

type fileRef struct {
	LibraryFileID      int64
	TrackFileID        int64
	TrackID            int64
	AlbumID            int64
	ArtistID           int64
	Size               int64
	ContentHash        string
	AlbumMusicBrainzID string
}

type stats struct {
	Scanned          int           `json:"scanned"`
	PathMatched      int           `json:"path_matched"`
	Matched          int           `json:"matched"`
	Unique           int           `json:"unique"`
	Ambiguous        int           `json:"ambiguous,omitempty"`
	IdentityMismatch int           `json:"identity_mismatch,omitempty"`
	IdentityUnknown  int           `json:"identity_unknown,omitempty"`
	Unlinked         int           `json:"unlinked,omitempty"`
	Applied          int           `json:"applied,omitempty"`
	Samples          []issueSample `json:"samples,omitempty"`
}

type issueSample struct {
	Reason string   `json:"reason"`
	Paths  []string `json:"paths"`
	IDs    []int64  `json:"ids,omitempty"`
}

type fileIdentity struct {
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
}

type nullable[T any] = *T

type trackFileExport struct {
	PathKey     string       `json:"path_key"`
	LibraryFile fileIdentity `json:"library_file"`
	TrackFile   struct {
		IntegratedLUFS          nullable[float64]   `json:"integrated_lufs"`
		TruePeakDB              nullable[float64]   `json:"true_peak_db"`
		LoudnessRangeDB         nullable[float64]   `json:"loudness_range_db"`
		SamplePeakDB            nullable[float64]   `json:"sample_peak_db"`
		LoudnessAnalyzedAt      nullable[time.Time] `json:"loudness_analyzed_at"`
		IntroEndMS              nullable[int32]     `json:"intro_end_ms"`
		OutroStartMS            nullable[int32]     `json:"outro_start_ms"`
		FadeStartMS             nullable[int32]     `json:"fade_start_ms"`
		SilenceStartMS          nullable[int32]     `json:"silence_start_ms"`
		BoundariesAnalyzedAt    nullable[time.Time] `json:"boundaries_analyzed_at"`
		Chromaprint             nullable[string]    `json:"chromaprint"`
		ChromaprintAlgorithm    nullable[int16]     `json:"chromaprint_algorithm"`
		ChromaprintDurationSecs nullable[int32]     `json:"chromaprint_duration_secs"`
		FingerprintedAt         nullable[time.Time] `json:"fingerprinted_at"`
	} `json:"track_file"`
}

type pathExport struct {
	PathKey string `json:"path_key"`
	fileIdentity
}

type facetExport struct {
	Paths []pathExport `json:"paths"`
	Track struct {
		FilePath string `json:"file_path"`
	} `json:"track"`
	Facet struct {
		TrackEmbedding   nullable[string]  `json:"track_embedding"`
		ArtistEmbedding  nullable[string]  `json:"artist_embedding"`
		ReleaseEmbedding nullable[string]  `json:"release_embedding"`
		TextEmbedding    nullable[string]  `json:"text_embedding"`
		BPM              nullable[float32] `json:"bpm"`
		BPMConfidence    nullable[float32] `json:"bpm_confidence"`
		KeyRoot          nullable[int16]   `json:"key_root"`
		KeyMode          nullable[int16]   `json:"key_mode"`
		KeyClarity       nullable[float32] `json:"key_clarity"`
		TopGenres        json.RawMessage   `json:"top_genres"`
		MoodTags         json.RawMessage   `json:"mood_tags"`
		Waveform         json.RawMessage   `json:"waveform"`
		AnalyzedAt       time.Time         `json:"analyzed_at"`
		AnalyzerVersion  int32             `json:"analyzer_version"`
	} `json:"facet"`
}

type albumLoudnessExport struct {
	Paths []string `json:"paths"`
	Album struct {
		MusicBrainzID      string              `json:"musicbrainz_id"`
		IntegratedLUFS     nullable[float64]   `json:"integrated_lufs"`
		TruePeakDB         nullable[float64]   `json:"true_peak_db"`
		LoudnessRangeDB    nullable[float64]   `json:"loudness_range_db"`
		LoudnessAnalyzedAt nullable[time.Time] `json:"loudness_analyzed_at"`
	} `json:"album"`
}

type segmentExport struct {
	PathKey string `json:"path_key"`
	Segment struct {
		SegmentType string    `json:"segment_type"`
		StartMS     int64     `json:"start_ms"`
		EndMS       int64     `json:"end_ms"`
		Source      string    `json:"source"`
		CreatedAt   time.Time `json:"created_at"`
	} `json:"segment"`
	LibraryFile struct {
		fileIdentity
		SegmentsAnalyzedAt nullable[time.Time] `json:"segments_analyzed_at"`
		SegmentsDetectedAt nullable[time.Time] `json:"segments_detected_at"`
	} `json:"library_file"`
}

type rawDecoder interface {
	decodeBackup([]byte) error
}

func (e *trackFileExport) decodeBackup(data []byte) error {
	libraryMarker := []byte(`"library_file"`)
	libraryStart := bytes.Index(data, libraryMarker)
	if libraryStart < 0 {
		return errors.New(`field "library_file" not found`)
	}
	marker := []byte(`"track_file"`)
	start := bytes.Index(data, marker)
	if start < 0 {
		return errors.New(`field "track_file" not found`)
	}
	libraryFile := data[libraryStart:]
	trackFile := data[start:]
	return errors.Join(
		decodeField(data, "path_key", &e.PathKey),
		decodeField(libraryFile, "size", &e.LibraryFile.Size),
		decodeField(libraryFile, "content_hash", &e.LibraryFile.ContentHash),
		decodeField(trackFile, "integrated_lufs", &e.TrackFile.IntegratedLUFS),
		decodeField(trackFile, "true_peak_db", &e.TrackFile.TruePeakDB),
		decodeField(trackFile, "loudness_range_db", &e.TrackFile.LoudnessRangeDB),
		decodeField(trackFile, "sample_peak_db", &e.TrackFile.SamplePeakDB),
		decodeField(trackFile, "loudness_analyzed_at", &e.TrackFile.LoudnessAnalyzedAt),
		decodeField(trackFile, "intro_end_ms", &e.TrackFile.IntroEndMS),
		decodeField(trackFile, "outro_start_ms", &e.TrackFile.OutroStartMS),
		decodeField(trackFile, "fade_start_ms", &e.TrackFile.FadeStartMS),
		decodeField(trackFile, "silence_start_ms", &e.TrackFile.SilenceStartMS),
		decodeField(trackFile, "boundaries_analyzed_at", &e.TrackFile.BoundariesAnalyzedAt),
		decodeField(trackFile, "chromaprint", &e.TrackFile.Chromaprint),
		decodeField(trackFile, "chromaprint_algorithm", &e.TrackFile.ChromaprintAlgorithm),
		decodeField(trackFile, "chromaprint_duration_secs", &e.TrackFile.ChromaprintDurationSecs),
		decodeField(trackFile, "fingerprinted_at", &e.TrackFile.FingerprintedAt),
	)
}

func (e *facetExport) decodeBackup(data []byte) error {
	return errors.Join(
		decodeField(data, "paths", &e.Paths),
		decodeField(data, "file_path", &e.Track.FilePath),
		decodeField(data, "facet", &e.Facet),
	)
}

func (e *albumLoudnessExport) decodeBackup(data []byte) error {
	if err := decodeField(data, "paths", &e.Paths); err != nil {
		return err
	}
	start := bytes.Index(data, []byte(`"album"`))
	if start < 0 {
		return errors.New(`field "album" not found`)
	}
	album := data[start:]
	return errors.Join(
		decodeField(album, "musicbrainz_id", &e.Album.MusicBrainzID),
		decodeField(album, "integrated_lufs", &e.Album.IntegratedLUFS),
		decodeField(album, "true_peak_db", &e.Album.TruePeakDB),
		decodeField(album, "loudness_range_db", &e.Album.LoudnessRangeDB),
		decodeField(album, "loudness_analyzed_at", &e.Album.LoudnessAnalyzedAt),
	)
}

func (e *segmentExport) decodeBackup(data []byte) error {
	if err := decodeField(data, "path_key", &e.PathKey); err != nil {
		return err
	}
	if err := decodeField(data, "segment", &e.Segment); err != nil {
		return err
	}
	start := bytes.Index(data, []byte(`"library_file"`))
	if start < 0 {
		return errors.New(`field "library_file" not found`)
	}
	libraryFile := data[start:]
	return errors.Join(
		decodeField(libraryFile, "size", &e.LibraryFile.Size),
		decodeField(libraryFile, "content_hash", &e.LibraryFile.ContentHash),
		decodeField(libraryFile, "segments_analyzed_at", &e.LibraryFile.SegmentsAnalyzedAt),
		decodeField(libraryFile, "segments_detected_at", &e.LibraryFile.SegmentsDetectedAt),
	)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var opts options
	flag.StringVar(&opts.exportDir, "export-dir", "", "critical-music export directory")
	flag.StringVar(&opts.databaseURL, "database-url", os.Getenv("HEYA_DATABASE_URL"), "PostgreSQL connection string (or HEYA_DATABASE_URL)")
	flag.BoolVar(&opts.apply, "apply", false, "write matched backup data (default is dry-run)")
	flag.Parse()

	if opts.exportDir == "" || opts.databaseURL == "" {
		return errors.New("--export-dir and --database-url (or HEYA_DATABASE_URL) are required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, opts.databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	refs, err := loadCurrentPaths(ctx, conn)
	if err != nil {
		return fmt.Errorf("load current paths: %w", err)
	}
	fmt.Printf("mode=%s current_paths=%d\n", map[bool]string{false: "dry-run", true: "apply"}[opts.apply], len(refs))

	results := make(map[string]stats)
	backupIdentities := make(map[string]fileIdentity, 450000)
	if results["track_files"], err = restoreTrackFiles(ctx, conn, opts, refs, backupIdentities); err != nil {
		return err
	}
	if results["track_facets"], err = restoreFacets(ctx, conn, opts, refs); err != nil {
		return err
	}
	if results["album_loudness"], err = restoreAlbumLoudness(ctx, conn, opts, refs, backupIdentities); err != nil {
		return err
	}
	if results["media_segments"], err = restoreSegments(ctx, conn, opts, refs); err != nil {
		return err
	}
	if opts.apply {
		if err := rebuildCentroids(ctx, conn); err != nil {
			return fmt.Errorf("rebuild centroids: %w", err)
		}
	}

	names := []string{"track_files", "track_facets", "album_loudness", "media_segments"}
	for _, name := range names {
		encoded, _ := json.Marshal(results[name])
		fmt.Printf("%s=%s\n", name, encoded)
	}
	return nil
}

func loadCurrentPaths(ctx context.Context, conn *pgx.Conn) (map[string]fileRef, error) {
	rows, err := conn.Query(ctx, `
		SELECT lf.path, lf.id, COALESCE(tf.id, 0), COALESCE(tf.track_id, 0),
		       COALESCE(t.album_id, 0), COALESCE(a.artist_id, 0), lf.size, lf.content_hash,
		       COALESCE(a.musicbrainz_id, '')
		FROM library_files lf
		LEFT JOIN track_files tf ON tf.library_file_id = lf.id
		LEFT JOIN tracks t ON t.id = tf.track_id
		LEFT JOIN albums a ON a.id = t.album_id
		WHERE lf.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make(map[string]fileRef)
	for rows.Next() {
		var path string
		var ref fileRef
		if err := rows.Scan(&path, &ref.LibraryFileID, &ref.TrackFileID, &ref.TrackID, &ref.AlbumID, &ref.ArtistID,
			&ref.Size, &ref.ContentHash, &ref.AlbumMusicBrainzID); err != nil {
			return nil, err
		}
		refs[path] = ref
	}
	return refs, rows.Err()
}

func openScanner(path string) (*bufio.Scanner, io.Closer, error) {
	f, err := os.Open(path) //nolint:gosec // explicit operator-supplied export path
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	s := bufio.NewScanner(gz)
	s.Buffer(make([]byte, 64*1024), 32*1024*1024)
	return s, closerFunc(func() error {
		return errors.Join(gz.Close(), f.Close())
	}), nil
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func scanFile[T any](path string, fn func(T) error) (int, error) {
	s, closer, err := openScanner(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = closer.Close() }()
	count := 0
	for s.Scan() {
		count++
		var row T
		clean := sanitizeNonFinite(s.Bytes())
		var decodeErr error
		if decoder, ok := any(&row).(rawDecoder); ok {
			decodeErr = decoder.decodeBackup(clean)
		} else {
			decodeErr = json.Unmarshal(clean, &row)
		}
		if decodeErr != nil {
			var syntaxErr *json.SyntaxError
			if errors.As(decodeErr, &syntaxErr) {
				start := max(0, int(syntaxErr.Offset)-80)
				end := min(len(clean), int(syntaxErr.Offset)+80)
				return count, fmt.Errorf("%s line %d near byte %d (%q): %w",
					filepath.Base(path), count, syntaxErr.Offset, clean[start:end], decodeErr)
			}
			return count, fmt.Errorf("%s line %d: %w", filepath.Base(path), count, decodeErr)
		}
		if err := fn(row); err != nil {
			return count, err
		}
		if count%25000 == 0 {
			fmt.Printf("scan dataset=%s rows=%d\n", strings.TrimSuffix(filepath.Base(path), ".jsonl.gz"), count)
		}
	}
	return count, s.Err()
}

// Python's JSON encoder permits NaN and +/-Infinity by default. PostgreSQL
// numeric columns do not, and those values carry no useful restore data, so
// normalize only unquoted occurrences to JSON null. Quoted vector strings and
// metadata are left byte-for-byte intact.
func sanitizeNonFinite(src []byte) []byte {
	var dst []byte
	inString := false
	escaped := false
	for i := 0; i < len(src); {
		b := src[i]
		if inString {
			if escaped {
				if dst != nil {
					dst = append(dst, b)
				}
				escaped = false
			} else if b == '\\' {
				if dst != nil {
					dst = append(dst, b)
				}
				escaped = true
			} else if b == '"' {
				if dst != nil {
					dst = append(dst, b)
				}
				inString = false
			} else if dst != nil {
				dst = append(dst, b)
			}
			i++
			continue
		}
		if b == '"' {
			inString = true
			if dst != nil {
				dst = append(dst, b)
			}
			i++
			continue
		}
		tokenLen := 0
		switch {
		case hasToken(src[i:], "-Infinity"):
			tokenLen = len("-Infinity")
		case hasToken(src[i:], "Infinity"):
			tokenLen = len("Infinity")
		case hasToken(src[i:], "NaN"):
			tokenLen = len("NaN")
		}
		if tokenLen > 0 {
			if dst == nil {
				dst = make([]byte, 0, len(src))
				dst = append(dst, src[:i]...)
			}
			dst = append(dst, "null"...)
			i += tokenLen
			continue
		}
		if dst != nil {
			dst = append(dst, b)
		}
		i++
	}
	if dst == nil {
		return src
	}
	return dst
}

func hasToken(src []byte, token string) bool {
	if len(src) < len(token) || string(src[:len(token)]) != token {
		return false
	}
	if len(src) == len(token) {
		return true
	}
	next := src[len(token)]
	return next == ',' || next == '}' || next == ']' || next == ' ' || next == '\n' || next == '\r' || next == '\t'
}

func decodeField(data []byte, key string, dst any) error {
	value, err := extractValue(data, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(value, dst); err != nil {
		// A few exported strings containing literal quotes were written as
		// \\" instead of \". Retry only the isolated field after collapsing
		// that invalid sequence; unrelated metadata is never touched.
		fixed := bytes.ReplaceAll(value, []byte{'\\', '\\', '"'}, []byte{'\\', '"'})
		if retryErr := json.Unmarshal(fixed, dst); retryErr != nil {
			return fmt.Errorf("decode %q: %w", key, err)
		}
	}
	return nil
}

func extractValue(data []byte, key string) ([]byte, error) {
	needle := []byte(`"` + key + `"`)
	start := bytes.Index(data, needle)
	if start < 0 {
		return nil, fmt.Errorf("field %q not found", key)
	}
	start += len(needle)
	for start < len(data) && (data[start] == ' ' || data[start] == '\t' || data[start] == '\r' || data[start] == '\n') {
		start++
	}
	if start >= len(data) || data[start] != ':' {
		return nil, fmt.Errorf("field %q missing colon", key)
	}
	start++
	for start < len(data) && (data[start] == ' ' || data[start] == '\t' || data[start] == '\r' || data[start] == '\n') {
		start++
	}
	if start >= len(data) {
		return nil, fmt.Errorf("field %q has no value", key)
	}

	switch data[start] {
	case '{', '[':
		open := data[start]
		close := byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(data); i++ {
			b := data[i]
			if inString {
				if escaped {
					escaped = false
				} else if b == '\\' {
					escaped = true
				} else if b == '"' {
					inString = false
				}
				continue
			}
			switch b {
			case '"':
				inString = true
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return data[start : i+1], nil
				}
			}
		}
	case '"':
		escaped := false
		for i := start + 1; i < len(data); i++ {
			if escaped {
				escaped = false
			} else if data[i] == '\\' {
				escaped = true
			} else if data[i] == '"' {
				return data[start : i+1], nil
			}
		}
	default:
		for i := start; i < len(data); i++ {
			switch data[i] {
			case ',', '}', ']':
				return bytes.TrimSpace(data[start:i]), nil
			}
		}
	}
	return nil, fmt.Errorf("field %q has unterminated value", key)
}

func beginStage(ctx context.Context, conn *pgx.Conn, ddl string) (pgx.Tx, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, ddl); err != nil {
		return nil, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, nil
}

func flush(ctx context.Context, tx pgx.Tx, table pgx.Identifier, columns []string, rows *[][]any) error {
	if len(*rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, table, columns, pgx.CopyFromRows(*rows))
	*rows = (*rows)[:0]
	return err
}

type identityResult uint8

const (
	identityUnknown identityResult = iota
	identityMatched
	identityMismatch
)

func compareIdentity(backup fileIdentity, current fileRef) identityResult {
	if backup.ContentHash != "" && current.ContentHash != "" {
		if backup.ContentHash == current.ContentHash {
			return identityMatched
		}
		return identityMismatch
	}
	if backup.Size > 0 && current.Size > 0 {
		if backup.Size == current.Size {
			return identityMatched
		}
		return identityMismatch
	}
	return identityUnknown
}

func addSample(out *stats, reason string, paths []string, ids []int64) {
	const sampleLimit = 5
	count := 0
	for _, sample := range out.Samples {
		if sample.Reason == reason {
			count++
		}
	}
	if count >= sampleLimit {
		return
	}
	if len(paths) > 3 {
		paths = paths[:3]
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out.Samples = append(out.Samples, issueSample{Reason: reason, Paths: paths, IDs: ids})
}

func restoreTrackFiles(
	ctx context.Context,
	conn *pgx.Conn,
	opts options,
	refs map[string]fileRef,
	backupIdentities map[string]fileIdentity,
) (stats, error) {
	var out stats
	seen := make(map[int64]struct{})
	var tx pgx.Tx
	var err error
	if opts.apply {
		tx, err = beginStage(ctx, conn, `CREATE TEMP TABLE restore_track_files (
			track_file_id bigint, integrated_lufs numeric, true_peak_db numeric,
			loudness_range_db numeric, sample_peak_db numeric, loudness_analyzed_at timestamptz,
			intro_end_ms integer, outro_start_ms integer, fade_start_ms integer,
			silence_start_ms integer, boundaries_analyzed_at timestamptz, chromaprint text,
			chromaprint_algorithm smallint, chromaprint_duration_secs integer,
			fingerprinted_at timestamptz) ON COMMIT DROP`)
		if err != nil {
			return out, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
	}
	rows := make([][]any, 0, 500)
	path := filepath.Join(opts.exportDir, "track_files.jsonl.gz")
	out.Scanned, err = scanFile[trackFileExport](path, func(row trackFileExport) error {
		backupIdentities[row.PathKey] = row.LibraryFile
		ref, ok := refs[row.PathKey]
		if !ok {
			return nil
		}
		out.PathMatched++
		if ref.TrackFileID == 0 {
			out.Unlinked++
			addSample(&out, "unlinked", []string{row.PathKey}, nil)
			return nil
		}
		switch compareIdentity(row.LibraryFile, ref) {
		case identityMismatch:
			out.IdentityMismatch++
			addSample(&out, "identity_mismatch", []string{row.PathKey}, nil)
			return nil
		case identityUnknown:
			out.IdentityUnknown++
			addSample(&out, "identity_unknown", []string{row.PathKey}, nil)
			return nil
		}
		out.Matched++
		if _, ok := seen[ref.TrackFileID]; ok {
			return nil
		}
		seen[ref.TrackFileID] = struct{}{}
		out.Unique++
		if !opts.apply {
			return nil
		}
		r := row.TrackFile
		rows = append(rows, []any{ref.TrackFileID, r.IntegratedLUFS, r.TruePeakDB, r.LoudnessRangeDB,
			r.SamplePeakDB, r.LoudnessAnalyzedAt, r.IntroEndMS, r.OutroStartMS, r.FadeStartMS,
			r.SilenceStartMS, r.BoundariesAnalyzedAt, r.Chromaprint, r.ChromaprintAlgorithm,
			r.ChromaprintDurationSecs, r.FingerprintedAt})
		if len(rows) == cap(rows) {
			return flush(ctx, tx, pgx.Identifier{"restore_track_files"}, []string{
				"track_file_id", "integrated_lufs", "true_peak_db", "loudness_range_db", "sample_peak_db",
				"loudness_analyzed_at", "intro_end_ms", "outro_start_ms", "fade_start_ms", "silence_start_ms",
				"boundaries_analyzed_at", "chromaprint", "chromaprint_algorithm", "chromaprint_duration_secs", "fingerprinted_at"}, &rows)
		}
		return nil
	})
	if err != nil || !opts.apply {
		return out, err
	}
	if err = flush(ctx, tx, pgx.Identifier{"restore_track_files"}, []string{
		"track_file_id", "integrated_lufs", "true_peak_db", "loudness_range_db", "sample_peak_db",
		"loudness_analyzed_at", "intro_end_ms", "outro_start_ms", "fade_start_ms", "silence_start_ms",
		"boundaries_analyzed_at", "chromaprint", "chromaprint_algorithm", "chromaprint_duration_secs", "fingerprinted_at"}, &rows); err != nil {
		return out, err
	}
	result, err := tx.Exec(ctx, `UPDATE track_files tf SET
		integrated_lufs=COALESCE(tf.integrated_lufs,s.integrated_lufs),
		true_peak_db=COALESCE(tf.true_peak_db,s.true_peak_db),
		loudness_range_db=COALESCE(tf.loudness_range_db,s.loudness_range_db),
		sample_peak_db=COALESCE(tf.sample_peak_db,s.sample_peak_db),
		loudness_analyzed_at=COALESCE(tf.loudness_analyzed_at,s.loudness_analyzed_at),
		intro_end_ms=COALESCE(tf.intro_end_ms,s.intro_end_ms),
		outro_start_ms=COALESCE(tf.outro_start_ms,s.outro_start_ms),
		fade_start_ms=COALESCE(tf.fade_start_ms,s.fade_start_ms),
		silence_start_ms=COALESCE(tf.silence_start_ms,s.silence_start_ms),
		boundaries_analyzed_at=COALESCE(tf.boundaries_analyzed_at,s.boundaries_analyzed_at),
		chromaprint=COALESCE(tf.chromaprint,s.chromaprint),
		chromaprint_algorithm=COALESCE(tf.chromaprint_algorithm,s.chromaprint_algorithm),
		chromaprint_duration_secs=COALESCE(tf.chromaprint_duration_secs,s.chromaprint_duration_secs),
		fingerprinted_at=COALESCE(tf.fingerprinted_at,s.fingerprinted_at)
	FROM restore_track_files s WHERE tf.id=s.track_file_id AND (
		(tf.integrated_lufs IS NULL AND s.integrated_lufs IS NOT NULL) OR
		(tf.true_peak_db IS NULL AND s.true_peak_db IS NOT NULL) OR
		(tf.loudness_range_db IS NULL AND s.loudness_range_db IS NOT NULL) OR
		(tf.sample_peak_db IS NULL AND s.sample_peak_db IS NOT NULL) OR
		(tf.loudness_analyzed_at IS NULL AND s.loudness_analyzed_at IS NOT NULL) OR
		(tf.intro_end_ms IS NULL AND s.intro_end_ms IS NOT NULL) OR
		(tf.outro_start_ms IS NULL AND s.outro_start_ms IS NOT NULL) OR
		(tf.fade_start_ms IS NULL AND s.fade_start_ms IS NOT NULL) OR
		(tf.silence_start_ms IS NULL AND s.silence_start_ms IS NOT NULL) OR
		(tf.boundaries_analyzed_at IS NULL AND s.boundaries_analyzed_at IS NOT NULL) OR
		(tf.chromaprint IS NULL AND s.chromaprint IS NOT NULL) OR
		(tf.chromaprint_algorithm IS NULL AND s.chromaprint_algorithm IS NOT NULL) OR
		(tf.chromaprint_duration_secs IS NULL AND s.chromaprint_duration_secs IS NOT NULL) OR
		(tf.fingerprinted_at IS NULL AND s.fingerprinted_at IS NOT NULL)
	)`)
	if err != nil {
		return out, err
	}
	out.Applied = int(result.RowsAffected())
	return out, tx.Commit(ctx)
}

type entityMatch struct {
	ID               int64
	PathMatched      bool
	IdentityMatched  bool
	IdentityMismatch bool
	IdentityUnknown  bool
	Unlinked         bool
	IDs              []int64
}

func matchEntity(paths []pathExport, refs map[string]fileRef, pick func(fileRef) int64) entityMatch {
	var result entityMatch
	ids := make(map[int64]struct{})
	for _, path := range paths {
		ref, ok := refs[path.PathKey]
		if !ok {
			continue
		}
		result.PathMatched = true
		switch compareIdentity(path.fileIdentity, ref) {
		case identityMismatch:
			result.IdentityMismatch = true
			continue
		case identityUnknown:
			result.IdentityUnknown = true
			continue
		}
		result.IdentityMatched = true
		value := pick(ref)
		if value == 0 {
			result.Unlinked = true
			continue
		}
		ids[value] = struct{}{}
	}
	result.IDs = make([]int64, 0, len(ids))
	for value := range ids {
		result.IDs = append(result.IDs, value)
	}
	if len(result.IDs) == 1 {
		result.ID = result.IDs[0]
	}
	return result
}

func resolveEntityByPreferredPath(
	result *entityMatch,
	preferredPath string,
	paths []pathExport,
	refs map[string]fileRef,
	pick func(fileRef) int64,
) {
	if result.ID != 0 || preferredPath == "" {
		return
	}
	for _, path := range paths {
		if path.PathKey != preferredPath {
			continue
		}
		ref, ok := refs[path.PathKey]
		if !ok || compareIdentity(path.fileIdentity, ref) != identityMatched {
			return
		}
		preferredID := pick(ref)
		for _, candidateID := range result.IDs {
			if candidateID == preferredID {
				result.ID = preferredID
				return
			}
		}
		return
	}
}

func resolveAlbumByMusicBrainzID(result *entityMatch, mbid string, paths []pathExport, refs map[string]fileRef) {
	if result.ID != 0 || mbid == "" {
		return
	}
	matches := make(map[int64]struct{})
	for _, path := range paths {
		ref, ok := refs[path.PathKey]
		if !ok || compareIdentity(path.fileIdentity, ref) != identityMatched || ref.AlbumMusicBrainzID != mbid {
			continue
		}
		matches[ref.AlbumID] = struct{}{}
	}
	if len(matches) != 1 {
		return
	}
	for albumID := range matches {
		result.ID = albumID
	}
}

func recordEntityMatch(out *stats, result entityMatch, paths []string) {
	if result.PathMatched {
		out.PathMatched++
	}
	if result.IdentityMismatch {
		out.IdentityMismatch++
	}
	if result.IdentityUnknown {
		out.IdentityUnknown++
	}
	if result.Unlinked {
		out.Unlinked++
	}
	if len(result.IDs) > 0 {
		out.Matched++
	}
	if result.ID == 0 && len(result.IDs) > 1 {
		out.Ambiguous++
		addSample(out, "ambiguous", paths, append([]int64(nil), result.IDs...))
	}
}

func restoreFacets(ctx context.Context, conn *pgx.Conn, opts options, refs map[string]fileRef) (stats, error) {
	var out stats
	seen := make(map[int64]struct{})
	var tx pgx.Tx
	var err error
	if opts.apply {
		tx, err = beginStage(ctx, conn, `CREATE TEMP TABLE restore_track_facets (
			track_id bigint, track_embedding text, artist_embedding text, release_embedding text,
			text_embedding text, bpm real, bpm_confidence real, key_root smallint, key_mode smallint,
			key_clarity real, top_genres text, mood_tags text, waveform text,
			analyzed_at timestamptz, analyzer_version integer) ON COMMIT DROP`)
		if err != nil {
			return out, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
	}
	rows := make([][]any, 0, 250)
	path := filepath.Join(opts.exportDir, "track_facets.jsonl.gz")
	out.Scanned, err = scanFile[facetExport](path, func(row facetExport) error {
		paths := make([]string, 0, len(row.Paths))
		for _, p := range row.Paths {
			paths = append(paths, p.PathKey)
		}
		match := matchEntity(row.Paths, refs, func(r fileRef) int64 { return r.TrackID })
		resolveEntityByPreferredPath(&match, row.Track.FilePath, row.Paths, refs, func(r fileRef) int64 { return r.TrackID })
		recordEntityMatch(&out, match, paths)
		if match.ID == 0 {
			return nil
		}
		if _, ok := seen[match.ID]; ok {
			return nil
		}
		seen[match.ID] = struct{}{}
		out.Unique++
		if !opts.apply {
			return nil
		}
		f := row.Facet
		rows = append(rows, []any{match.ID, f.TrackEmbedding, f.ArtistEmbedding, f.ReleaseEmbedding,
			f.TextEmbedding, f.BPM, f.BPMConfidence, f.KeyRoot, f.KeyMode, f.KeyClarity,
			nullableJSON(f.TopGenres), nullableJSON(f.MoodTags), nullableJSON(f.Waveform),
			f.AnalyzedAt, f.AnalyzerVersion})
		if len(rows) == cap(rows) {
			return flushFacetRows(ctx, tx, &rows)
		}
		return nil
	})
	if err != nil || !opts.apply {
		return out, err
	}
	if err := flushFacetRows(ctx, tx, &rows); err != nil {
		return out, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO track_facets (
		track_id, track_embedding, artist_embedding, release_embedding, text_embedding,
		bpm, bpm_confidence, key_root, key_mode, key_clarity, top_genres, mood_tags,
		waveform, analyzed_at, analyzer_version)
	SELECT DISTINCT ON (track_id) track_id,
		track_embedding::vector(512), artist_embedding::vector(512), release_embedding::vector(512),
		text_embedding::vector(512), bpm, bpm_confidence, key_root, key_mode, key_clarity,
		top_genres::jsonb, mood_tags::jsonb, translate(waveform, '[]', '{}')::real[],
		analyzed_at, analyzer_version
	FROM restore_track_facets ORDER BY track_id
	ON CONFLICT (track_id) DO NOTHING`)
	if err != nil {
		return out, err
	}
	out.Applied = int(result.RowsAffected())
	return out, tx.Commit(ctx)
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func flushFacetRows(ctx context.Context, tx pgx.Tx, rows *[][]any) error {
	return flush(ctx, tx, pgx.Identifier{"restore_track_facets"}, []string{
		"track_id", "track_embedding", "artist_embedding", "release_embedding", "text_embedding",
		"bpm", "bpm_confidence", "key_root", "key_mode", "key_clarity", "top_genres", "mood_tags",
		"waveform", "analyzed_at", "analyzer_version"}, rows)
}

func restoreAlbumLoudness(
	ctx context.Context,
	conn *pgx.Conn,
	opts options,
	refs map[string]fileRef,
	backupIdentities map[string]fileIdentity,
) (stats, error) {
	var out stats
	seen := make(map[int64]struct{})
	var tx pgx.Tx
	var err error
	if opts.apply {
		tx, err = beginStage(ctx, conn, `CREATE TEMP TABLE restore_album_loudness (
			album_id bigint, integrated_lufs numeric, true_peak_db numeric,
			loudness_range_db numeric, loudness_analyzed_at timestamptz) ON COMMIT DROP`)
		if err != nil {
			return out, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
	}
	rows := make([][]any, 0, 500)
	path := filepath.Join(opts.exportDir, "album_loudness.jsonl.gz")
	out.Scanned, err = scanFile[albumLoudnessExport](path, func(row albumLoudnessExport) error {
		paths := make([]pathExport, 0, len(row.Paths))
		for _, path := range row.Paths {
			paths = append(paths, pathExport{PathKey: path, fileIdentity: backupIdentities[path]})
		}
		match := matchEntity(paths, refs, func(r fileRef) int64 { return r.AlbumID })
		resolveAlbumByMusicBrainzID(&match, row.Album.MusicBrainzID, paths, refs)
		recordEntityMatch(&out, match, row.Paths)
		if match.ID == 0 {
			return nil
		}
		if _, ok := seen[match.ID]; ok {
			return nil
		}
		seen[match.ID] = struct{}{}
		out.Unique++
		if !opts.apply {
			return nil
		}
		a := row.Album
		rows = append(rows, []any{match.ID, a.IntegratedLUFS, a.TruePeakDB, a.LoudnessRangeDB, a.LoudnessAnalyzedAt})
		if len(rows) == cap(rows) {
			return flush(ctx, tx, pgx.Identifier{"restore_album_loudness"}, []string{
				"album_id", "integrated_lufs", "true_peak_db", "loudness_range_db", "loudness_analyzed_at"}, &rows)
		}
		return nil
	})
	if err != nil || !opts.apply {
		return out, err
	}
	if err := flush(ctx, tx, pgx.Identifier{"restore_album_loudness"}, []string{
		"album_id", "integrated_lufs", "true_peak_db", "loudness_range_db", "loudness_analyzed_at"}, &rows); err != nil {
		return out, err
	}
	result, err := tx.Exec(ctx, `UPDATE albums a SET
		integrated_lufs=COALESCE(a.integrated_lufs,s.integrated_lufs),
		true_peak_db=COALESCE(a.true_peak_db,s.true_peak_db),
		loudness_range_db=COALESCE(a.loudness_range_db,s.loudness_range_db),
		loudness_analyzed_at=COALESCE(a.loudness_analyzed_at,s.loudness_analyzed_at)
	FROM restore_album_loudness s WHERE a.id=s.album_id AND (
		(a.integrated_lufs IS NULL AND s.integrated_lufs IS NOT NULL) OR
		(a.true_peak_db IS NULL AND s.true_peak_db IS NOT NULL) OR
		(a.loudness_range_db IS NULL AND s.loudness_range_db IS NOT NULL) OR
		(a.loudness_analyzed_at IS NULL AND s.loudness_analyzed_at IS NOT NULL)
	)`)
	if err != nil {
		return out, err
	}
	out.Applied = int(result.RowsAffected())
	return out, tx.Commit(ctx)
}

func restoreSegments(ctx context.Context, conn *pgx.Conn, opts options, refs map[string]fileRef) (stats, error) {
	var out stats
	seenFiles := make(map[int64]struct{})
	var tx pgx.Tx
	var err error
	if opts.apply {
		tx, err = beginStage(ctx, conn, `CREATE TEMP TABLE restore_media_segments (
			library_file_id bigint, segment_type text, start_ms bigint, end_ms bigint,
			source text, created_at timestamptz, segments_analyzed_at timestamptz,
			segments_detected_at timestamptz) ON COMMIT DROP`)
		if err != nil {
			return out, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
	}
	rows := make([][]any, 0, 500)
	path := filepath.Join(opts.exportDir, "media_segments.jsonl.gz")
	out.Scanned, err = scanFile[segmentExport](path, func(row segmentExport) error {
		ref, ok := refs[row.PathKey]
		if !ok {
			return nil
		}
		out.PathMatched++
		switch compareIdentity(row.LibraryFile.fileIdentity, ref) {
		case identityMismatch:
			out.IdentityMismatch++
			addSample(&out, "identity_mismatch", []string{row.PathKey}, nil)
			return nil
		case identityUnknown:
			out.IdentityUnknown++
			addSample(&out, "identity_unknown", []string{row.PathKey}, nil)
			return nil
		}
		out.Matched++
		seenFiles[ref.LibraryFileID] = struct{}{}
		if !opts.apply {
			return nil
		}
		s := row.Segment
		rows = append(rows, []any{ref.LibraryFileID, s.SegmentType, s.StartMS, s.EndMS, s.Source,
			s.CreatedAt, row.LibraryFile.SegmentsAnalyzedAt, row.LibraryFile.SegmentsDetectedAt})
		if len(rows) == cap(rows) {
			return flushSegmentRows(ctx, tx, &rows)
		}
		return nil
	})
	out.Unique = len(seenFiles)
	if err != nil || !opts.apply {
		return out, err
	}
	if err := flushSegmentRows(ctx, tx, &rows); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO media_segments (library_file_id, segment_type, start_ms, end_ms, source, created_at)
		SELECT library_file_id, segment_type, start_ms, end_ms, source, created_at
		FROM restore_media_segments WHERE segment_type <> 'commercial'
		ON CONFLICT (library_file_id, segment_type) WHERE segment_type <> 'commercial'
		DO UPDATE SET start_ms=EXCLUDED.start_ms, end_ms=EXCLUDED.end_ms,
			source=EXCLUDED.source, created_at=EXCLUDED.created_at
		WHERE (CASE WHEN media_segments.source='manual' THEN 2
		            WHEN media_segments.source='chromaprint' OR media_segments.source LIKE 'community:%' THEN 1 ELSE 0 END)
		    < (CASE WHEN EXCLUDED.source='manual' THEN 2
		            WHEN EXCLUDED.source='chromaprint' OR EXCLUDED.source LIKE 'community:%' THEN 1 ELSE 0 END)
		RETURNING 1
	), commercials AS (
		INSERT INTO media_segments (library_file_id, segment_type, start_ms, end_ms, source, created_at)
		SELECT s.library_file_id, s.segment_type, s.start_ms, s.end_ms, s.source, s.created_at
		FROM restore_media_segments s
		WHERE s.segment_type='commercial' AND NOT EXISTS (
			SELECT 1 FROM media_segments m WHERE m.library_file_id=s.library_file_id
			AND m.segment_type=s.segment_type AND m.start_ms=s.start_ms AND m.end_ms=s.end_ms AND m.source=s.source)
		RETURNING 1
	), touched AS (
		UPDATE library_files lf SET
			segments_analyzed_at=COALESCE(lf.segments_analyzed_at,s.segments_analyzed_at),
			segments_detected_at=COALESCE(lf.segments_detected_at,s.segments_detected_at)
		FROM (SELECT library_file_id, max(segments_analyzed_at) segments_analyzed_at,
		             max(segments_detected_at) segments_detected_at
		      FROM restore_media_segments GROUP BY library_file_id) s
		WHERE lf.id=s.library_file_id RETURNING 1)
	SELECT (SELECT count(*) FROM inserted)+(SELECT count(*) FROM commercials)`).Scan(&out.Applied)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func flushSegmentRows(ctx context.Context, tx pgx.Tx, rows *[][]any) error {
	return flush(ctx, tx, pgx.Identifier{"restore_media_segments"}, []string{
		"library_file_id", "segment_type", "start_ms", "end_ms", "source", "created_at",
		"segments_analyzed_at", "segments_detected_at"}, rows)
}

func rebuildCentroids(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := []string{
		`INSERT INTO artist_centroids (artist_id, sonic_centroid, text_centroid, track_count, updated_at)
		 SELECT ar.id, AVG(tf.artist_embedding)::vector(512), AVG(tf.text_embedding)::vector(512), count(*)::int, now()
		 FROM artists ar JOIN albums a ON a.artist_id=ar.id JOIN tracks t ON t.album_id=a.id
		 JOIN track_facets tf ON tf.track_id=t.id
		 WHERE tf.artist_embedding IS NOT NULL AND tf.text_embedding IS NOT NULL GROUP BY ar.id
		 ON CONFLICT (artist_id) DO UPDATE SET sonic_centroid=EXCLUDED.sonic_centroid,
		 text_centroid=EXCLUDED.text_centroid, track_count=EXCLUDED.track_count, updated_at=now()`,
		`INSERT INTO album_centroids (album_id, sonic_centroid, text_centroid, track_count, updated_at)
		 SELECT a.id, AVG(tf.release_embedding)::vector(512), AVG(tf.text_embedding)::vector(512), count(*)::int, now()
		 FROM albums a JOIN tracks t ON t.album_id=a.id JOIN track_facets tf ON tf.track_id=t.id
		 WHERE tf.release_embedding IS NOT NULL AND tf.text_embedding IS NOT NULL GROUP BY a.id
		 ON CONFLICT (album_id) DO UPDATE SET sonic_centroid=EXCLUDED.sonic_centroid,
		 text_centroid=EXCLUDED.text_centroid, track_count=EXCLUDED.track_count, updated_at=now()`,
	}
	for _, query := range queries {
		if _, err := tx.Exec(ctx, query); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
