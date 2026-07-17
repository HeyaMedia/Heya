package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/communitysegments"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/rs/zerolog/log"
)

var _ pgx.Tx // ensure import used

// MatchService abstracts the matcher operations used by workers so that tests
// can supply lightweight fakes instead of a fully-wired *matcher.Matcher.
type MatchService interface {
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
	HeyaMetadata   *heyametadata.Client
	Heya           *heyametadata.HeyaProvider
	Segments       *communitysegments.Service
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
	// EmbedBackfill runs the recommendations-embedding self-heal sweep
	// (kickoff_embed_recommendations). Same no-import-of-service
	// indirection as SonicEnabled; nil-safe (worker no-ops).
	EmbedBackfill EmbedBackfillFn
	// LastfmCreds resolves the server-level Last.fm app credentials for the
	// listen-import workers (env or admin-configured). Nil-safe.
	LastfmCreds LastfmCredsFn
	// Watcher receives Pause/Resume during library scans so fsnotify
	// doesn't race the scanner's bulk writes. nil during tests + the
	// `heya queue process` CLI which runs without watchers.
	Watcher WatcherPauser
	// Progress is the per-task "currently working on X" emitter.
	// Workers call SetCurrentByKind at the top of Work() so the UI
	// shows live labels. nil-safe — emissions become no-ops.
	Progress     *TaskProgressBroadcaster
	WorkerCounts map[string]int
	// Passive marks this process a read-mostly guest on a shared DB
	// (dev box on prod, `heya doctor` on any box). Setup then skips its
	// two writes — River's schema migrations and the one-time
	// unique_states cleanup — mirroring how service.New skips goose
	// AutoMigrate: failing queries on a drifted schema are safer than
	// altering a database this process doesn't own.
	Passive bool
}

// NewInsertClient returns a River client that can insert/cancel/query jobs but
// owns no queues and starts no maintenance services. The API process uses this
// client so serving traffic cannot accidentally compete with the dedicated
// worker process for jobs or run queue migrations during ingress startup.
func NewInsertClient(db *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
	if err != nil {
		return nil, fmt.Errorf("river insert client: %w", err)
	}
	return client, nil
}

func Setup(ctx context.Context, cfg Config) (*river.Client[pgx.Tx], error) {
	if cfg.Passive {
		log.Warn().Msg("passive mode: skipping river migrations + stale unique_states cleanup")
	} else {
		if err := database.MigrateRiver(ctx, cfg.DB); err != nil {
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
			if err := renameLegacyScannerJobs(cleanupCtx, cfg.DB); err != nil {
				log.Warn().Err(err).Msg("rename legacy scanner jobs: skipped")
			}
		}()
	}

	workers := river.NewWorkers()
	continuationBackoff := newMetadataContinuationBackoff()
	river.AddWorker(workers, &EnrichMediaItemWorker{DB: cfg.DB, Matcher: cfg.Matcher, Heya: cfg.Heya, Hub: cfg.Hub, DataDir: cfg.DataDir, Progress: cfg.Progress})
	river.AddWorker(workers, &DownloadImageWorker{DB: cfg.DB, Downloader: cfg.Downloader, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &FFProbeWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &ScanKeyframesWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &DetectLocalAssetsWorker{DB: cfg.DB, DataDir: cfg.DataDir, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &PersonFetchWorker{DB: cfg.DB, HeyaMetadata: cfg.HeyaMetadata, Progress: cfg.Progress})
	river.AddWorker(workers, &FetchArtworkWorker{DB: cfg.DB, Heya: cfg.Heya, Progress: cfg.Progress})
	river.AddWorker(workers, &RatingsFetchWorker{DB: cfg.DB, Heya: cfg.Heya, Hub: cfg.Hub, Progress: cfg.Progress})
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
	river.AddWorker(workers, &ScanMediaSegmentsFileWorker{DB: cfg.DB, Segments: cfg.Segments, Progress: cfg.Progress})
	river.AddWorker(workers, &DetectSeasonSegmentsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &DetectMovieCreditsWorker{DB: cfg.DB, Progress: cfg.Progress})
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
	kickoffLibraryWorker := &KickoffLibraryScanWorker{DB: cfg.DB, Heya: cfg.Heya, Hub: cfg.Hub, Watcher: cfg.Watcher, Progress: cfg.Progress}
	river.AddWorker(workers, kickoffLibraryWorker)
	river.AddWorker(workers, &ProcessLibraryScanWorker{DB: cfg.DB, Hub: cfg.Hub, Watcher: cfg.Watcher, Progress: cfg.Progress})
	river.AddWorker(workers, &SearchLibraryMetadataWorker{DB: cfg.DB, Heya: cfg.Heya, Hub: cfg.Hub, Progress: cfg.Progress, Backoff: continuationBackoff})
	river.AddWorker(workers, &FetchLibraryMetadataWorker{DB: cfg.DB, Heya: cfg.Heya, Hub: cfg.Hub, Watcher: cfg.Watcher, Progress: cfg.Progress})
	river.AddWorker(workers, &ApplyLibraryScanWorker{DB: cfg.DB, Heya: cfg.Heya, Hub: cfg.Hub, Watcher: cfg.Watcher, SonicEnabled: cfg.SonicEnabled, Progress: cfg.Progress})
	river.AddWorker(workers, &ApplyRichMetadataWorker{DB: cfg.DB, Matcher: cfg.Matcher, Hub: cfg.Hub, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffRefreshStaleWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMusicLoudnessWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMusicFingerprintWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMediaSegmentsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffDetectSegmentsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffTrickplayWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffThumbnailsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffSonicAnalysisWorker{DB: cfg.DB, Enabled: cfg.SonicEnabled, Progress: cfg.Progress})
	river.AddWorker(workers, &CleanupScannerArtifactsWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffEmbedRecommendationsWorker{DB: cfg.DB, EmbedBackfill: cfg.EmbedBackfill, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffListenImportWorker{DB: cfg.DB, LastfmCreds: cfg.LastfmCreds, Progress: cfg.Progress})
	river.AddWorker(workers, &ImportListensBatchWorker{DB: cfg.DB, Progress: cfg.Progress})
	river.AddWorker(workers, &KickoffMusicServicesSyncWorker{DB: cfg.DB, LastfmCreds: cfg.LastfmCreds, Progress: cfg.Progress})
	river.AddWorker(workers, &SyncReactionsOutWorker{DB: cfg.DB, LastfmCreds: cfg.LastfmCreds, Progress: cfg.Progress})

	// Debounce sweep — owns its own queue and fires every 10s via the
	// periodic-jobs entry below.
	river.AddWorker(workers, &DebounceSweepWorker{DB: cfg.DB})
	river.AddWorker(workers, &MetadataContinuationSweepWorker{DB: cfg.DB, Backoff: continuationBackoff})
	river.AddWorker(workers, &SyncMetadataChangesWorker{DB: cfg.DB, Source: cfg.HeyaMetadata})

	client, err := river.NewClient(riverpgxv5.New(cfg.DB), &river.Config{
		// Scanner stages are split by media type, so a large Music fan-out
		// cannot monopolize the workers needed by Movies, TV, or Anime. The
		// unsuffixed queues remain for scan-all coordination and as a safe
		// fallback for legacy or unknown media-type payloads.
		Queues: map[string]river.QueueConfig{
			// Scanner pipeline. Local disk analysis stays conservative; remote
			// submission and due continuation checks use independent queues.
			"kickoff_library_scan": {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)}, // priority bands: P1=watcher, P2=scheduled/manual
			"process_scan":         {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},         // local analysis only; scoped for watcher-triggered folders
			"search_metadata":      {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},      // canonical search + discovery submission
			"search_metadata_poll": {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)}, // scheduled discovery status checks
			"fetch_metadata":       {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},       // resolution submission + ready entity fetch
			"fetch_metadata_poll":  {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},  // scheduled resolution status checks
			"apply_metadata":       {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},       // materialize + apply from persisted fetch artifact

			"kickoff_library_scan_movie":   {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_tv":      {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_anime":   {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_music":   {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_book":    {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_comic":   {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_podcast": {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},
			"kickoff_library_scan_radio":   {MaxWorkers: queueWorkers(cfg, "kickoff_library_scan", 1)},

			"process_scan_movie":   {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_tv":      {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_anime":   {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_music":   {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_book":    {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_comic":   {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_podcast": {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},
			"process_scan_radio":   {MaxWorkers: queueWorkers(cfg, "process_scan", 4)},

			"search_metadata_movie":        {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_tv":           {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_anime":        {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_music":        {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_book":         {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_comic":        {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_podcast":      {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_radio":        {MaxWorkers: queueWorkers(cfg, "search_metadata", 4)},
			"search_metadata_poll_movie":   {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_tv":      {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_anime":   {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_music":   {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_book":    {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_comic":   {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_podcast": {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},
			"search_metadata_poll_radio":   {MaxWorkers: queueWorkers(cfg, "search_metadata_poll", 4)},

			"fetch_metadata_movie":        {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_tv":           {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_anime":        {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_music":        {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_book":         {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_comic":        {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_podcast":      {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_radio":        {MaxWorkers: queueWorkers(cfg, "fetch_metadata", 4)},
			"fetch_metadata_poll_movie":   {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_tv":      {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_anime":   {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_music":   {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_book":    {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_comic":   {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_podcast": {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},
			"fetch_metadata_poll_radio":   {MaxWorkers: queueWorkers(cfg, "fetch_metadata_poll", 4)},

			"apply_metadata_movie":   {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_tv":      {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_anime":   {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_music":   {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_book":    {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_comic":   {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_podcast": {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},
			"apply_metadata_radio":   {MaxWorkers: queueWorkers(cfg, "apply_metadata", 4)},

			"apply_rich_metadata": {MaxWorkers: queueWorkers(cfg, "apply_rich_metadata", 4)}, // local set-based projection writes; shared people are locked canonically and concurrency-tested
			"ffprobe":             {MaxWorkers: queueWorkers(cfg, "ffprobe", 1)},
			"detect_local_assets": {MaxWorkers: queueWorkers(cfg, "detect_local_assets", 1)},

			// Enrich pipeline (external rate-limit safety).
			"enrich_media_item":      {MaxWorkers: queueWorkers(cfg, "enrich_media_item", 1)}, // priority bands P1=watcher/view, P2=movies+tv, P3=music+books
			"person_fetch":           {MaxWorkers: queueWorkers(cfg, "person_fetch", 8)},      // I/O-bound on heya.media; now lazy (on-view backfill) so backlog is small, but let concurrent person-page visits parallelize. Slug-race in person_worker is guarded (retry-on-conflict merge).
			"ratings_fetch":          {MaxWorkers: queueWorkers(cfg, "ratings_fetch", 4)},     // per-item heya.media call, clean upserts, no cross-item state — safe to parallelize (semaphore-8 in the client is the real ceiling)
			"force_refresh_metadata": {MaxWorkers: queueWorkers(cfg, "force_refresh_metadata", 1)},
			"fetch_artwork":          {MaxWorkers: queueWorkers(cfg, "fetch_artwork", 4)}, // secondary artwork pass — heya.media call + pending asset rows (ON CONFLICT DO NOTHING), no cross-item state

			// Images.
			"download_image":       {MaxWorkers: queueWorkers(cfg, "download_image", 4)}, // hits CDN/heya.media, not source FS
			"save_images":          {MaxWorkers: queueWorkers(cfg, "save_images", 1)},
			"force_refresh_images": {MaxWorkers: queueWorkers(cfg, "force_refresh_images", 1)},

			// NFOs.
			"save_nfo":       {MaxWorkers: queueWorkers(cfg, "save_nfo", 1)},
			"save_music_nfo": {MaxWorkers: queueWorkers(cfg, "save_music_nfo", 1)},

			// CPU/heavy work (one at a time so they can't starve scans).
			"scan_track_loudness":    {MaxWorkers: queueWorkers(cfg, "scan_track_loudness", 1)},    // ebur128 ~10-20× real-time
			"scan_album_loudness":    {MaxWorkers: queueWorkers(cfg, "scan_album_loudness", 1)},    // concat demuxer + ebur128
			"scan_track_fingerprint": {MaxWorkers: queueWorkers(cfg, "scan_track_fingerprint", 1)}, // chromaprint, first 120s only

			// Skip segments — direct community-provider calls, not local
			// decode. Parallelized within a bounded queue; per-source caches and
			// HTTP timeouts keep a cold sweep
			// no longer needs to trickle. Job key is per-file (unique-while-
			// active), no cross-item state.
			"scan_media_segments_file": {MaxWorkers: queueWorkers(cfg, "scan_media_segments_file", 8)},
			"scan_keyframes":           {MaxWorkers: queueWorkers(cfg, "scan_keyframes", 1)},

			// Local skip-segment detection — the fallback pass for files
			// the community databases had nothing for. Real audio decode
			// (chromaprint cross-episode matching / ffmpeg blackdetect),
			// so each stays MaxWorkers=1 like the other CPU-heavy queues.
			"detect_segments_season": {MaxWorkers: queueWorkers(cfg, "detect_segments_season", 1)}, // cross-episode chromaprint matching — heaviest of the two
			"detect_segments_movie":  {MaxWorkers: queueWorkers(cfg, "detect_segments_movie", 1)},  // ffmpeg blackdetect over the tail window

			"trickplay":      {MaxWorkers: queueWorkers(cfg, "trickplay", 1)},      // ffmpeg sprites
			"thumbnails":     {MaxWorkers: queueWorkers(cfg, "thumbnails", 1)},     // ffmpeg thumbnail extraction
			"sonic_analysis": {MaxWorkers: queueWorkers(cfg, "sonic_analysis", 1)}, // full model bundle (Discogs heads + EffNet base + classifier heads + CLAP audio) held by AnalyzerHolder singleton; ~hundreds of MB resident
			"transcode":      {MaxWorkers: queueWorkers(cfg, "transcode", 1)},

			// Sonic centroid refreshes (cheap; own queue so they don't
			// block the next track analysis).
			"artist_centroid": {MaxWorkers: queueWorkers(cfg, "artist_centroid", 1)},
			"album_centroid":  {MaxWorkers: queueWorkers(cfg, "album_centroid", 1)},

			// Disk-usage scan — read-only walk of library paths to populate
			// the Storage page. Own queue so a multi-TB walk doesn't block
			// any other admin-triggered work.
			"scan_library_disk": {MaxWorkers: queueWorkers(cfg, "scan_library_disk", 1)},

			// Kickoffs (each their own queue, UniqueByArgs so click-spam
			// is a no-op while one is queued/running).
			"kickoff_refresh_stale":         {MaxWorkers: queueWorkers(cfg, "kickoff_refresh_stale", 1)},
			"kickoff_music_loudness":        {MaxWorkers: queueWorkers(cfg, "kickoff_music_loudness", 1)},
			"kickoff_music_fingerprint":     {MaxWorkers: queueWorkers(cfg, "kickoff_music_fingerprint", 1)},
			"kickoff_media_segments":        {MaxWorkers: queueWorkers(cfg, "kickoff_media_segments", 1)},
			"kickoff_detect_segments":       {MaxWorkers: queueWorkers(cfg, "kickoff_detect_segments", 1)},
			"kickoff_trickplay":             {MaxWorkers: queueWorkers(cfg, "kickoff_trickplay", 1)},
			"kickoff_thumbnails":            {MaxWorkers: queueWorkers(cfg, "kickoff_thumbnails", 1)},
			"kickoff_sonic_analysis":        {MaxWorkers: queueWorkers(cfg, "kickoff_sonic_analysis", 1)},
			"cleanup_scanner_artifacts":     {MaxWorkers: queueWorkers(cfg, "cleanup_scanner_artifacts", 1)},
			"kickoff_embed_recommendations": {MaxWorkers: queueWorkers(cfg, "kickoff_embed_recommendations", 1)},
			"kickoff_listen_import":         {MaxWorkers: queueWorkers(cfg, "kickoff_listen_import", 1)},
			"import_listens_batch":          {MaxWorkers: queueWorkers(cfg, "import_listens_batch", 4)},
			"kickoff_music_services_sync":   {MaxWorkers: queueWorkers(cfg, "kickoff_music_services_sync", 1)},
			"sync_reactions_out":            {MaxWorkers: queueWorkers(cfg, "sync_reactions_out", 1)},

			// Misc.
			"soft_delete":                 {MaxWorkers: queueWorkers(cfg, "soft_delete", 1)},
			"debounce_sweep":              {MaxWorkers: queueWorkers(cfg, "debounce_sweep", 1)}, // periodic sweep; trailing-edge debounce of child-content enriches
			"metadata_continuation_sweep": {MaxWorkers: 1},                                      // promotes a bounded due batch; intentionally not user-tunable
			"sync_metadata_changes":       {MaxWorkers: queueWorkers(cfg, "sync_metadata_changes", 1)},
			river.QueueDefault:            {MaxWorkers: queueWorkers(cfg, river.QueueDefault, 1)}, // fallback only; we shouldn't actually use it after the split
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
			river.NewPeriodicJob(
				river.PeriodicInterval(5*time.Second),
				func() (river.JobArgs, *river.InsertOpts) {
					return MetadataContinuationSweepArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(30*time.Second),
				func() (river.JobArgs, *river.InsertOpts) {
					return SyncMetadataChangesArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
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

func queueWorkers(cfg Config, kind string, fallback int) int {
	if cfg.WorkerCounts != nil {
		if n, ok := cfg.WorkerCounts[kind]; ok && n > 0 {
			return n
		}
	}
	return fallback
}
