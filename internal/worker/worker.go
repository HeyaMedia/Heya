package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/rs/zerolog/log"
)

var _ pgx.Tx // ensure import used

// MatchService abstracts the matcher operations used by workers so that tests
// can supply lightweight fakes instead of a fully-wired *matcher.Matcher.
type MatchService interface {
	MatchSingleFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) (matcher.MatchInfo, error)
	StoreEntityMetadata(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail) error
	StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail) error
	ResolveMatch(ctx context.Context, fileID, candidateID int64) error
	RefreshMusicArtist(ctx context.Context, artistID int64) (matcher.RefreshArtistResult, error)
	MediaItemIDForArtist(ctx context.Context, artistID int64) (int64, error)
}

// EventPublisher abstracts the event-emitting side of the event hub so that
// workers can be tested without a live Hub.
type EventPublisher interface {
	Emit(eventType eventhub.EventType, payload any)
}

type Config struct {
	DB             *pgxpool.Pool
	DataDir        string
	HeyaMedia      *heyamedia.Client
	Heya           *heyamedia.HeyaProvider
	Matcher        MatchService
	Downloader     *images.Downloader
	TranscodeCache *transcoder.CacheManager
	HWAccel        *transcoder.HwAccelProvider
	Hub            EventPublisher
	// SonicHolder is the singleton CLAP/Discogs model lessor used by
	// the analyze_track_facets worker. Nil when sonic analysis is
	// disabled — the worker errors fast in that case.
	SonicHolder *sonicanalysis.Holder
	// SonicEnabled gates kickoff_sonic_analysis at runtime. Looks up
	// the system_settings toggle without importing service/.
	SonicEnabled SonicEnabledFn
	// Watcher receives Pause/Resume during library scans so fsnotify
	// doesn't race the scanner's bulk writes. nil during tests + the
	// `heya queue process` CLI which runs without watchers.
	Watcher WatcherPauser
	// Progress is the per-task "currently working on X" emitter.
	// Workers call SetCurrentByKind at the top of Work() so the UI
	// shows live labels. nil-safe — emissions become no-ops.
	Progress *TaskProgressBroadcaster
}

func Setup(ctx context.Context, cfg Config) (*river.Client[pgx.Tx], error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(cfg.DB), nil)
	if err != nil {
		return nil, fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("river migrate: %w", err)
	}
	log.Info().Msg("river migrations applied")

	// One-time, idempotent: free pre-fix jobs from the unique index so the
	// uniqueWhileActive() change takes effect on this deploy instead of a
	// day later. Non-fatal and time-boxed — a slow/unreachable DB must not
	// stall startup, and worst case is the old (slow) re-runnable behaviour
	// until River's cleaner ages the rows out.
	func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := clearStaleUniqueJobStates(cleanupCtx, cfg.DB); err != nil {
			log.Warn().Err(err).Msg("clear stale unique_states: skipped")
		}
	}()

	workers := river.NewWorkers()
	river.AddWorker(workers, &ProcessFileWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &MetadataMatchWorker{DB: cfg.DB, Matcher: cfg.Matcher, Heya: cfg.Heya, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &EnrichMediaItemWorker{DB: cfg.DB, Matcher: cfg.Matcher, Heya: cfg.Heya, Hub: cfg.Hub, DataDir: cfg.DataDir, Progress: cfg.Progress})
	river.AddWorker(workers, &DownloadImageWorker{DB: cfg.DB, Downloader: cfg.Downloader, HeyaMedia: cfg.HeyaMedia, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &FFProbeWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &DetectLocalAssetsWorker{DB: cfg.DB, DataDir: cfg.DataDir, Progress: cfg.Progress})
	river.AddWorker(workers, &PersonFetchWorker{DB: cfg.DB, HeyaMedia: cfg.HeyaMedia, Progress: cfg.Progress})
	river.AddWorker(workers, &FetchArtworkWorker{DB: cfg.DB, Heya: cfg.Heya, Progress: cfg.Progress})
	river.AddWorker(workers, &RatingsFetchWorker{DB: cfg.DB, Heya: cfg.Heya, Progress: cfg.Progress})
	river.AddWorker(workers, &SaveNFOWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &SaveImagesWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ForceRefreshMetadataWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ForceRefreshImagesWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &TranscodeWorker{DB: cfg.DB, Cache: cfg.TranscodeCache, HWAccel: cfg.HWAccel, Progress: cfg.Progress})
	river.AddWorker(workers, &SoftDeleteWorker{DB: cfg.DB, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &SaveMusicNFOWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ScanTrackLoudnessWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ScanAlbumLoudnessWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ScanTrackFingerprintWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &TrickplayFileWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ThumbnailExtraWorker{DB: cfg.DB, DataDir: cfg.DataDir, Progress: cfg.Progress})
	river.AddWorker(workers, &AnalyzeTrackFacetsWorker{DB: cfg.DB, Holder: cfg.SonicHolder, Progress: cfg.Progress})
	river.AddWorker(workers, &RefreshArtistCentroidWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &RefreshAlbumCentroidWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ScanLibraryDiskWorker{DB: cfg.DB, Progress: cfg.Progress})

	// Kickoff workers — each fans out into the per-item work queues
	// above. The 60-second trigger loop in internal/scheduler/ inserts
	// these on the cadence set in the scheduled_tasks table. "Run Now"
	// from the UI hits the same insertion path with UniqueByArgs so
	// concurrent clicks coalesce.
	river.AddWorker(workers, &KickoffLibraryScanWorker{DB: cfg.DB, Hub: cfg.Hub, Watcher: cfg.Watcher, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffRefreshStaleWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMusicLoudnessWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMusicFingerprintWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffTrickplayWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffThumbnailsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffSonicAnalysisWorker{DB: cfg.DB, Enabled: cfg.SonicEnabled, Progress: cfg.Progress})

	// Debounce sweep — owns its own queue and fires every 10s via the
	// periodic-jobs entry below.
	river.AddWorker(workers, &DebounceSweepWorker{DB: cfg.DB})

	client, err := river.NewClient(riverpgxv5.New(cfg.DB), &river.Config{
		// One queue per worker kind. Two reasons:
		//   1. Scanner / probe / match touch the source filesystem (or
		//      SMB share); serialising each step keeps us from
		//      hammering the source with concurrent IO.
		//   2. Each external dependency (HeyaMedia search, TMDB people,
		//      rating providers) gets its own concurrency knob without
		//      contending against unrelated work.
		// download_image is the lone exception — it hits provider CDNs,
		// not the source, so parallelism there is fine.
		// New kinds added in this refactor (kickoff_*, trickplay_file,
		// thumbnail_extra, analyze_track_facets, refresh_*_centroids)
		// also live here at MaxWorkers=1.
		Queues: map[string]river.QueueConfig{
			// Scanner pipeline (source-throttled).
			"kickoff_library_scan": {MaxWorkers: 1},
			"process_file":         {MaxWorkers: 1}, // priority bands: P1=watcher, P2=scan
			"ffprobe":              {MaxWorkers: 1},
			"detect_local_assets":  {MaxWorkers: 1},
			"metadata_match":       {MaxWorkers: 1},

			// Enrich pipeline (external rate-limit safety).
			"enrich_media_item":      {MaxWorkers: 1}, // priority bands P1=watcher/view, P2=movies+tv, P3=music+books
			"person_fetch":           {MaxWorkers: 1},
			"ratings_fetch":          {MaxWorkers: 1},
			"force_refresh_metadata": {MaxWorkers: 1},
			"fetch_artwork":          {MaxWorkers: 1}, // secondary artwork pass — extra backdrops + alternates beyond the primary set GetDetail returned

			// Images.
			"download_image":       {MaxWorkers: 4}, // hits CDN/heya.media, not source FS
			"save_images":          {MaxWorkers: 1},
			"force_refresh_images": {MaxWorkers: 1},

			// NFOs.
			"save_nfo":       {MaxWorkers: 1},
			"save_music_nfo": {MaxWorkers: 1},

			// CPU/heavy work (one at a time so they can't starve scans).
			"scan_track_loudness":    {MaxWorkers: 1}, // ebur128 ~10-20× real-time
			"scan_album_loudness":    {MaxWorkers: 1}, // concat demuxer + ebur128
			"scan_track_fingerprint": {MaxWorkers: 1}, // chromaprint, first 120s only
			"trickplay":              {MaxWorkers: 1}, // ffmpeg sprites
			"thumbnails":             {MaxWorkers: 1}, // ffmpeg thumbnail extraction
			"sonic_analysis":         {MaxWorkers: 1}, // full model bundle (Discogs heads + EffNet base + classifier heads + CLAP audio) held by AnalyzerHolder singleton; ~hundreds of MB resident
			"transcode":              {MaxWorkers: 1},

			// Sonic centroid refreshes (cheap; own queue so they don't
			// block the next track analysis).
			"artist_centroid": {MaxWorkers: 1},
			"album_centroid":  {MaxWorkers: 1},

			// Disk-usage scan — read-only walk of library paths to populate
			// the Storage page. Own queue so a multi-TB walk doesn't block
			// any other admin-triggered work.
			"scan_library_disk": {MaxWorkers: 1},

			// Kickoffs (each their own queue, UniqueByArgs so click-spam
			// is a no-op while one is queued/running).
			"kickoff_refresh_stale":     {MaxWorkers: 1},
			"kickoff_music_loudness":    {MaxWorkers: 1},
			"kickoff_music_fingerprint": {MaxWorkers: 1},
			"kickoff_trickplay":         {MaxWorkers: 1},
			"kickoff_thumbnails":        {MaxWorkers: 1},
			"kickoff_sonic_analysis":    {MaxWorkers: 1},

			// Misc.
			"soft_delete":      {MaxWorkers: 1},
			"debounce_sweep":   {MaxWorkers: 1}, // periodic sweep; trailing-edge debounce of child-content enriches
			river.QueueDefault: {MaxWorkers: 1}, // fallback only; we shouldn't actually use it after the split
		},
		// Periodic jobs — River-managed cron. The DebounceSweep fires
		// every 10s so the trailing-edge debounce on child-content
		// matches lands within a tight window after churn stops.
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(10*time.Second),
				func() (river.JobArgs, *river.InsertOpts) {
					return DebounceSweepArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		},
		// Per-job context deadline. River's default JobTimeout is 1
		// MINUTE — it silently wraps every Work(ctx) in a 60s
		// context.WithTimeout, which killed essentially every heavy job
		// here (SMB library scans, the 30-minute sonic model fetch,
		// loudness/transcode/trickplay/disk-walk) with "context deadline
		// exceeded". queueops.JobTimeout (6h) is a generous ceiling no
		// legitimate single job should hit; real bounds live where they
		// belong — per-HTTP-client timeouts, ffmpeg/walk semantics.
		JobTimeout: queueops.JobTimeout,
		// Backstop for jobs whose worker died mid-run (process died, OS
		// killed it, etc.). Held at queueops.RescueStuckAfter (= JobTimeout
		// + 1h) so it always exceeds the longest a healthy job can run: a
		// job past its timeout has had its context cancelled and is
		// genuinely stuck, so rescuing it can't duplicate a live worker.
		// Shared with the manual RescueStuckRunning sweep so the two agree.
		RescueStuckJobsAfter: queueops.RescueStuckAfter,
		Workers:              workers,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}

	return client, nil
}
