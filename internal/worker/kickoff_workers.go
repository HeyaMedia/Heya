package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// WatcherPauser is the subset of *watcher.Manager that
// KickoffLibraryScanWorker needs. Letting fsnotify run during a scan
// would race with the scanner's bulk writes; pause/resume bracketing
// avoids that.
type WatcherPauser interface {
	Pause(libraryID int64)
	Resume(libraryID int64)
}

const manualJobMetadata = `{"source":"manual"}`

const (
	scannerProcessTimeout = 30 * time.Minute
	scannerFetchTimeout   = 30 * time.Minute
	scannerApplyTimeout   = 10 * time.Minute
	scannerRichTimeout    = 5 * time.Minute
)

func scannerWorkerError(err error) error {
	var tooLarge *scanner.ArtifactTooLargeError
	if errors.As(err, &tooLarge) {
		return river.JobCancel(err)
	}
	return err
}

type jobSourceMetadata struct {
	Source string `json:"source"`
}

func scheduledJobSource(metadata []byte) string {
	var src jobSourceMetadata
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &src)
	}
	return src.Source
}

func scheduledJobMetadata(source string) []byte {
	if source == queueops.KickoffSourceManual {
		return []byte(manualJobMetadata)
	}
	return nil
}

func scheduledJobInsertOpts(source string) *river.InsertOpts {
	if source == queueops.KickoffSourceManual {
		return &river.InsertOpts{Metadata: []byte(manualJobMetadata)}
	}
	return nil
}

func applyScheduledJobSource(opts river.InsertOpts, source string) *river.InsertOpts {
	if source == queueops.KickoffSourceManual {
		opts.Metadata = []byte(manualJobMetadata)
	}
	return &opts
}

func libraryScanProgressLabel(lib sqlc.Library, scopes []string) string {
	if len(scopes) == 0 {
		return lib.Name
	}
	first := libraryScopeDisplayName(lib, scopes[0])
	if first == "" {
		first = "scoped"
	}
	if len(scopes) == 1 {
		return lib.Name + " · " + first
	}
	return fmt.Sprintf("%s · %s +%d", lib.Name, first, len(scopes)-1)
}

func libraryScopeDisplayName(lib sqlc.Library, scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	scope = strings.TrimRight(scope, `/\`)

	if strings.Contains(scope, "://") {
		for _, root := range lib.Paths {
			root = strings.TrimRight(strings.TrimSpace(root), `/\`)
			if root == "" || !strings.HasPrefix(scope, root+"/") {
				continue
			}
			if rel := strings.TrimPrefix(scope, root+"/"); rel != "" {
				return rel
			}
		}
		return scannerScopeBase(scope)
	}

	cleanScope := filepath.Clean(scope)
	for _, root := range lib.Paths {
		root = strings.TrimSpace(root)
		if root == "" || strings.Contains(root, "://") {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(root), cleanScope)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return rel
	}
	if base := scannerScopeBase(scope); base != "" && base != "." {
		return base
	}
	return scope
}

// ---------------------------------------------------------------------------
// kickoff_library_scan
// ---------------------------------------------------------------------------

// KickoffLibraryScanWorker walks one or all libraries, records which inputs
// changed, and enqueues scanner processing for changed scopes. Unsupported
// domains are deliberately skipped instead of falling back to the legacy
// scanner.
// When args.LibraryID > 0 it scans that single library; otherwise it walks
// every library in the priority order movies → tv → music → books so a fresh DB
// fills predictably for the user's primary media type first.
type KickoffLibraryScanWorker struct {
	river.WorkerDefaults[KickoffLibraryScanArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Hub      EventPublisher
	Watcher  WatcherPauser
	Progress *TaskProgressBroadcaster
}

func (w *KickoffLibraryScanWorker) Work(ctx context.Context, job *river.Job[KickoffLibraryScanArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	source := scheduledJobSource(job.Metadata)
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	var libs []sqlc.Library
	var err error
	if job.Args.LibraryID > 0 {
		lib, gErr := q.GetLibraryByID(ctx, job.Args.LibraryID)
		if gErr != nil {
			finishKickoff(ctx, q, taskID, startedAt, 0, 0, gErr)
			return gErr
		}
		libs = []sqlc.Library{lib}
	} else {
		libs, err = q.ListLibraries(ctx)
		if err != nil {
			finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
			return err
		}
		sortLibrariesByMediaPriority(libs)
	}

	enqueued := 0
	failed := 0

	for _, lib := range libs {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}

		w.Progress.Set("scan_libraries", "kickoff_library_scan", lib.Name)

		if w.Watcher != nil {
			w.Watcher.Pause(lib.ID)
		}
		emit(w.Hub, eventhub.EventScanStarted, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
		})

		result, remainingScopes, inv, scanErr := w.planLibraryScan(ctx, lib, job.Args.Force)

		if w.Watcher != nil {
			w.Watcher.Resume(lib.ID)
		}

		if scanErr != nil {
			log.Error().Err(scanErr).Int64("library_id", lib.ID).Msg("kickoff_library_scan: scan error")
			failed++
			// A cancelled scan leaves the discovered set incomplete, so don't
			// act on partial results. On a partial-root failure (e.g. one
			// removed root), inspectLibraryChanges returns early with the
			// changed scopes it saw but WITHOUT running move detection or the
			// missing-file soft-delete pass — deletions are never derived from
			// an incomplete walk. Fall through so the files that were
			// discovered still get processed.
			if ctx.Err() != nil {
				continue
			}
		}

		n := 0
		processQueued := false
		if supportsScanner(lib.MediaType) && (job.Args.Force || result.New > 0) {
			queued, enqueueFailed := enqueueProcessLibraryScanFanout(ctx, rc, lib, ProcessLibraryScanArgs{
				LibraryID:       lib.ID,
				Force:           job.Args.Force,
				ScheduledTaskID: taskID,
			}, remainingScopes, inv, PriorityScan, source)
			n += queued
			failed += enqueueFailed
			processQueued = processQueued || queued > 0
		}
		// Self-heal files that were matched but never successfully probed (their
		// first ffprobe failed on a flaky mount, and the size+mtime skip means
		// plain rescans never revisit them). ffprobe jobs are unique-while-active,
		// so this can't stack duplicates against probes still in flight.
		reprobed := enqueueReprobeUnprobed(ctx, q, rc, lib.ID, taskID, source)
		enqueued += n + reprobed

		log.Info().
			Int64("library_id", lib.ID).
			Int("discovered", result.Discovered).
			Int("changed", result.New).
			Int("deleted", result.Deleted).
			Int("moved", result.Moved).
			Bool("scanner", supportsScanner(lib.MediaType)).
			Int("enqueued", n).
			Int("reprobed", reprobed).
			Msg("kickoff_library_scan: library done")

		if !processQueued {
			emit(w.Hub, eventhub.EventScanCompleted, eventhub.ScanPayload{
				LibraryID:   lib.ID,
				LibraryName: lib.Name,
				Discovered:  result.Discovered,
				New:         result.New,
				Missing:     result.Deleted,
			})
		}
		if result.Deleted > 0 {
			emit(w.Hub, eventhub.EventMediaRemoved, eventhub.MediaPayload{LibraryID: lib.ID})
		}
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

type libraryScanOutcome struct {
	Discovered int
	New        int
	Deleted    int
	Moved      int
}

// ---------------------------------------------------------------------------
// process_scan
// ---------------------------------------------------------------------------

type ProcessLibraryScanWorker struct {
	river.WorkerDefaults[ProcessLibraryScanArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Hub      EventPublisher
	Watcher  WatcherPauser
	Progress *TaskProgressBroadcaster
}

func (w *ProcessLibraryScanWorker) Work(ctx context.Context, job *river.Job[ProcessLibraryScanArgs]) error {
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	source := scheduledJobSource(job.Metadata)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}
	if !supportsScanner(lib.MediaType) {
		log.Warn().
			Int64("library_id", lib.ID).
			Str("library", lib.Name).
			Str("media_type", string(lib.MediaType)).
			Msg("process_scan: scanner does not support this library type")
		return nil
	}
	if processLibraryScanNeedsOwnerFanout(lib, job.Args.ScopePaths) {
		sink := scanner.NewEventSink(scanner.Event{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			LibraryType: string(lib.MediaType),
			Domain:      string(lib.MediaType),
		})
		inventoryCtx, cancelInventory := context.WithTimeout(ctx, scannerProcessTimeout)
		inv, walkErr := scanner.WalkInventory(inventoryCtx, lib.Paths, sink)
		cancelInventory()
		if walkErr != nil {
			return fmt.Errorf("process_scan: inventory root scope for owner fanout: %w", walkErr)
		}
		queued, failed := enqueueProcessLibraryScanFanout(ctx, rc, lib, job.Args, job.Args.ScopePaths, inv, PriorityScan, source)
		log.Info().
			Int64("library_id", lib.ID).
			Int("owner_scopes", queued).
			Int("enqueue_failed", failed).
			Msg("process_scan: replaced library-root scope with owner fanout")
		if failed > 0 {
			return fmt.Errorf("process_scan: enqueue owner fanout: %d of %d jobs failed", failed, queued+failed)
		}
		return nil
	}

	w.Progress.Set("scan_libraries", "process_scan", libraryScanProgressLabel(lib, job.Args.ScopePaths))

	if w.Watcher != nil {
		w.Watcher.Pause(lib.ID)
		defer w.Watcher.Resume(lib.ID)
	}
	emit(w.Hub, eventhub.EventScanStarted, eventhub.ScanPayload{
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
	})

	scanCtx, cancel := context.WithTimeout(ctx, scannerProcessTimeout)
	defer cancel()
	outcome, result, searchScanRunID, err := w.scanLibrarySearch(scanCtx, lib, job.Args.ScopePaths)
	if err != nil {
		log.Error().Err(err).Int64("library_id", lib.ID).Msg("process_scan: scan error")
		return scannerWorkerError(err)
	}

	entityOpts := scannerSearchOptions(w.DB, w.Heya)
	entityOpts.ScopePaths = job.Args.ScopePaths
	refs, err := scanner.PersistScannerSearchEntities(ctx, w.DB, lib, entityOpts, result, searchScanRunID)
	if err != nil {
		log.Error().Err(err).Int64("library_id", lib.ID).Msg("process_scan: persist scanner entities failed")
		return scannerWorkerError(err)
	}
	// Park files no accepted identity claims so unmatched/needs-review scopes
	// stop re-triggering a live search on every scan. Best-effort: a parking
	// failure just means those files re-detect next scan, which was the old
	// behavior anyway.
	parked, parkErr := scanner.ParkUnmatchedFiles(ctx, w.DB, lib, result)
	if parkErr != nil {
		log.Warn().Err(parkErr).Int64("library_id", lib.ID).Int("parked", parked).Msg("process_scan: park unmatched files failed")
	}
	enqueued := 0
	for _, ref := range refs {
		if !ref.Accepted || ref.ProviderID == "" {
			continue
		}
		if err := enqueueFetchLibraryMetadata(ctx, rc, FetchLibraryMetadataArgs{
			LibraryID:        lib.ID,
			ScopePaths:       job.Args.ScopePaths,
			ScannerEntityID:  ref.Entity.ID,
			SearchArtifactID: ref.Artifact.ID,
			Force:            job.Args.Force,
			ScheduledTaskID:  job.Args.ScheduledTaskID,
		}, PriorityScan, source); err != nil {
			log.Warn().Err(err).Int64("library_id", lib.ID).Int64("scanner_entity_id", ref.Entity.ID).Msg("process_scan: enqueue metadata fetch failed")
			return err
		}
		enqueued++
	}

	log.Info().
		Int64("library_id", lib.ID).
		Int("scopes", len(job.Args.ScopePaths)).
		Int("discovered", outcome.Discovered).
		Int("selected", outcome.New).
		Int("entities", len(refs)).
		Int("parked", parked).
		Int("enqueued_fetch", enqueued).
		Msg("process_scan: library done")
	return nil
}

// ---------------------------------------------------------------------------
// fetch_metadata
// ---------------------------------------------------------------------------

type FetchLibraryMetadataWorker struct {
	river.WorkerDefaults[FetchLibraryMetadataArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Hub      EventPublisher
	Watcher  WatcherPauser
	Progress *TaskProgressBroadcaster
}

func (w *FetchLibraryMetadataWorker) Work(ctx context.Context, job *river.Job[FetchLibraryMetadataArgs]) error {
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	source := scheduledJobSource(job.Metadata)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}
	if !supportsScanner(lib.MediaType) {
		log.Warn().
			Int64("library_id", lib.ID).
			Str("library", lib.Name).
			Str("media_type", string(lib.MediaType)).
			Msg("fetch_metadata: scanner does not support this library type")
		return nil
	}

	w.Progress.Set("scan_libraries", "fetch_metadata", libraryScanProgressLabel(lib, job.Args.ScopePaths))

	if w.Watcher != nil {
		w.Watcher.Pause(lib.ID)
		defer w.Watcher.Resume(lib.ID)
	}

	scanCtx, cancel := context.WithTimeout(ctx, scannerFetchTimeout)
	defer cancel()
	result, fetchScanRunID, metadataArtifactID, err := w.scanLibraryFetch(scanCtx, lib, job.Args.ScopePaths, job.Args.ScannerEntityID, job.Args.SearchArtifactID)
	if err != nil {
		scanner.MarkScannerEntityFailed(ctx, w.DB, job.Args.ScannerEntityID, "metadata_error", err)
		log.Error().Err(err).Int64("library_id", lib.ID).Msg("fetch_metadata: scan error")
		return scannerWorkerError(err)
	}
	if result.New == 0 {
		log.Info().
			Int64("library_id", lib.ID).
			Int64("scanner_entity_id", job.Args.ScannerEntityID).
			Int64("metadata_artifact_id", metadataArtifactID).
			Msg("fetch_metadata: no usable metadata fetched; apply not enqueued")
		return nil
	}

	if err := enqueueApplyLibraryScan(ctx, rc, ApplyLibraryScanArgs{
		LibraryID:          lib.ID,
		ScopePaths:         job.Args.ScopePaths,
		ScannerEntityID:    job.Args.ScannerEntityID,
		MetadataArtifactID: metadataArtifactID,
		Force:              job.Args.Force,
		ScheduledTaskID:    job.Args.ScheduledTaskID,
	}, PriorityScan, source); err != nil {
		log.Warn().Err(err).Int64("library_id", lib.ID).Msg("fetch_metadata: enqueue apply failed")
		return err
	}

	log.Info().
		Int64("library_id", lib.ID).
		Int64("scanner_entity_id", job.Args.ScannerEntityID).
		Int("scopes", len(job.Args.ScopePaths)).
		Int("discovered", result.Discovered).
		Int("fetched", result.New).
		Int64("fetch_scan_run_id", fetchScanRunID).
		Int64("metadata_artifact_id", metadataArtifactID).
		Msg("fetch_metadata: library done")
	return nil
}

// ---------------------------------------------------------------------------
// apply_metadata
// ---------------------------------------------------------------------------

type ApplyLibraryScanWorker struct {
	river.WorkerDefaults[ApplyLibraryScanArgs]
	DB           *pgxpool.Pool
	Heya         *heyamedia.HeyaProvider
	Hub          EventPublisher
	Watcher      WatcherPauser
	SonicEnabled SonicEnabledFn
	Progress     *TaskProgressBroadcaster
}

type ApplyRichMetadataWorker struct {
	river.WorkerDefaults[ApplyRichMetadataArgs]
	DB       *pgxpool.Pool
	Matcher  MatchService
	Hub      EventPublisher
	Progress *TaskProgressBroadcaster
}

func (w *ApplyLibraryScanWorker) Work(ctx context.Context, job *river.Job[ApplyLibraryScanArgs]) error {
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	source := scheduledJobSource(job.Metadata)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}
	if !supportsScanner(lib.MediaType) {
		log.Warn().
			Int64("library_id", lib.ID).
			Str("library", lib.Name).
			Str("media_type", string(lib.MediaType)).
			Msg("apply_metadata: scanner does not support this library type")
		return nil
	}

	w.Progress.Set("scan_libraries", "apply_metadata", libraryScanProgressLabel(lib, job.Args.ScopePaths))

	if w.Watcher != nil {
		w.Watcher.Pause(lib.ID)
		defer w.Watcher.Resume(lib.ID)
	}

	scanCtx, cancel := context.WithTimeout(ctx, scannerApplyTimeout)
	defer cancel()
	outcome, result, err := w.scanLibraryApply(scanCtx, lib, job.Args.ScopePaths, job.Args.ScannerEntityID, job.Args.MetadataArtifactID)
	if err != nil {
		scanner.MarkScannerEntityFailed(ctx, w.DB, job.Args.ScannerEntityID, "apply_error", err)
		log.Error().Err(err).Int64("library_id", lib.ID).Msg("apply_metadata: scan error")
		return scannerWorkerError(err)
	}
	richQueued, richFailed := w.enqueueRichMetadataWork(ctx, rc, lib, result, job.Args.MetadataArtifactID, job.Args.ScannerEntityID, job.Args.ScheduledTaskID, source)
	fanout := w.enqueuePostApplyWork(ctx, q, rc, lib, result, job.Args.ScheduledTaskID, source)
	if richQueued == 0 && richFailed == 0 {
		// Exclude this apply job from the guard — it's still running, and with
		// no rich work queued nothing else will reference the entity's
		// artifacts from this cycle.
		compactAppliedScannerArtifacts(ctx, w.DB, job.Args.ScannerEntityID, job.ID)
	}

	log.Info().
		Int64("library_id", lib.ID).
		Int("scopes", len(job.Args.ScopePaths)).
		Int("discovered", outcome.Discovered).
		Int("applied", outcome.New).
		Int("ratings", fanout.Ratings).
		Int("save_nfo", fanout.SaveNFO).
		Int("save_music_nfo", fanout.SaveMusicNFO).
		Int("ffprobe", fanout.FFProbe).
		Int("trickplay", fanout.Trickplay).
		Int("segments", fanout.Segments).
		Int("thumbnails", fanout.Thumbnails).
		Int("fingerprint", fanout.Fingerprint).
		Int("loudness", fanout.Loudness).
		Int("sonic", fanout.Sonic).
		Int("rich_metadata", richQueued).
		Int("rich_metadata_failed", richFailed).
		Int("fanout_failed", fanout.Failed).
		Msg("apply_metadata: library done")

	emit(w.Hub, eventhub.EventScanCompleted, eventhub.ScanPayload{
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
		Discovered:  outcome.Discovered,
		New:         outcome.New,
	})
	return nil
}

func (w *ApplyLibraryScanWorker) enqueueRichMetadataWork(ctx context.Context, rc *river.Client[pgx.Tx], lib sqlc.Library, result scanner.Result, metadataArtifactID, scannerEntityID int64, taskID string, source string) (queued, failed int) {
	if rc == nil || metadataArtifactID == 0 {
		return 0, 0
	}
	for _, target := range scannerRichMetadataTargets(lib, result) {
		if target.mediaItemID == 0 {
			continue
		}
		args := ApplyRichMetadataArgs{
			LibraryID:          lib.ID,
			MediaItemID:        target.mediaItemID,
			ScannerEntityID:    scannerEntityID,
			MetadataArtifactID: metadataArtifactID,
			MediaKind:          string(target.kind),
			Key:                target.key,
			ScheduledTaskID:    taskID,
		}
		opts := args.InsertOpts()
		opts.Priority = PriorityScan
		if _, err := rc.Insert(ctx, args, applyScheduledJobSource(opts, source)); err != nil {
			log.Warn().Err(err).Int64("library_id", lib.ID).Int64("media_item_id", target.mediaItemID).Msg("apply_metadata: enqueue rich metadata failed")
			failed++
			continue
		}
		queued++
	}
	return queued, failed
}

type scannerRichMetadataTarget struct {
	key         string
	mediaItemID int64
	kind        metadata.MediaKind
}

func scannerRichMetadataTargets(lib sqlc.Library, result scanner.Result) []scannerRichMetadataTarget {
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		out := make([]scannerRichMetadataTarget, 0, len(result.MovieApply))
		for _, item := range result.MovieApply {
			if item.MediaItemID == 0 || item.Action == "failed" || item.Action == "skipped" || item.Action == "blocked" {
				continue
			}
			out = append(out, scannerRichMetadataTarget{key: item.Key, mediaItemID: item.MediaItemID, kind: metadata.KindMovie})
		}
		return out
	case mediatype.IsTVLike(lib.MediaType):
		out := make([]scannerRichMetadataTarget, 0, len(result.TVApply))
		for _, item := range result.TVApply {
			if item.MediaItemID == 0 || item.Action == "failed" || item.Action == "skipped" || item.Action == "blocked" {
				continue
			}
			out = append(out, scannerRichMetadataTarget{key: item.Key, mediaItemID: item.MediaItemID, kind: metadata.KindTV})
		}
		return out
	default:
		return nil
	}
}

func richMetadataDetailForJob(result scanner.Result, args ApplyRichMetadataArgs) (*metadata.MediaDetail, metadata.MediaKind, error) {
	kind := metadata.MediaKind(args.MediaKind)
	switch kind {
	case metadata.KindMovie:
		for _, item := range result.MovieMetadata {
			if item.Detail != nil && (args.Key == "" || item.Key == args.Key) {
				return item.Detail, kind, nil
			}
		}
	case metadata.KindTV:
		for _, item := range result.TVMetadata {
			if item.Detail == nil {
				continue
			}
			if args.Key == "" || item.Key == args.Key || stringSliceContains(item.Keys, args.Key) {
				return item.Detail, kind, nil
			}
		}
	default:
		return nil, kind, fmt.Errorf("apply_rich_metadata unsupported media kind %q", args.MediaKind)
	}
	return nil, kind, fmt.Errorf("apply_rich_metadata metadata detail missing for key %q", args.Key)
}

func stringSliceContains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func (w *ApplyRichMetadataWorker) Work(ctx context.Context, job *river.Job[ApplyRichMetadataArgs]) error {
	q := sqlc.New(w.DB)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}
	w.Progress.Set("scan_libraries", "apply_rich_metadata", libraryScanProgressLabel(lib, nil))
	if w.Matcher == nil {
		return fmt.Errorf("apply_rich_metadata requires matcher")
	}
	if job.Args.MediaItemID == 0 || job.Args.MetadataArtifactID == 0 {
		return fmt.Errorf("apply_rich_metadata requires media_item_id and metadata_artifact_id")
	}

	_, result, err := scanner.LoadScannerEntityArtifactResult(ctx, w.DB, job.Args.MetadataArtifactID)
	if err != nil {
		_ = q.MarkEnrichPartial(ctx, job.Args.MediaItemID)
		// A concurrent apply cycle for the same entity may have compacted this
		// artifact out from under us. It's gone for good, so retrying the load
		// can never succeed — but the applied item can still get its rich
		// side-data straight from the provider. Queue a force-enrich to
		// recover it and cancel this job instead of retry-storming.
		if errors.Is(err, pgx.ErrNoRows) {
			if eqErr := EnqueueEnrichForceTx(ctx, job.Args.MediaItemID, lib.MediaType, EnrichSourceForced); eqErr != nil {
				log.Warn().Err(eqErr).Int64("media_item_id", job.Args.MediaItemID).Msg("apply_rich_metadata: recovery enrich enqueue failed")
			}
			log.Warn().
				Int64("library_id", job.Args.LibraryID).
				Int64("media_item_id", job.Args.MediaItemID).
				Int64("scanner_entity_id", job.Args.ScannerEntityID).
				Int64("metadata_artifact_id", job.Args.MetadataArtifactID).
				Msg("apply_rich_metadata: artifact was compacted; queued force-enrich recovery")
			return river.JobCancel(err)
		}
		return err
	}
	detail, kind, err := richMetadataDetailForJob(result, job.Args)
	if err != nil {
		_ = q.MarkEnrichPartial(ctx, job.Args.MediaItemID)
		return err
	}

	richCtx, cancel := context.WithTimeout(ctx, scannerRichTimeout)
	defer cancel()
	if err := w.Matcher.StoreRichMetadata(richCtx, job.Args.MediaItemID, detail); err != nil {
		markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = q.MarkEnrichPartial(markCtx, job.Args.MediaItemID)
		markCancel()
		log.Warn().
			Err(err).
			Int64("library_id", job.Args.LibraryID).
			Int64("media_item_id", job.Args.MediaItemID).
			Int64("scanner_entity_id", job.Args.ScannerEntityID).
			Str("kind", string(kind)).
			Msg("apply_rich_metadata: rich metadata failed")
		return err
	}
	if err := q.MarkEnrichPeopleDone(ctx, job.Args.MediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichExtrasDone(ctx, job.Args.MediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichComplete(ctx, job.Args.MediaItemID); err != nil {
		return err
	}
	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: job.Args.MediaItemID,
			LibraryID:   job.Args.LibraryID,
			Title:       detail.Title,
			MediaType:   string(lib.MediaType),
		})
	}
	log.Info().
		Int64("library_id", job.Args.LibraryID).
		Int64("media_item_id", job.Args.MediaItemID).
		Int64("scanner_entity_id", job.Args.ScannerEntityID).
		Str("kind", string(kind)).
		Int("cast", len(detail.Cast)).
		Int("crew", len(detail.Crew)).
		Int("keywords", len(detail.Keywords)).
		Int("videos", len(detail.Videos)).
		Msg("apply_rich_metadata: complete")
	compactAppliedScannerArtifacts(ctx, w.DB, job.Args.ScannerEntityID, job.ID)
	return nil
}

func compactAppliedScannerArtifacts(ctx context.Context, db *pgxpool.Pool, scannerEntityID, currentJobID int64) {
	if db == nil || scannerEntityID == 0 {
		return
	}
	// Compaction deletes ALL of the entity's artifacts, so the guard must be
	// entity-scoped AND cover any pipeline job that could still produce or
	// consume an artifact for this entity: a live fetch_metadata/apply_metadata
	// cycle will enqueue a rich job we haven't seen yet, and an already-queued
	// apply_rich_metadata will load one. Guarding on apply_rich_metadata alone
	// left a window — a concurrent apply cycle mid-flight (before it reached
	// the line that enqueues its rich job) would be missed, and the rich job it
	// was about to queue would later fail with "no rows in result set".
	//
	// This is race-free because apply_metadata inserts its rich jobs (immediate
	// commit) before its Work returns, so it stays in a live state until its
	// rich job is visible — there is no instant where neither the apply job nor
	// its rich job is countable.
	busy, err := activeScannerJobsForEntity(ctx, db, scannerEntityID, currentJobID)
	if err != nil {
		log.Warn().
			Err(err).
			Int64("scanner_entity_id", scannerEntityID).
			Msg("scanner artifact compaction skipped: active pipeline job check failed")
		return
	}
	if busy {
		return
	}
	q := sqlc.New(db)
	deleted, err := q.CompactAppliedScannerArtifactsForEntity(ctx, scannerEntityID)
	if err != nil {
		log.Warn().Err(err).Int64("scanner_entity_id", scannerEntityID).Msg("scanner artifact compaction failed")
		return
	}
	if deleted.EntityArtifactsDeleted > 0 || deleted.ScanRunArtifactsDeleted > 0 {
		log.Debug().
			Int64("scanner_entity_id", scannerEntityID).
			Int64("scanner_entity_artifacts", deleted.EntityArtifactsDeleted).
			Int64("scan_run_artifacts", deleted.ScanRunArtifactsDeleted).
			Msg("scanner artifacts compacted")
	}
}

func activeScannerJobsForEntity(ctx context.Context, db *pgxpool.Pool, scannerEntityID, currentJobID int64) (bool, error) {
	var count int64
	err := db.QueryRow(ctx, `
		SELECT count(*)
		FROM river_job
		WHERE kind IN ('fetch_metadata', 'apply_metadata', 'apply_rich_metadata')
		  AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled')
		  AND (args->>'scanner_entity_id')::bigint = $1
		  AND ($2::bigint = 0 OR id <> $2)
	`, scannerEntityID, currentJobID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (w *KickoffLibraryScanWorker) planLibraryScan(ctx context.Context, lib sqlc.Library, force bool) (libraryScanOutcome, []string, scanner.Inventory, error) {
	if supportsScanner(lib.MediaType) {
		return w.inspectLibraryChanges(ctx, lib, force)
	}
	log.Warn().
		Int64("library_id", lib.ID).
		Str("library", lib.Name).
		Str("media_type", string(lib.MediaType)).
		Bool("force", force).
		Msg("kickoff_library_scan: scanner does not support this library type yet; legacy scanner skipped")
	return libraryScanOutcome{}, nil, scanner.Inventory{}, nil
}

func (w *KickoffLibraryScanWorker) inspectLibraryChanges(ctx context.Context, lib sqlc.Library, force bool) (libraryScanOutcome, []string, scanner.Inventory, error) {
	q := sqlc.New(w.DB)
	sink := scanner.NewEventSink(scanner.Event{
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
		LibraryType: string(lib.MediaType),
		Domain:      string(lib.MediaType),
	})
	existingRows, err := q.ListLibraryFilesForScan(ctx, lib.ID)
	if err != nil {
		return libraryScanOutcome{}, nil, scanner.Inventory{}, err
	}

	existingByPath := make(map[string]sqlc.ListLibraryFilesForScanRow, len(existingRows))
	for _, row := range existingRows {
		existingByPath[row.Path] = row
	}

	initialFullScan := force || len(existingRows) == 0
	var outcome libraryScanOutcome
	seen := make(map[string]bool)
	scopeSet := map[string]bool{}
	markChangedScope := func(scope string) {
		if scope == "" || scopeSet[scope] {
			return
		}
		scopeSet[scope] = true
	}

	var newFiles []scanner.InventoryFile
	inv, err := scanner.WalkInventoryWithObserver(ctx, lib.Paths, sink, &scanner.InventoryObserver{
		OnFile: func(file scanner.InventoryFile) {
			if !scannerInventoryFileTracked(file) {
				return
			}
			seen[file.Path] = true
			scope := scannerScopeForInventoryFile(lib.MediaType, file)
			existing, found := existingByPath[file.Path]
			if !found {
				newFiles = append(newFiles, file)
			}
			if initialFullScan {
				markChangedScope(scope)
				return
			}
			if !found || existing.DeletedAt.Valid || libraryFileChanged(existing, file) {
				outcome.New++
				markChangedScope(scope)
			}
		},
	})
	if err != nil {
		outcome.Discovered = countScannerInventoryFiles(inv)
		return outcome, compactScannerScopes(sortedMapKeys(scopeSet)), inv, err
	}

	outcome.Discovered = countScannerInventoryFiles(inv)
	if initialFullScan {
		outcome.New = outcome.Discovered
	}

	nfoScopes, nfoChanges, err := syncLibraryNFODirs(ctx, q, lib.ID, inv)
	if err != nil {
		return outcome, compactScannerScopes(sortedMapKeys(scopeSet)), inv, err
	}
	if !initialFullScan {
		outcome.New += nfoChanges
	}
	for _, scope := range nfoScopes {
		markChangedScope(ScannerScopeForLibraryDirectory(lib, scope))
	}

	// Relocate moves/renames BEFORE the soft-delete pass so a moved file keeps
	// its library_files id — and with it probe data, trickplay, segments,
	// fingerprints, track_files, and file links.
	outcome.Moved = w.relocateMovedFiles(ctx, q, lib, existingRows, seen, newFiles, markChangedScope)

	var missing []string
	for _, row := range existingRows {
		if row.DeletedAt.Valid || seen[row.Path] {
			continue
		}
		missing = append(missing, row.Path)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		if err := q.SoftDeleteLibraryFilesByPath(ctx, sqlc.SoftDeleteLibraryFilesByPathParams{
			LibraryID: lib.ID,
			Column2:   missing,
		}); err != nil {
			return outcome, nil, inv, err
		}
		outcome.Deleted = len(missing)
	}

	return outcome, compactScannerScopes(sortedMapKeys(scopeSet)), inv, nil
}

// scannerFileMove pairs a file that appeared on disk with the known row it
// relocates: same byte size plus a matching basename (move across dirs) or
// µs-truncated mtime (rename in place). Size alone is deliberately not
// enough — a coincidentally same-sized new file would steal the old row's
// identity, probe data, and watch-adjacent state.
type scannerFileMove struct {
	Row  sqlc.ListLibraryFilesForScanRow
	File scanner.InventoryFile
}

// matchMovedFiles pairs new on-disk files against rows whose paths are gone:
// missed by this walk (deleted in this very scan) or already soft-deleted
// within the last 7 days (a move noticed across two scans, e.g. via watcher
// events). Live missing rows win over older soft-deleted ones; basename
// matches win over mtime matches. Pure so the pairing rules are testable
// without a database.
func matchMovedFiles(existingRows []sqlc.ListLibraryFilesForScanRow, seen map[string]bool, newFiles []scanner.InventoryFile) []scannerFileMove {
	if len(newFiles) == 0 {
		return nil
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	bySize := map[int64][]sqlc.ListLibraryFilesForScanRow{}
	for _, row := range existingRows {
		if seen[row.Path] {
			continue
		}
		if row.DeletedAt.Valid && row.DeletedAt.Time.Before(cutoff) {
			continue
		}
		bySize[row.Size] = append(bySize[row.Size], row)
	}
	if len(bySize) == 0 {
		return nil
	}
	for _, bucket := range bySize {
		sort.Slice(bucket, func(i, j int) bool {
			if bucket[i].DeletedAt.Valid != bucket[j].DeletedAt.Valid {
				return !bucket[i].DeletedAt.Valid // live missing rows first
			}
			if bucket[i].DeletedAt.Valid && !bucket[i].DeletedAt.Time.Equal(bucket[j].DeletedAt.Time) {
				return bucket[i].DeletedAt.Time.After(bucket[j].DeletedAt.Time) // newest deletion first
			}
			return bucket[i].Path < bucket[j].Path
		})
	}

	consumed := map[int64]bool{}
	var moves []scannerFileMove
	claim := func(file scanner.InventoryFile, match func(sqlc.ListLibraryFilesForScanRow) bool) bool {
		for _, row := range bySize[file.Size] {
			if consumed[row.ID] || !match(row) {
				continue
			}
			consumed[row.ID] = true
			moves = append(moves, scannerFileMove{Row: row, File: file})
			return true
		}
		return false
	}
	// Basename pass first across ALL new files, so a basename match is never
	// beaten to its row by another file's weaker mtime match.
	remaining := make([]scanner.InventoryFile, 0, len(newFiles))
	for _, file := range newFiles {
		base := filepath.Base(file.Path)
		if !claim(file, func(row sqlc.ListLibraryFilesForScanRow) bool { return filepath.Base(row.Path) == base }) {
			remaining = append(remaining, file)
		}
	}
	for _, file := range remaining {
		if file.MTime.IsZero() {
			continue
		}
		want := file.MTime.Truncate(time.Microsecond)
		claim(file, func(row sqlc.ListLibraryFilesForScanRow) bool {
			return row.Mtime.Valid && row.Mtime.Time.Truncate(time.Microsecond).Equal(want)
		})
	}
	return moves
}

// relocateMovedFiles applies matchMovedFiles pairings: the row keeps its id
// under the new path and escapes the soft-delete pass, while both the old
// and new owner scopes re-enter the pipeline (already marked for the new
// path by the walk observer) — naming carries identity, so a renamed owner
// must re-match and the old owner must re-plan what it lost.
func (w *KickoffLibraryScanWorker) relocateMovedFiles(ctx context.Context, q *sqlc.Queries, lib sqlc.Library, existingRows []sqlc.ListLibraryFilesForScanRow, seen map[string]bool, newFiles []scanner.InventoryFile, markChangedScope func(string)) int {
	moved := 0
	for _, move := range matchMovedFiles(existingRows, seen, newFiles) {
		if err := q.RelocateLibraryFile(ctx, sqlc.RelocateLibraryFileParams{
			ID:    move.Row.ID,
			Path:  move.File.Path,
			Mtime: pgtype.Timestamptz{Time: move.File.MTime, Valid: !move.File.MTime.IsZero()},
		}); err != nil {
			log.Warn().Err(err).Int64("file_id", move.Row.ID).Str("from", move.Row.Path).Str("to", move.File.Path).Msg("kickoff_library_scan: relocate moved file failed")
			continue
		}
		seen[move.Row.Path] = true // old path escapes the soft-delete pass
		markChangedScope(ScannerScopeForLibraryPath(lib, move.Row.Path))
		log.Info().Int64("file_id", move.Row.ID).Str("from", move.Row.Path).Str("to", move.File.Path).Msg("kickoff_library_scan: detected file move")
		moved++
	}
	return moved
}

type scannerNFODirState struct {
	DirPath string
	NFOName string
	MTime   pgtype.Timestamptz
}

func syncLibraryNFODirs(ctx context.Context, q *sqlc.Queries, libraryID int64, inv scanner.Inventory) ([]string, int, error) {
	current := map[string]scannerNFODirState{}
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if file.Class != scanner.ClassNFO {
				continue
			}
			dir := scannerScopeForPath(file.Path)
			if dir == "" {
				continue
			}
			if _, exists := current[dir]; exists {
				continue
			}
			current[dir] = scannerNFODirState{
				DirPath: dir,
				NFOName: file.Name,
				MTime: pgtype.Timestamptz{
					Time:  file.MTime,
					Valid: !file.MTime.IsZero(),
				},
			}
		}
	}

	rows, err := q.ListLibraryNFODirs(ctx, libraryID)
	if err != nil {
		return nil, 0, err
	}
	existing := make(map[string]sqlc.ListLibraryNFODirsRow, len(rows))
	for _, row := range rows {
		existing[row.DirPath] = row
	}

	scopeSet := map[string]bool{}
	changes := 0
	for dir, state := range current {
		row, found := existing[dir]
		if !found || row.NfoName != state.NFOName || timestamptzChanged(row.Mtime, state.MTime) {
			scopeSet[dir] = true
			changes++
		}
		if err := q.UpsertLibraryNFODir(ctx, sqlc.UpsertLibraryNFODirParams{
			LibraryID: libraryID,
			DirPath:   state.DirPath,
			NfoName:   state.NFOName,
			Mtime:     state.MTime,
		}); err != nil {
			return nil, 0, err
		}
	}

	var removed []string
	for dir := range existing {
		if _, ok := current[dir]; ok {
			continue
		}
		removed = append(removed, dir)
		scopeSet[dir] = true
		changes++
	}
	sort.Strings(removed)
	if len(removed) > 0 {
		if err := q.DeleteLibraryNFODirs(ctx, sqlc.DeleteLibraryNFODirsParams{
			LibraryID: libraryID,
			Column2:   removed,
		}); err != nil {
			return nil, 0, err
		}
	}
	return sortedMapKeys(scopeSet), changes, nil
}

func scannerInventoryFileTracked(file scanner.InventoryFile) bool {
	return file.Class == scanner.ClassPrimaryMedia || file.Class == scanner.ClassExtraMedia
}

// libraryFileChanged compares µs-truncated mtimes: Postgres timestamptz
// stores microseconds, while a fresh os.Stat carries nanoseconds on
// APFS/ext4/ZFS. Comparing untruncated instants means the round-tripped DB
// value never equals the stat value, so every file reads as "changed" on
// every scan and the incremental skip never engages.
func libraryFileChanged(row sqlc.ListLibraryFilesForScanRow, file scanner.InventoryFile) bool {
	if row.Size != file.Size {
		return true
	}
	if row.Mtime.Valid != !file.MTime.IsZero() {
		return true
	}
	if row.Mtime.Valid && !row.Mtime.Time.Truncate(time.Microsecond).Equal(file.MTime.Truncate(time.Microsecond)) {
		return true
	}
	return false
}

// timestamptzChanged compares µs-truncated for the same reason as
// libraryFileChanged: one side has been through Postgres, the other is a
// fresh stat.
func timestamptzChanged(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid {
		return true
	}
	if a.Valid && !a.Time.Truncate(time.Microsecond).Equal(b.Time.Truncate(time.Microsecond)) {
		return true
	}
	return false
}

func sortedMapKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func compactScannerScopes(scopes []string) []string {
	var out []string
	for _, scope := range scopes {
		if scope == "" {
			continue
		}
		covered := false
		for _, parent := range out {
			if scannerScopeContains(parent, scope) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, scope)
		}
	}
	return out
}

func scannerScopeContains(parent, child string) bool {
	parent = strings.TrimSpace(parent)
	child = strings.TrimSpace(child)
	if parent == "" || child == "" {
		return false
	}
	if strings.Contains(parent, "://") || strings.Contains(child, "://") {
		parent = strings.TrimRight(parent, "/")
		child = strings.TrimRight(child, "/")
		return parent == child || strings.HasPrefix(child, parent+"/")
	}
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

var scannerSeasonDirRE = regexp.MustCompile(`(?i)^(?:Season|Series|S)[ ._-]*(?:\d{1,2}|specials?)$`)

func scannerScopeForInventoryFile(mediaType sqlc.MediaType, file scanner.InventoryFile) string {
	if mediaType == sqlc.MediaTypeMusic {
		if scope := scannerMusicArtistScopeForRootRel(file.Root, file.RelPath, file.Path); scope != "" {
			return scope
		}
	}
	if file.Class == scanner.ClassPrimaryMedia && filepath.Dir(file.RelPath) == "." {
		return file.Path
	}
	return ScannerScopeForPath(mediaType, file.Path)
}

func ScannerScopeForPath(mediaType sqlc.MediaType, path string) string {
	return scannerOwnerScope(mediaType, scannerScopeForPath(path))
}

func ScannerScopeForLibraryPath(lib sqlc.Library, path string) string {
	if lib.MediaType == sqlc.MediaTypeMusic {
		if scope := scannerMusicArtistScopeForLibraryPath(lib.Paths, path); scope != "" {
			return scope
		}
	}
	return ScannerScopeForPath(lib.MediaType, path)
}

// ScannerScopeForLibraryDirectory resolves a directory that is already known
// to contain scanner-owned data. Unlike ScannerScopeForLibraryPath it does not
// strip another path component before promoting season/extras directories to
// their movie or show owner.
func ScannerScopeForLibraryDirectory(lib sqlc.Library, path string) string {
	if lib.MediaType == sqlc.MediaTypeMusic {
		if scope := scannerMusicArtistScopeForLibraryPath(lib.Paths, path); scope != "" {
			return scope
		}
	}
	return scannerOwnerScope(lib.MediaType, strings.TrimRight(strings.TrimSpace(path), `/\`))
}

func scannerMusicArtistScopeForLibraryPath(roots []string, path string) string {
	for _, root := range roots {
		relPath, ok := scannerRelPath(root, path)
		if !ok {
			continue
		}
		return scannerMusicArtistScopeForRootRel(root, relPath, path)
	}
	return ""
}

func scannerMusicArtistScopeForRootRel(root, relPath, fallbackPath string) string {
	parts := scannerRelPathParts(relPath)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return fallbackPath
	}
	return scannerJoinScopePath(root, parts[0])
}

func scannerRelPath(root, path string) (string, bool) {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	if root == "" || path == "" {
		return "", false
	}
	if strings.Contains(root, "://") || strings.Contains(path, "://") {
		root = strings.TrimRight(root, "/")
		path = strings.TrimRight(path, "/")
		if path == root {
			return ".", true
		}
		prefix := root + "/"
		if !strings.HasPrefix(path, prefix) {
			return "", false
		}
		return strings.TrimPrefix(path, prefix), true
	}
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relPath, true
}

func scannerRelPathParts(relPath string) []string {
	relPath = strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	relPath = strings.Trim(relPath, "/")
	if relPath == "" || relPath == "." {
		return nil
	}
	rawParts := strings.Split(relPath, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func scannerJoinScopePath(root, child string) string {
	root = strings.TrimSpace(root)
	child = strings.Trim(child, "/\\")
	if root == "" || child == "" {
		return ""
	}
	if strings.Contains(root, "://") {
		return strings.TrimRight(root, "/") + "/" + child
	}
	return filepath.Join(root, child)
}

func scannerOwnerScope(mediaType sqlc.MediaType, scope string) string {
	for {
		base := scannerScopeBase(scope)
		if (scannerMediaTypeUsesExtrasDirs(mediaType) && mediafile.IsExtrasDir(base)) || (mediatype.IsTVLike(mediaType) && scannerSeasonDirRE.MatchString(base)) {
			parent := scannerScopeParent(scope)
			if parent == "" || parent == scope {
				return scope
			}
			scope = parent
			continue
		}
		return scope
	}
}

func scannerMediaTypeUsesExtrasDirs(mediaType sqlc.MediaType) bool {
	return mediaType == sqlc.MediaTypeMovie || mediatype.IsTVLike(mediaType)
}

func scannerScopeBase(scope string) string {
	scope = strings.TrimRight(strings.TrimSpace(scope), "/")
	if scope == "" {
		return ""
	}
	if strings.Contains(scope, "://") {
		if idx := strings.LastIndex(scope, "/"); idx >= 0 {
			return scope[idx+1:]
		}
		return scope
	}
	return filepath.Base(scope)
}

func scannerScopeParent(scope string) string {
	scope = strings.TrimRight(strings.TrimSpace(scope), "/")
	if scope == "" {
		return ""
	}
	if strings.Contains(scope, "://") {
		idx := strings.LastIndex(scope, "/")
		if idx <= strings.Index(scope, "://")+2 {
			return scope
		}
		return scope[:idx]
	}
	return filepath.Dir(scope)
}

func scannerScopeForPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.Contains(path, "://") {
		path = strings.TrimRight(path, "/")
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			return path[:idx]
		}
		return path
	}
	return filepath.Dir(path)
}

func enqueueProcessLibraryScan(ctx context.Context, rc *river.Client[pgx.Tx], args ProcessLibraryScanArgs, priority int, source string) error {
	if rc == nil {
		return fmt.Errorf("river client unavailable")
	}
	opts := args.InsertOpts()
	opts.Priority = priority
	_, err := rc.Insert(ctx, args, applyScheduledJobSource(opts, source))
	return err
}

func enqueueProcessLibraryScanFanout(ctx context.Context, rc *river.Client[pgx.Tx], lib sqlc.Library, base ProcessLibraryScanArgs, scopes []string, inv scanner.Inventory, priority int, source string) (queued, failed int) {
	argsList := processLibraryScanFanoutArgs(lib, base, scopes, inv)
	for _, args := range argsList {
		if err := ctx.Err(); err != nil {
			log.Warn().Err(err).Int64("library_id", base.LibraryID).Msg("kickoff_library_scan: enqueue scanner processing canceled")
			return queued, failed + 1
		}
		if err := enqueueProcessLibraryScan(ctx, rc, args, priority, source); err != nil {
			log.Warn().Err(err).Int64("library_id", base.LibraryID).Strs("scopes", args.ScopePaths).Msg("kickoff_library_scan: enqueue scanner processing failed")
			failed++
			continue
		}
		queued++
	}
	return queued, failed
}

// musicScanScopeChunk caps how many artist directories one music
// process_scan job analyzes. The analyze phase ffprobes every audio file
// under its scopes serially, so an unbounded batch (a whole-library pass)
// cannot finish inside scannerProcessTimeout on a real library — it times
// out, each retry restarts the probe sweep from scratch, and while the job
// sits running/retryable its unique args block every subsequent scan's
// insert. Chunks keep per-job work bounded and retryable, and let the
// process_scan workers run chunks in parallel.
const musicScanScopeChunk = 24

func processLibraryScanFanoutArgs(lib sqlc.Library, base ProcessLibraryScanArgs, scopes []string, inv scanner.Inventory) []ProcessLibraryScanArgs {
	// Music is deliberately batched at the local scan boundary, in bounded
	// chunks of artist directories. One analysis pass per chunk discovers its
	// changed artists, after which PersistScannerSearchEntities creates one
	// narrow artifact per artist and fetch_metadata/apply_metadata provide
	// the remote-work fanout. Per-artist splitting here would repeatedly
	// open the same library before reaching the artist-sized metadata work.
	if lib.MediaType == sqlc.MediaTypeMusic {
		requested := compactScannerScopes(scopes)
		if len(requested) == 0 || scannerScopesCoverInventoryOwners(lib, requested, inv) {
			// Full/forced scan (or changed scopes that already span every
			// owner): chunk the actual owner list from the inventory instead
			// of one unbounded whole-library pass.
			requested = scannerOwnerScopesFromInventory(lib, inv)
		}
		sort.Strings(requested)
		out := make([]ProcessLibraryScanArgs, 0, (len(requested)+musicScanScopeChunk-1)/musicScanScopeChunk)
		for start := 0; start < len(requested); start += musicScanScopeChunk {
			args := base
			args.ScopePaths = requested[start:min(start+musicScanScopeChunk, len(requested))]
			out = append(out, args)
		}
		return out
	}

	if !scannerMediaTypeRequiresOwnerFanout(lib.MediaType) {
		if len(scopes) == 0 {
			return []ProcessLibraryScanArgs{base}
		}
		out := make([]ProcessLibraryScanArgs, 0, len(scopes))
		for _, scope := range compactScannerScopes(scopes) {
			args := base
			args.ScopePaths = []string{scope}
			out = append(out, args)
		}
		return out
	}

	ownerScopes := scannerOwnerScopesFromInventory(lib, inv)
	requested := compactScannerScopes(scopes)
	if len(requested) == 0 {
		requested = ownerScopes
	}
	set := map[string]bool{}
	for _, scope := range requested {
		if scannerScopeIsLibraryRoot(lib, scope) {
			for _, owner := range ownerScopes {
				if scannerScopeContains(scope, owner) {
					set[owner] = true
				}
			}
			continue
		}
		set[scope] = true
	}

	out := make([]ProcessLibraryScanArgs, 0, len(set))
	for _, scope := range compactScannerScopes(sortedMapKeys(set)) {
		if scannerScopeIsLibraryRoot(lib, scope) {
			continue
		}
		args := base
		args.ScopePaths = []string{scope}
		out = append(out, args)
	}
	return out
}

func scannerScopesCoverInventoryOwners(lib sqlc.Library, scopes []string, inv scanner.Inventory) bool {
	owners := scannerOwnerScopesFromInventory(lib, inv)
	if len(owners) == 0 {
		return false
	}
	for _, owner := range owners {
		covered := false
		for _, scope := range scopes {
			if scannerScopeContains(scope, owner) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func processLibraryScanNeedsOwnerFanout(lib sqlc.Library, scopes []string) bool {
	// Music re-fans out too: an empty or library-root scope would otherwise
	// run one unbounded whole-library analysis (see musicScanScopeChunk).
	// This also converts stale nil-scope music jobs from older deploys into
	// bounded chunks instead of letting them wedge on the timeout.
	if !scannerMediaTypeRequiresOwnerFanout(lib.MediaType) && lib.MediaType != sqlc.MediaTypeMusic {
		return false
	}
	if len(scopes) == 0 {
		return true
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" {
			return true
		}
		if scannerScopeIsLibraryRoot(lib, scope) {
			return true
		}
	}
	return false
}

func scannerMediaTypeRequiresOwnerFanout(mediaType sqlc.MediaType) bool {
	return mediaType == sqlc.MediaTypeMovie || mediatype.IsTVLike(mediaType)
}

func scannerOwnerScopesFromInventory(lib sqlc.Library, inv scanner.Inventory) []string {
	set := map[string]bool{}
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if !scannerInventoryFileTracked(file) {
				continue
			}
			scope := scannerScopeForInventoryFile(lib.MediaType, file)
			if scope != "" && !scannerScopeIsLibraryRoot(lib, scope) {
				set[scope] = true
			}
		}
	}
	return compactScannerScopes(sortedMapKeys(set))
}

func scannerScopeIsLibraryRoot(lib sqlc.Library, scope string) bool {
	for _, root := range lib.Paths {
		relPath, ok := scannerRelPath(root, scope)
		if ok && relPath == "." {
			return true
		}
	}
	return false
}

func enqueueApplyLibraryScan(ctx context.Context, rc *river.Client[pgx.Tx], args ApplyLibraryScanArgs, priority int, source string) error {
	if rc == nil {
		return fmt.Errorf("river client unavailable")
	}
	opts := args.InsertOpts()
	opts.Priority = priority
	_, err := rc.Insert(ctx, args, applyScheduledJobSource(opts, source))
	return err
}

func enqueueFetchLibraryMetadata(ctx context.Context, rc *river.Client[pgx.Tx], args FetchLibraryMetadataArgs, priority int, source string) error {
	if rc == nil {
		return fmt.Errorf("river client unavailable")
	}
	opts := args.InsertOpts()
	opts.Priority = priority
	_, err := rc.Insert(ctx, args, applyScheduledJobSource(opts, source))
	return err
}

func (w *ProcessLibraryScanWorker) scanLibrarySearch(ctx context.Context, lib sqlc.Library, scopePaths []string) (libraryScanOutcome, scanner.Result, int64, error) {
	opts := scannerSearchOptions(w.DB, w.Heya)
	opts.ScopePaths = scopePaths
	opts.EventWriters = []scanner.EventWriter{newScannerEventBridge(w.Hub, "process_scan")}
	run := scanner.NewLibraryRun(lib, opts, io.Discard)
	if err := run.Run(ctx, scanner.PhasesForOptions(opts)...); err != nil {
		result := run.Result()
		return libraryScanOutcome{Discovered: countScannerInventoryFiles(result.Inventory)}, result, 0, err
	}
	result, err := run.Finish(ctx)
	return libraryScanOutcome{
		Discovered: countScannerInventoryFiles(result.Inventory),
		New:        countScannerAcceptedSearch(result),
	}, result, run.ScanRunID(), err
}

func (w *FetchLibraryMetadataWorker) scanLibraryFetch(ctx context.Context, lib sqlc.Library, scopePaths []string, entityID, searchArtifactID int64) (libraryScanOutcome, int64, int64, error) {
	if entityID == 0 || searchArtifactID == 0 {
		return libraryScanOutcome{}, 0, 0, fmt.Errorf("fetch_metadata requires scanner_entity_id and search_artifact_id")
	}
	opts := scannerFetchOptions(w.DB, w.Heya)
	opts.ScopePaths = scopePaths
	opts.EventWriters = []scanner.EventWriter{newScannerEventBridge(w.Hub, "fetch_metadata")}
	run := scanner.NewLibraryRun(lib, opts, io.Discard)
	if _, err := sqlc.New(w.DB).MarkScannerEntityFetching(ctx, entityID); err != nil {
		return libraryScanOutcome{}, 0, 0, fmt.Errorf("mark scanner entity fetching: %w", err)
	}
	_, result, err := scanner.LoadScannerEntityArtifactResult(ctx, w.DB, searchArtifactID)
	if err != nil {
		return libraryScanOutcome{}, 0, 0, err
	}
	if err := run.ResumeSearchResult(ctx, result, searchArtifactID); err != nil {
		return libraryScanOutcome{}, 0, 0, err
	}
	if err := run.Run(ctx, scanner.PhaseFetch); err != nil {
		result := run.Result()
		return libraryScanOutcome{Discovered: countScannerInventoryFiles(result.Inventory)}, 0, 0, err
	}
	result, err = run.Finish(ctx)
	if err != nil {
		return libraryScanOutcome{}, run.ScanRunID(), 0, err
	}
	artifact, err := scanner.PersistScannerFetchEntity(ctx, w.DB, entityID, result, run.ScanRunID())
	if err != nil {
		return libraryScanOutcome{}, run.ScanRunID(), 0, err
	}
	return libraryScanOutcome{
		Discovered: countScannerInventoryFiles(result.Inventory),
		New:        countScannerFetchedMetadata(result),
	}, run.ScanRunID(), artifact.ID, nil
}

func (w *ApplyLibraryScanWorker) scanLibraryApply(ctx context.Context, lib sqlc.Library, scopePaths []string, entityID, metadataArtifactID int64) (libraryScanOutcome, scanner.Result, error) {
	if entityID == 0 || metadataArtifactID == 0 {
		return libraryScanOutcome{}, scanner.Result{}, fmt.Errorf("apply_metadata requires scanner_entity_id and metadata_artifact_id")
	}
	opts := scannerApplyOptions(w.DB, w.Heya)
	opts.ScopePaths = scopePaths
	opts.EventWriters = []scanner.EventWriter{newScannerEventBridge(w.Hub, "apply_metadata")}
	run := scanner.NewLibraryRun(lib, opts, io.Discard)
	if _, err := sqlc.New(w.DB).MarkScannerEntityApplying(ctx, entityID); err != nil {
		return libraryScanOutcome{}, scanner.Result{}, fmt.Errorf("mark scanner entity applying: %w", err)
	}
	_, result, err := scanner.LoadScannerEntityArtifactResult(ctx, w.DB, metadataArtifactID)
	if err != nil {
		return libraryScanOutcome{}, scanner.Result{}, err
	}
	resumed, err := run.ResumeFetchResult(ctx, result, metadataArtifactID)
	if err != nil {
		return libraryScanOutcome{}, scanner.Result{}, err
	}
	if !resumed {
		return libraryScanOutcome{}, scanner.Result{}, fmt.Errorf("metadata artifact %d is stale for current search decision", metadataArtifactID)
	}
	if err := run.Run(ctx, scanner.PhaseMaterialize, scanner.PhaseApply); err != nil {
		result := run.Result()
		return libraryScanOutcome{Discovered: countScannerInventoryFiles(result.Inventory)}, result, err
	}
	result, err = run.Finish(ctx)
	if err != nil {
		return libraryScanOutcome{}, result, err
	}
	if _, err := scanner.PersistScannerApplyEntity(ctx, w.DB, entityID, result, run.ScanRunID()); err != nil {
		return libraryScanOutcome{}, result, err
	}
	return libraryScanOutcome{
		Discovered: countScannerInventoryFiles(result.Inventory),
		New:        countScannerAppliedFiles(result),
	}, result, nil
}

type postApplyFanout struct {
	Files        int
	Ratings      int
	SaveNFO      int
	SaveMusicNFO int
	FFProbe      int
	Trickplay    int
	Segments     int
	Thumbnails   int
	Fingerprint  int
	Loudness     int
	Sonic        int
	Skipped      int
	Failed       int
}

func (w *ApplyLibraryScanWorker) enqueuePostApplyWork(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], lib sqlc.Library, result scanner.Result, taskID string, source string) postApplyFanout {
	var fanout postApplyFanout
	if rc == nil {
		return fanout
	}
	settings := metadata.ParseSettings(lib.Settings)
	mediaItemIDs := map[int64]bool{}
	saveNFOQueued := map[int64]bool{}
	saveMusicNFOQueued := map[int64]bool{}
	trickplayQueued := map[int64]bool{}
	segmentsQueued := map[int64]bool{}
	for _, path := range scannerInventoryPostApplyPaths(result.Inventory) {
		if err := ctx.Err(); err != nil {
			return fanout
		}
		file, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
			LibraryID: lib.ID,
			Path:      path,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			log.Warn().Err(err).Int64("library_id", lib.ID).Str("path", path).Msg("apply_metadata: post-apply file lookup failed")
			fanout.Failed++
			continue
		}
		if file.DeletedAt.Valid {
			continue
		}
		fanout.Files++

		links, err := q.ListLibraryFileLinksByFile(ctx, file.ID)
		if err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("apply_metadata: file link lookup failed")
			fanout.Failed++
			continue
		}
		for _, link := range links {
			mediaItemIDs[link.MediaItemID] = true
			if link.RelationType == "extra" {
				if link.ThumbnailPath == "" {
					if res, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: link.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
						log.Warn().Err(err).Int64("extra_id", link.ID).Msg("apply_metadata: enqueue extra thumbnail failed")
						fanout.Failed++
					} else if res.UniqueSkippedAsDuplicate {
						fanout.Skipped++
					} else {
						fanout.Thumbnails++
					}
				}
				continue
			}
			if settings.SaveNFO && scannerMediaTypeWritesVideoNFO(lib.MediaType) && !saveNFOQueued[link.MediaItemID] {
				if res, err := rc.Insert(ctx, SaveNFOArgs{
					MediaItemID:   link.MediaItemID,
					LibraryFileID: file.ID,
					FilePath:      file.Path,
					MediaType:     string(lib.MediaType),
				}, nil); err != nil {
					log.Warn().Err(err).Int64("media_item_id", link.MediaItemID).Msg("apply_metadata: enqueue save nfo failed")
					fanout.Failed++
				} else if res.UniqueSkippedAsDuplicate {
					fanout.Skipped++
				} else {
					fanout.SaveNFO++
				}
				saveNFOQueued[link.MediaItemID] = true
			}
		}

		probeable := mediafile.IsProbeable(file.Path)
		needsProbe := probeable && libraryFileNeedsProbe(file)
		if needsProbe {
			if res, err := rc.Insert(ctx, FFProbeArgs{
				LibraryFileID:   file.ID,
				FilePath:        file.Path,
				ScheduledTaskID: taskID,
			}, scheduledJobInsertOpts(source)); err != nil {
				log.Warn().Err(err).Int64("file_id", file.ID).Msg("apply_metadata: enqueue ffprobe failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.FFProbe++
			}
		}
		if probeable && !needsProbe && libraryFileHasVideo(file) {
			if settings.EnableTrickplay && !file.HasTrickplay && !trickplayQueued[file.ID] {
				if res, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: file.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
					log.Warn().Err(err).Int64("file_id", file.ID).Msg("apply_metadata: enqueue trickplay failed")
					fanout.Failed++
				} else if res.UniqueSkippedAsDuplicate {
					fanout.Skipped++
				} else {
					fanout.Trickplay++
				}
				trickplayQueued[file.ID] = true
			}
			if scannerMediaTypeScansSegments(lib.MediaType) && !file.SegmentsAnalyzedAt.Valid && !segmentsQueued[file.ID] && libraryFileHasPrimaryLink(links) {
				if res, err := rc.Insert(ctx, ScanMediaSegmentsFileArgs{LibraryFileID: file.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
					log.Warn().Err(err).Int64("file_id", file.ID).Msg("apply_metadata: enqueue media segments failed")
					fanout.Failed++
				} else if res.UniqueSkippedAsDuplicate {
					fanout.Skipped++
				} else {
					fanout.Segments++
				}
				segmentsQueued[file.ID] = true
			}
		}

		trackFile, err := q.GetTrackFileByLibraryFileID(ctx, file.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("apply_metadata: track file lookup failed")
			fanout.Failed++
			continue
		}
		if !trackFile.FingerprintedAt.Valid {
			if res, err := rc.Insert(ctx, ScanTrackFingerprintArgs{TrackFileID: trackFile.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
				log.Warn().Err(err).Int64("track_file_id", trackFile.ID).Msg("apply_metadata: enqueue chromaprint failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.Fingerprint++
			}
		}
		if trackFileNeedsLoudness(trackFile) {
			if res, err := rc.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: trackFile.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
				log.Warn().Err(err).Int64("track_file_id", trackFile.ID).Msg("apply_metadata: enqueue loudness failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.Loudness++
			}
		}
		if w.sonicEnabled(ctx) && trackNeedsSonicAnalysis(ctx, q, trackFile.TrackID) {
			if res, err := rc.Insert(ctx, AnalyzeTrackFacetsArgs{TrackID: trackFile.TrackID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
				log.Warn().Err(err).Int64("track_id", trackFile.TrackID).Msg("apply_metadata: enqueue sonic analysis failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.Sonic++
			}
		}
	}
	for mediaItemID := range mediaItemIDs {
		if scannerMediaTypeFetchesRatings(lib.MediaType) {
			if res, err := rc.Insert(ctx, RatingsFetchArgs{MediaItemID: mediaItemID, LibraryID: lib.ID}, nil); err != nil {
				log.Warn().Err(err).Int64("media_item_id", mediaItemID).Msg("apply_metadata: enqueue ratings failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.Ratings++
			}
		}
		if settings.SaveNFO && lib.MediaType == sqlc.MediaTypeMusic && !saveMusicNFOQueued[mediaItemID] {
			artist, err := q.GetArtistByMediaItemID(ctx, mediaItemID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				log.Warn().Err(err).Int64("media_item_id", mediaItemID).Msg("apply_metadata: artist lookup for music nfo failed")
				fanout.Failed++
				continue
			}
			if res, err := rc.Insert(ctx, SaveMusicNFOArgs{ArtistID: artist.ID}, nil); err != nil {
				log.Warn().Err(err).Int64("artist_id", artist.ID).Msg("apply_metadata: enqueue music nfo failed")
				fanout.Failed++
			} else if res.UniqueSkippedAsDuplicate {
				fanout.Skipped++
			} else {
				fanout.SaveMusicNFO++
			}
			saveMusicNFOQueued[mediaItemID] = true
		}
	}
	return fanout
}

func (w *ApplyLibraryScanWorker) sonicEnabled(ctx context.Context) bool {
	return w != nil && w.SonicEnabled != nil && w.SonicEnabled(ctx)
}

func scannerInventoryPostApplyPaths(inv scanner.Inventory) []string {
	set := map[string]bool{}
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if !scannerInventoryFileTracked(file) {
				continue
			}
			set[file.Path] = true
		}
	}
	return sortedMapKeys(set)
}

func scannerMediaTypeFetchesRatings(mt sqlc.MediaType) bool {
	return mt != sqlc.MediaTypeMusic
}

func scannerMediaTypeWritesVideoNFO(mt sqlc.MediaType) bool {
	return mt == sqlc.MediaTypeMovie || mt == sqlc.MediaTypeTv || mt == sqlc.MediaTypeAnime
}

func scannerMediaTypeScansSegments(mt sqlc.MediaType) bool {
	return mt == sqlc.MediaTypeMovie || mt == sqlc.MediaTypeTv || mt == sqlc.MediaTypeAnime
}

func libraryFileHasVideo(file sqlc.LibraryFile) bool {
	if libraryFileNeedsProbe(file) {
		return false
	}
	var info struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(file.MediaInfo, &info); err != nil {
		return false
	}
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			return true
		}
	}
	return false
}

func libraryFileHasPrimaryLink(links []sqlc.LibraryFileLink) bool {
	for _, link := range links {
		if link.RelationType != "extra" {
			return true
		}
	}
	return false
}

func libraryFileNeedsProbe(file sqlc.LibraryFile) bool {
	mediaInfo := bytes.TrimSpace(file.MediaInfo)
	return len(mediaInfo) == 0 || bytes.Equal(mediaInfo, []byte("{}")) || bytes.Equal(mediaInfo, []byte("null"))
}

func trackFileNeedsLoudness(file sqlc.TrackFile) bool {
	return !file.IntegratedLufs.Valid || !file.BoundariesAnalyzedAt.Valid
}

func trackNeedsSonicAnalysis(ctx context.Context, q *sqlc.Queries, trackID int64) bool {
	if trackID <= 0 {
		return false
	}
	ids, err := q.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
		AfterID:            trackID - 1,
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
		LimitCount:         1,
	})
	if err != nil {
		log.Warn().Err(err).Int64("track_id", trackID).Msg("apply_metadata: sonic eligibility lookup failed")
		return false
	}
	return len(ids) > 0 && ids[0] == trackID
}

func scannerSearchOptions(db *pgxpool.Pool, heya *heyamedia.HeyaProvider) scanner.Options {
	opts := scannerBaseOptions(db, heya)
	opts.RemoteSearch = true
	return opts
}

func scannerFetchOptions(db *pgxpool.Pool, heya *heyamedia.HeyaProvider) scanner.Options {
	opts := scannerBaseOptions(db, heya)
	opts.FetchPreview = true
	return scanner.NormalizeOptions(opts)
}

func scannerApplyOptions(db *pgxpool.Pool, heya *heyamedia.HeyaProvider) scanner.Options {
	opts := scannerBaseOptions(db, heya)
	opts.Apply = true
	return scanner.NormalizeOptions(opts)
}

func scannerBaseOptions(db *pgxpool.Pool, heya *heyamedia.HeyaProvider) scanner.Options {
	return scanner.Options{
		ApplyDB:             db,
		BookFetcher:         heya,
		BookMaterializer:    scanner.NewSQLBookMaterializeStore(db),
		BookSearcher:        heya,
		MovieFetcher:        heya,
		MovieMaterializer:   scanner.NewSQLMovieMaterializeStore(db),
		MovieSearcher:       heya,
		MusicFetcher:        heya,
		MusicMaterializer:   scanner.NewSQLMusicMaterializeStore(db),
		MusicProbe:          ProbeFile,
		MusicSearcher:       heya,
		PersistenceDB:       db,
		PersistScan:         true,
		OmitResultArtifacts: true,
		TVFetcher:           heya,
		TVMaterializer:      scanner.NewSQLTVMaterializeStore(db),
		TVSearcher:          heya,
	}
}

func supportsScanner(mt sqlc.MediaType) bool {
	return mt == sqlc.MediaTypeMovie || mt == sqlc.MediaTypeMusic || mt == sqlc.MediaTypeBook || mediatype.IsTVLike(mt)
}

func countScannerInventoryFiles(inv scanner.Inventory) int {
	total := 0
	for _, root := range inv.Roots {
		total += len(root.Files)
	}
	return total
}

func countScannerAppliedFiles(result scanner.Result) int {
	total := 0
	for _, applied := range result.BookApply {
		total += applied.FilesCreated + applied.FilesAttached + applied.FilesReassigned
	}
	for _, applied := range result.MovieApply {
		total += applied.FilesCreated + applied.FilesAttached + applied.FilesReassigned
	}
	for _, applied := range result.TVApply {
		total += applied.FilesCreated + applied.FilesAttached + applied.FilesReassigned
	}
	for _, applied := range result.MusicApply {
		total += applied.FilesCreated + applied.FilesAttached + applied.FilesReassigned
	}
	return total
}

func countScannerAcceptedSearch(result scanner.Result) int {
	total := 0
	for _, match := range result.BookSearch {
		if match.Accepted {
			total++
		}
	}
	for _, match := range result.MovieSearch {
		if match.Accepted {
			total++
		}
	}
	for _, match := range result.TVSearch {
		if match.Accepted {
			total++
		}
	}
	for _, match := range result.MusicSearch {
		if match.Accepted {
			total++
		}
	}
	return total
}

func countScannerFetchedMetadata(result scanner.Result) int {
	return countFetchedResultItems(result.MovieMetadata, result.BookMetadata, result.TVMetadata, result.MusicMetadata)
}

func countFetchedResultItems(movie []scanner.MovieFetchPreview, book []scanner.BookFetchPreview, tv []scanner.TVFetchPreview, music []scanner.MusicFetchPreview) int {
	total := 0
	for _, item := range movie {
		if item.ProviderID != "" {
			total++
		}
	}
	for _, item := range book {
		if item.ProviderID != "" {
			total++
		}
	}
	for _, item := range tv {
		if item.ProviderID != "" {
			total++
		}
	}
	for _, item := range music {
		if item.ProviderID != "" {
			total++
		}
	}
	return total
}

func sortLibrariesByMediaPriority(libs []sqlc.Library) {
	rank := func(mt sqlc.MediaType) int {
		switch mt {
		case sqlc.MediaTypeMovie:
			return 0
		case sqlc.MediaTypeTv, sqlc.MediaTypeAnime:
			return 1
		case sqlc.MediaTypeMusic:
			return 2
		case sqlc.MediaTypeBook:
			return 3
		}
		return 4
	}
	sort.SliceStable(libs, func(i, j int) bool {
		return rank(libs[i].MediaType) < rank(libs[j].MediaType)
	})
}

// reprobeCap bounds how many stuck-unprobed files one scan re-enqueues per
// library, so a large backlog (the single ffprobe worker drains slowly) can't
// flood the queue in one pass. ffprobe jobs are unique-while-active, so the same
// files simply re-coalesce across scans until they actually drain.
const reprobeCap = 2000

// enqueueReprobeUnprobed re-enqueues ffprobe for probeable files that are known
// (matched) but never got media_info — the "scanned once, probe failed, never
// retried" gap. Files that already carry media_info are left untouched, so a
// probed-and-unchanged file is never needlessly re-probed.
func enqueueReprobeUnprobed(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], libraryID int64, taskID string, source string) int {
	if rc == nil {
		return 0
	}
	files, err := q.ListUnprobedProbeableFiles(ctx, sqlc.ListUnprobedProbeableFilesParams{
		LibraryID: libraryID,
		Limit:     reprobeCap,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("kickoff_library_scan: list unprobed failed")
		return 0
	}
	n := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return n
		}
		if !mediafile.IsProbeable(f.Path) {
			continue // sidecars (.nfo/.srt/...) legitimately have no media_info
		}
		if _, err := rc.Insert(ctx, FFProbeArgs{
			LibraryFileID:   f.ID,
			FilePath:        f.Path,
			ScheduledTaskID: taskID,
		}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("file_id", f.ID).Msg("kickoff_library_scan: enqueue reprobe failed")
			continue
		}
		n++
	}
	return n
}

func emit(hub EventPublisher, t eventhub.EventType, p any) {
	if hub == nil {
		return
	}
	hub.Emit(t, p)
}

// ---------------------------------------------------------------------------
// kickoff_refresh_stale
// ---------------------------------------------------------------------------

type KickoffRefreshStaleWorker struct {
	river.WorkerDefaults[KickoffRefreshStaleArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffRefreshStaleWorker) Work(ctx context.Context, job *river.Job[KickoffRefreshStaleArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	source := scheduledJobSource(job.Metadata)
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	rows, err := w.DB.Query(ctx, `
		SELECT mi.id, mi.media_type, mi.title, mi.status, mi.metadata_refreshed_at, mi.enrichment_status
		FROM media_item_cards mi
		WHERE mi.media_type = 'music'
		   OR EXISTS (SELECT 1 FROM media_item_external_ids ei WHERE ei.media_item_id = mi.id)
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	defer rows.Close()

	now := time.Now()
	type stale struct {
		ID        int64
		MediaType sqlc.MediaType
		Title     string
		Force     bool
	}
	var items []stale
	for rows.Next() {
		var id int64
		var mt, title, mediaStatus, enrichStatus string
		var refreshedAt *time.Time
		if err := rows.Scan(&id, &mt, &title, &mediaStatus, &refreshedAt, &enrichStatus); err != nil {
			continue
		}
		// A previously FAILED enrichment is stranded — River doesn't retry it
		// (markFailed returns nil) and rescans skip the unchanged file. Re-drive
		// it every sweep so a transient provider blip self-heals. Non-forced is
		// enough (the item isn't 'complete', so the enrich idempotency gate lets
		// it run).
		if enrichStatus == "failed" {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title})
			continue
		}
		// Everything else here is the staleness path: only 'complete' items past
		// their refresh window. The window is automatic and keyed off the item's
		// production status — finished content barely changes, so it refreshes
		// slowly; still-airing content refreshes often. force=true because the
		// enrich worker short-circuits non-forced refreshes of 'complete' items —
		// without it the sweep would no-op.
		if enrichStatus != enrichStatusComplete {
			continue
		}
		window := refreshWindowDays(mediaStatus)
		if refreshedAt == nil {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title, Force: true})
			continue
		}
		cutoff := now.AddDate(0, 0, -window)
		if refreshedAt.Before(cutoff) {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title, Force: true})
		}
	}

	enqueued := 0
	failed := 0
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("refresh_stale_items", "kickoff_refresh_stale", it.Title)
		if err := enqueueEnrichWithMetadata(ctx, rc, it.ID, it.MediaType, EnrichSourceScheduled, it.Force, taskID, 0, 0, 0, scheduledJobMetadata(source)); err != nil {
			log.Warn().Err(err).Int64("item_id", it.ID).Msg("kickoff_refresh_stale: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		log.Info().Int("enqueued", enqueued).Msg("kickoff_refresh_stale: enqueued enrich jobs")
	}
	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// Automatic metadata-refresh windows. Finished content (ended/cancelled TV,
// released movies) almost never changes upstream, so it refreshes on a long
// cadence; anything still in motion (airing series, unreleased titles, and
// music/books which carry no status) refreshes far more often. These replace
// the old per-library metadata_refresh_days knob.
const (
	refreshWindowActiveDays = 14
	refreshWindowFinalDays  = 180
)

// refreshWindowDays maps a media_items.status string to its staleness window
// in days. Status arrives lowercase and unnormalized from heya.media; the same
// finished-vs-active split the Jellyfin mapper uses.
func refreshWindowDays(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ended", "canceled", "cancelled", "released":
		return refreshWindowFinalDays
	default:
		return refreshWindowActiveDays
	}
}

// ---------------------------------------------------------------------------
// kickoff pumps (music loudness + sonic analysis)
// ---------------------------------------------------------------------------

// The loudness and sonic kickoffs are "pumps": instead of fanning out one
// bounded batch and finishing (which stranded the rest of the backlog until
// the next cron window), the kickoff job stays active for the whole run —
// snoozing between wakes, topping the work queue up wave by wave until the
// backlog drains. Consequences that the rest of the design leans on:
//
//   - The kickoff row's uniqueness hold (uniqueWhileActive) spans the run,
//     so a cron firing during an active run coalesces into it — the window
//     is skipped rather than stacking a second run.
//   - The row's created_at is the run's start time and its metadata is the
//     run's memory: sweep cursors, enqueue counters, and the manual/
//     scheduled source marker all survive snoozes and even process
//     restarts (an orphaned 'running' row is rescued on boot and resumes).
//   - Manual runs ("Run Now" → metadata source=manual) drain everything;
//     cron-started runs additionally stop when the task's max-runtime
//     window closes. The pump checks the window itself on every wake, so
//     it winds down gracefully and stamps the scheduled_tasks row.
//   - The pending sets are swept in id order exactly once per run (cursor
//     in metadata), so an item whose work job fails permanently is passed
//     over instead of being re-listed and re-enqueued forever.
const (
	pumpSnoozeInterval = 30 * time.Second
	// pumpMaxErrStreak is how many consecutive failing wakes a run
	// survives before it's declared dead. One-off DB blips shouldn't kill
	// a days-long drain; a persistent fault shouldn't wedge the task.
	pumpMaxErrStreak = 10
)

// pumpState is the pump's cross-wake memory, persisted in the kickoff job
// row's metadata. Loudness uses both cursors; sonic only TrackCursor.
//
// Skipped counts sweep items whose insert coalesced with a job owned by
// another task (unique keys are per-entity, so e.g. a library scan's
// loudness hand-offs occupy the same slot but are invisible to this run's
// scoped counts) or whose insert errored. If any were skipped, the finish
// path re-runs the sweep once from zero (FinalSweep) so work that the
// other owner dropped — a cancelled scan, a max-runtime kill — still gets
// picked up instead of being silently stranded past the cursor.
type pumpState struct {
	Source      string `json:"source,omitempty"`
	Enqueued    int    `json:"enqueued,omitempty"`
	Failed      int    `json:"failed,omitempty"`
	ErrStreak   int    `json:"err_streak,omitempty"`
	Skipped     int    `json:"skipped,omitempty"`
	FinalSweep  bool   `json:"final_sweep,omitempty"`
	TrackCursor int64  `json:"track_cursor,omitempty"`
	AlbumCursor int64  `json:"album_cursor,omitempty"`
}

func readPumpState(metadata []byte) pumpState {
	var st pumpState
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &st)
	}
	return st
}

// patch serializes the keys the pump owns. Source is deliberately absent:
// MarkActiveKickoffManual may flip it mid-run, and the jsonb || merge must
// not undo that upgrade with the stale value read at wake start.
// "finishing" is always reset: it's only meaningful between a
// ClaimKickoffFinish and the completion that follows it (no patch is
// written in that window), so any patched wake is by definition a run
// that continues — including one that aborted a wind-down or resumed
// after a crash mid-finish — and must accept upgrades again.
func (st pumpState) patch() []byte {
	b, err := json.Marshal(map[string]any{
		"enqueued":     st.Enqueued,
		"failed":       st.Failed,
		"err_streak":   st.ErrStreak,
		"skipped":      st.Skipped,
		"final_sweep":  st.FinalSweep,
		"finishing":    false,
		"track_cursor": st.TrackCursor,
		"album_cursor": st.AlbumCursor,
	})
	if err != nil {
		return []byte("{}")
	}
	return b
}

// restartSweep resets the cursors for the one-time verification pass over
// items that were skipped (coalesced with another owner's job or failed to
// insert). Returns false once the final sweep has already run — the pump
// finishes rather than looping.
func (st *pumpState) restartSweep() bool {
	if st.Skipped == 0 || st.FinalSweep {
		return false
	}
	st.FinalSweep = true
	st.Skipped = 0
	st.TrackCursor = 0
	st.AlbumCursor = 0
	return true
}

// continueAsManual reorients an in-flight run after a mid-wake Run-Now
// upgrade beat the completion claim: sweep everything still pending from
// scratch, exactly like a freshly-started manual run would. (The row's
// source is already "manual" — MarkActiveKickoffManual wrote it — so only
// the in-memory copy and the sweep state need resetting; the next state
// patch clears the finishing claim.)
func (st *pumpState) continueAsManual() {
	st.Source = queueops.KickoffSourceManual
	st.Skipped = 0
	st.FinalSweep = false
	st.TrackCursor = 0
	st.AlbumCursor = 0
}

// pumpFinishHandshake claims the finishing marker ahead of ANY pump
// completion — drained, wind-down, disabled, or error give-up. It returns
// proceed=false when the claim reveals a Run-Now upgrade that landed
// mid-wake on a cron run: st has been reoriented as a fresh manual drain
// and the caller must keep the run alive. With proceed=true the caller
// completes, and upgrades arriving from now on are rejected by
// MarkActiveKickoffManual's finishing guard (their TriggerNow starts a
// fresh run instead) — so a click can never land on a completing row and
// be silently swallowed. Runs already manual always proceed: the claim
// still blocks late upgrades, but their own source can't distinguish a
// new click from the old state, and re-aborting on it would loop forever.
func pumpFinishHandshake(ctx context.Context, db *pgxpool.Pool, jobID int64, st *pumpState) (proceed bool, err error) {
	live, err := queueops.ClaimKickoffFinish(ctx, db, jobID)
	if err != nil {
		return false, err
	}
	if st.Source != queueops.KickoffSourceManual && live == queueops.KickoffSourceManual {
		st.continueAsManual()
		return false, nil
	}
	return true, nil
}

// pumpSnooze persists the pump's state and puts the kickoff back to
// sleep. JobSnooze doesn't consume attempts, so a MaxAttempts=1 kickoff
// can wake indefinitely.
func pumpSnooze(ctx context.Context, db *pgxpool.Pool, jobID int64, taskID string, st pumpState) error {
	if err := queueops.MergeJobMetadata(ctx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state failed")
	}
	return river.JobSnooze(pumpSnoozeInterval)
}

// pumpActiveCount returns how many of the task's own work jobs of one kind
// are still pending or running. Jobs the same kind owes to other owners
// (e.g. a library scan's loudness hand-off) are excluded — the pump only
// waits on work it fanned out.
func pumpActiveCount(ctx context.Context, db *pgxpool.Pool, taskID, kind string) (int, error) {
	if taskID == "" {
		counts, err := queueops.CountByKinds(ctx, db, []string{kind})
		return counts.Pending + counts.Running, err
	}
	counts, err := queueops.CountScheduledTask(ctx, db, taskID, []string{kind})
	return counts.Pending + counts.Running, err
}

// pumpShouldStop reports whether a cron-started run must wind down: the
// task was disabled mid-run, or it outlived its max-runtime window. Manual
// runs never expire — only a user cancel stops them. The task row is
// re-read on every wake so a mid-run settings change takes effect.
func pumpShouldStop(ctx context.Context, q *sqlc.Queries, taskID, source string, runStarted time.Time) (bool, string) {
	if source == queueops.KickoffSourceManual || taskID == "" {
		return false, ""
	}
	task, err := q.GetScheduledTask(ctx, taskID)
	if err != nil {
		return false, ""
	}
	if !task.Enabled {
		return true, "task disabled"
	}
	if task.MaxRuntimeMinutes > 0 && time.Since(runStarted) > time.Duration(task.MaxRuntimeMinutes)*time.Minute {
		return true, "max runtime reached"
	}
	return false, ""
}

// pumpInterrupted handles a context death mid-wake (user cancel, process
// shutdown, job timeout): persist the cursors best-effort and yield with a
// zero snooze. This can't escape a user cancel — River finalizes a
// snoozing job as cancelled when cancel_attempted_at is stamped in its
// metadata — while a plain shutdown parks the row 'available' so the run
// resumes right where it left off on the next boot. Run bookkeeping for
// the cancel case is stamped by service.CancelTask, which reads the
// kickoff row before cancelling it.
func pumpInterrupted(ctx context.Context, db *pgxpool.Pool, jobID int64, taskID string, st pumpState) error {
	_ = ctx // dead by definition here; persist on a short background context
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := queueops.MergeJobMetadata(persistCtx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state on interrupt failed")
	}
	return river.JobSnooze(0)
}

// pumpTransientFailure bumps the run's error streak and snoozes instead
// of failing the single-attempt kickoff. Past pumpMaxErrStreak the run
// fails for real (finishKickoff stamps the error, MaxAttempts=1 discards)
// — through the finishing handshake, so a Run Now that landed mid-wake
// restarts the drain ("user poked it, try again") instead of dying with
// the run, and one arriving later starts a fresh run.
func pumpTransientFailure(ctx context.Context, db *pgxpool.Pool, q *sqlc.Queries, jobID int64, taskID string, st pumpState, runStarted time.Time, cause error) error {
	if ctx.Err() != nil {
		return pumpInterrupted(ctx, db, jobID, taskID, st)
	}
	st.ErrStreak++
	if st.ErrStreak >= pumpMaxErrStreak {
		proceed, hErr := pumpFinishHandshake(ctx, db, jobID, &st)
		switch {
		case hErr != nil:
			// The give-up REQUIRES a successful claim — completing
			// unmarked would let a late Run-Now land on the dying row and
			// vanish. The claim is a single-row UPDATE by primary key; if
			// even that fails, snooze and retry the give-up next wake
			// (the streak stays ≥ max, so it re-enters here; a healthy
			// wake in between resets it instead). If the claim actually
			// committed but its result was lost, the snooze patch clears
			// the stray marker, so upgrades aren't blocked meanwhile.
			log.Warn().Err(hErr).Str("task", taskID).Msg("kickoff pump: finishing claim failed, deferring give-up")
			return pumpSnooze(ctx, db, jobID, taskID, st)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff pump: error give-up aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, db, jobID, taskID, st)
		}
		log.Error().Err(cause).Str("task", taskID).Msg("kickoff pump: giving up after repeated failures")
		finishKickoff(ctx, q, taskID, runStarted, st.Enqueued, st.Failed, cause)
		return cause
	}
	log.Warn().Err(cause).Str("task", taskID).Int("err_streak", st.ErrStreak).Msg("kickoff pump: transient failure, snoozing")
	if err := queueops.MergeJobMetadata(ctx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state failed")
	}
	return river.JobSnooze(pumpSnoozeInterval)
}

// ---------------------------------------------------------------------------
// kickoff_music_loudness
// ---------------------------------------------------------------------------

// Per-wave caps. The scan_track_loudness queue is MaxWorkers=1 so it'll
// chew through the backlog at ~30s/track regardless. The pump keeps at
// most one wave in River at a time and tops it up as it drains, so the
// job table stays bounded no matter how large the backlog is.
const (
	kickoffLoudnessTrackBatch = 500
	kickoffLoudnessAlbumBatch = 200
)

type KickoffMusicLoudnessWorker struct {
	river.WorkerDefaults[KickoffMusicLoudnessArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffMusicLoudnessWorker) Work(ctx context.Context, job *river.Job[KickoffMusicLoudnessArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	st := readPumpState(job.Metadata)
	trackKind := ScanTrackLoudnessArgs{}.Kind()
	albumKind := ScanAlbumLoudnessArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_music_loudness: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{trackKind, albumKind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_music_loudness: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	// Track phase: keep one wave of per-track jobs topped up, sweeping the
	// pending set in id order exactly once.
	trackActive, err := pumpActiveCount(ctx, w.DB, taskID, trackKind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	tracksListed := -1 // -1: wave full, sweep not consulted this wake
	if want := kickoffLoudnessTrackBatch - trackActive; want > 0 {
		rows, err := q.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{
			AfterID:  st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		tracksListed = len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Path)
			res, err := rc.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: row.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(st.Source))
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("kickoff_music_loudness: enqueue track failed")
				st.Failed++
				st.Skipped++
			case res.UniqueSkippedAsDuplicate:
				st.Skipped++
			default:
				st.Enqueued++
			}
			st.TrackCursor = row.ID
		}
	}
	tracksDone := trackActive == 0 && tracksListed == 0

	// Album phase: only starts once the track sweep has drained, so album
	// eligibility (all tracks measured) is stable and one monotonic pass is
	// complete. Albums that finished *during* this run were already
	// enqueued by the track worker's cascade; the unique args make this
	// sweep coalesce with those.
	if tracksDone {
		albumActive, err := pumpActiveCount(ctx, w.DB, taskID, albumKind)
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		albumsListed := -1
		if want := kickoffLoudnessAlbumBatch - albumActive; want > 0 {
			rows, err := q.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{
				AfterID:  st.AlbumCursor,
				RowLimit: int32(want),
			})
			if err != nil {
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			}
			albumsListed = len(rows)
			for _, row := range rows {
				if ctx.Err() != nil {
					return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
				}
				w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Title)
				res, err := rc.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: row.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(st.Source))
				switch {
				case err != nil:
					log.Warn().Err(err).Int64("album_id", row.ID).Msg("kickoff_music_loudness: enqueue album failed")
					st.Failed++
					st.Skipped++
				case res.UniqueSkippedAsDuplicate:
					st.Skipped++
				default:
					st.Enqueued++
				}
				st.AlbumCursor = row.ID
			}
		}
		if albumActive == 0 && albumsListed == 0 {
			if st.restartSweep() {
				log.Info().Str("task", taskID).Msg("kickoff_music_loudness: re-sweeping for items skipped during the run")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
			case err != nil:
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			case !proceed:
				log.Info().Str("task", taskID).Msg("kickoff_music_loudness: finish aborted — run upgraded to manual mid-wake")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_music_loudness: backlog drained")
			finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
			return nil
		}
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}

// ---------------------------------------------------------------------------
// kickoff_trickplay
// ---------------------------------------------------------------------------

type KickoffTrickplayWorker struct {
	river.WorkerDefaults[KickoffTrickplayArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffTrickplayWorker) Work(ctx context.Context, job *river.Job[KickoffTrickplayArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	source := scheduledJobSource(job.Metadata)
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	// Eligibility lives in the trickplay_eligible_files view (migration 00035),
	// shared with the Settings counts and task item listings — one predicate,
	// no count-vs-enqueue drift.
	pending, err := q.ListTrickplayPendingKickoff(ctx)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, f := range pending {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("generate_trickplay", "kickoff_trickplay", filepathBase(f.Path))
		if _, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: f.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("library_file_id", f.ID).Msg("kickoff_trickplay: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// filepathBase is a local indirection so we can keep the import surface of
// kickoff_workers.go small (no path/filepath import needed elsewhere here).
func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// kickoff_thumbnails
// ---------------------------------------------------------------------------

type KickoffThumbnailsWorker struct {
	river.WorkerDefaults[KickoffThumbnailsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffThumbnailsWorker) Work(ctx context.Context, job *river.Job[KickoffThumbnailsArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	source := scheduledJobSource(job.Metadata)
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	// Eligibility lives in the thumbnail_eligible_extras view,
	// shared with the Settings counts and task item listings — one predicate,
	// no count-vs-enqueue drift.
	pending, err := q.ListThumbnailPendingKickoff(ctx)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, e := range pending {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		label := e.Title
		if label == "" {
			label = filepathBase(e.FilePath)
		}
		w.Progress.Set("generate_thumbnails", "kickoff_thumbnails", label)
		if _, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: e.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("extra_id", e.ID).Msg("kickoff_thumbnails: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// ---------------------------------------------------------------------------
// kickoff_sonic_analysis
// ---------------------------------------------------------------------------

// SonicEnabledFn is the runtime gate for kickoff_sonic_analysis. Lets
// the worker honour the system_settings toggle without importing the
// service layer. Wired up by the App at startup.
type SonicEnabledFn func(ctx context.Context) bool

// sonicKickoffBatch caps the pump's in-flight wave so a fresh 100k-track
// library doesn't dump 100k jobs into River in one shot. The pump tops
// the wave up as it drains until the whole backlog is analyzed.
const sonicKickoffBatch = 1000

type KickoffSonicAnalysisWorker struct {
	river.WorkerDefaults[KickoffSonicAnalysisArgs]
	DB       *pgxpool.Pool
	Enabled  SonicEnabledFn
	Progress *TaskProgressBroadcaster
}

func (w *KickoffSonicAnalysisWorker) Work(ctx context.Context, job *river.Job[KickoffSonicAnalysisArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	st := readPumpState(job.Metadata)
	kind := AnalyzeTrackFacetsArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	// Checked on every wake, so toggling the setting off mid-run stops the
	// pump; only the in-flight wave (bounded) is left to drain. Goes
	// through the finishing handshake like every completion — a mid-wake
	// upgrade just defers the (inevitable, feature's off) finish by one
	// wake rather than being swallowed by it.
	if w.Enabled != nil && !w.Enabled(ctx) {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		log.Info().Msg("kickoff_sonic_analysis: disabled in settings — stopping")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		// Pending centroid refreshes are left alone: they're quick and keep
		// artist/album centroids consistent with the tracks already analyzed.
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{kind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_sonic_analysis: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	rc := river.ClientFromContext[pgx.Tx](ctx)
	active, err := pumpActiveCount(ctx, w.DB, taskID, kind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	listed := -1 // -1: wave full, sweep not consulted this wake
	if want := sonicKickoffBatch - active; want > 0 {
		ids, err := q.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
			AfterID:            st.TrackCursor,
			MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
			AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
			LimitCount:         int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		listed = len(ids)
		if len(ids) > 0 {
			w.Progress.Set("analyze_music_facets", "kickoff_sonic_analysis", "queueing tracks…")
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			res, err := rc.Insert(ctx, AnalyzeTrackFacetsArgs{TrackID: id, ScheduledTaskID: taskID}, scheduledJobInsertOpts(st.Source))
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("track_id", id).Msg("kickoff_sonic_analysis: enqueue failed")
				st.Failed++
				st.Skipped++
			case res.UniqueSkippedAsDuplicate:
				st.Skipped++
			default:
				st.Enqueued++
			}
			st.TrackCursor = id
		}
	}
	if active == 0 && listed == 0 {
		if st.restartSweep() {
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: re-sweeping for items skipped during the run")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: finish aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		// Centroid refreshes cascade from the per-track jobs and are quick;
		// the run doesn't wait on them.
		log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_sonic_analysis: backlog drained")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}
