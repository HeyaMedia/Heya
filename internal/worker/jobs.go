package worker

import "github.com/riverqueue/river"

var SingleAssetTypes = map[string]bool{
	"poster":   true,
	"logo":     true,
	"art":      true,
	"banner":   true,
	"thumb":    true,
	"disc":     true,
	"clearart": true,
}

// Job priority bands. River runs higher-priority (lower number) jobs first
// within a queue. The watcher's new-file path overrides ProcessFile to
// PriorityWatcher at insert-time.
const (
	PriorityWatcher    = 1 // fsnotify-discovered file — user just touched this, run ASAP
	PriorityMatch      = 1 // metadata_match / metadata_fetch — matching is the critical path
	PriorityScan       = 2 // bulk library-scan ProcessFile + matching support jobs
	PriorityEnrichment = 3 // ffprobe / images / nfo writing / ratings — happens after match
	PriorityAnalysis   = 4 // ebur128, future ML / fingerprinting — runs whenever spare
)

type ProcessFileArgs struct {
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	LibraryID       int64  `json:"library_id"`
	FilePath        string `json:"file_path"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ProcessFileArgs) Kind() string { return "process_file" }
func (ProcessFileArgs) InsertOpts() river.InsertOpts {
	// Default to bulk-scan priority; watcher path overrides to PriorityWatcher
	// at insert-time so single-file changes jump ahead of any in-flight scan.
	return river.InsertOpts{Queue: "process_file", MaxAttempts: 3, Priority: PriorityScan, UniqueOpts: uniqueWhileActive()}
}

type MetadataMatchArgs struct {
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	LibraryID       int64  `json:"library_id"`
	MediaType       string `json:"media_type"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (MetadataMatchArgs) Kind() string { return "metadata_match" }
func (MetadataMatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata_match", MaxAttempts: 3, Priority: PriorityMatch, UniqueOpts: uniqueWhileActive()}
}

type PersonFetchArgs struct {
	PersonID int64  `json:"person_id"`
	TmdbID   int32  `json:"tmdb_id"`
	Language string `json:"language,omitempty"`
}

func (PersonFetchArgs) Kind() string { return "person_fetch" }
func (PersonFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "person_fetch",
		MaxAttempts: 3,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type DownloadImageArgs struct {
	MediaItemID int64  `json:"media_item_id"`
	PersonID    int64  `json:"person_id,omitempty"`
	EntityType  string `json:"entity_type"`
	URL         string `json:"url"`
	AssetType   string `json:"asset_type"`
	MediaType   string `json:"media_type"`
	Label       string `json:"label"`
	SortOrder   int    `json:"sort_order"`
}

func (DownloadImageArgs) Kind() string { return "download_image" }
func (DownloadImageArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "download_image", MaxAttempts: 5, Priority: PriorityEnrichment}
}

type FFProbeArgs struct {
	// LibraryFileID is the sole uniqueness key (river:"unique" — file_path and
	// scheduled_task_id are ignored), so at most one ffprobe job per file is
	// active at a time. That lets the scan re-enqueue a probe for a file whose
	// first attempt failed without stacking duplicates while one is still
	// queued/running; once the job reaches a terminal state it can be re-run.
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	FilePath        string `json:"file_path"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (FFProbeArgs) Kind() string { return "ffprobe" }
func (FFProbeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "ffprobe",
		MaxAttempts: 3,
		Priority:    PriorityEnrichment,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type PendingImage struct {
	URL       string `json:"url"`
	AssetType string `json:"asset_type"`
	Label     string `json:"label,omitempty"`
	SortOrder int    `json:"sort_order"`
	Priority  int    `json:"priority"`
}

type DetectLocalAssetsArgs struct {
	MediaItemID     int64          `json:"media_item_id"`
	LibraryFileID   int64          `json:"library_file_id"`
	FilePath        string         `json:"file_path"`
	MediaType       string         `json:"media_type"`
	PendingImages   []PendingImage `json:"pending_images,omitempty"`
	QueueEnrich     bool           `json:"queue_enrich,omitempty"`
	LibraryID       int64          `json:"library_id,omitempty"`
	ScheduledTaskID string         `json:"scheduled_task_id,omitempty"`
}

func (DetectLocalAssetsArgs) Kind() string { return "detect_local_assets" }
func (DetectLocalAssetsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "detect_local_assets", MaxAttempts: 3, Priority: PriorityEnrichment}
}

// FetchArtworkArgs runs the secondary artwork pass — a follow-up call
// to heya.FetchArtwork that returns the full artwork catalogue (up to
// 5 backdrops + the alternates that didn't make MediaDetail.Pending
// Images). enrich_media_item fans out the *primary* poster/backdrop
// from GetDetail via detect_local_assets → download_image; this
// worker is the long-tail pass for items the user actually opens.
//
// Triggered by detect_local_assets (when QueueEnrich is set on the
// args) and by metadata_editor after a user changes the match.
// Cheap enough that we don't enqueue from refresh paths — those go
// through enrich_media_item which calls detect_local_assets.
type FetchArtworkArgs struct {
	MediaItemID int64  `json:"media_item_id"`
	MediaType   string `json:"media_type"`
}

func (FetchArtworkArgs) Kind() string { return "fetch_artwork" }
func (FetchArtworkArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "fetch_artwork", MaxAttempts: 3, Priority: PriorityEnrichment}
}

// EnrichMediaItemArgs is the unified enrich job — replaces MetadataFetchArgs,
// RefreshMusicArtistArgs, and the secondary artwork pass that EnrichmentArgs
// covered. One worker, dispatched by media_type. The job only carries the
// item ID; everything else is looked up so callers don't have to plumb
// provider IDs / library IDs / file paths through their call chain.
//
// Callers should enqueue via service.EnqueueEnrich, which sets the River
// priority based on (source, media_type) per service.PriorityFor.
//
// BatchLibraryID / BatchTotal / BatchPosition are optional batch-context
// fields used by the post-scan music fan-out so the worker can emit
// "Refreshing 17/200 (Calvin Harris)" progress events without consulting
// River's job table.
type EnrichMediaItemArgs struct {
	// NOTE: deliberately NOT unique-while-active. River's unique dedup returns
	// the pre-existing job WITHOUT bumping its priority, so a P1 view-promotion
	// enrich (EnrichSourceView) would be swallowed by an already-queued P2/P3
	// scan enrich for the same item and never run at high priority. Coalescing is
	// instead handled priority-safely by the enrich idempotency gate + the
	// debounce table; the stale/failed re-drive is bounded (scheduled cadence,
	// fast-completing jobs, unique kickoff).
	ItemID          int64  `json:"item_id"`
	Source          string `json:"source,omitempty"`
	Force           bool   `json:"force,omitempty"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`

	BatchLibraryID int64 `json:"batch_library_id,omitempty"`
	BatchTotal     int   `json:"batch_total,omitempty"`
	BatchPosition  int   `json:"batch_position,omitempty"`
}

func (EnrichMediaItemArgs) Kind() string { return "enrich_media_item" }
func (EnrichMediaItemArgs) InsertOpts() river.InsertOpts {
	// Default priority is the middle band (movies/TV). service.EnqueueEnrich
	// overrides per-insert with the correct priority for the (source,
	// media_type) combination. Priority bands within this queue:
	// P1=watcher/view, P2=movies+tv, P3=music+books, P4=analysis.
	return river.InsertOpts{Queue: "enrich_media_item", MaxAttempts: 3, Priority: 2}
}

type SaveNFOArgs struct {
	MediaItemID   int64  `json:"media_item_id"`
	LibraryFileID int64  `json:"library_file_id"`
	FilePath      string `json:"file_path"`
	MediaType     string `json:"media_type"`
}

func (SaveNFOArgs) Kind() string { return "save_nfo" }
func (SaveNFOArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "save_nfo", MaxAttempts: 2, Priority: PriorityEnrichment}
}

type SaveImagesArgs struct {
	MediaItemID int64  `json:"media_item_id"`
	FilePath    string `json:"file_path"`
	CachedPath  string `json:"cached_path"`
	AssetType   string `json:"asset_type"`
}

func (SaveImagesArgs) Kind() string { return "save_images" }
func (SaveImagesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "save_images", MaxAttempts: 2, Priority: PriorityEnrichment}
}

type RatingsFetchArgs struct {
	MediaItemID int64 `json:"media_item_id"`
	LibraryID   int64 `json:"library_id"`
}

func (RatingsFetchArgs) Kind() string { return "ratings_fetch" }
func (RatingsFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "ratings_fetch", MaxAttempts: 3, Priority: PriorityEnrichment}
}

type TranscodeArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	Profile       string `json:"profile"`
}

func (TranscodeArgs) Kind() string { return "transcode" }
func (TranscodeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "transcode", MaxAttempts: 2}
}

type SoftDeleteArgs struct {
	LibraryID int64    `json:"library_id"`
	Paths     []string `json:"paths"`
}

func (SoftDeleteArgs) Kind() string { return "soft_delete" }
func (SoftDeleteArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "soft_delete", MaxAttempts: 3}
}

type ForceRefreshMetadataArgs struct {
	LibraryID int64 `json:"library_id"`
}

func (ForceRefreshMetadataArgs) Kind() string { return "force_refresh_metadata" }
func (ForceRefreshMetadataArgs) InsertOpts() river.InsertOpts {
	// uniqueWhileActive (not the default ByArgs bitmask): this is the
	// per-library "Refresh metadata" button. The user must be able to
	// re-run it after a previous refresh finished — dedup only while one
	// is still in flight.
	return river.InsertOpts{Queue: "force_refresh_metadata", MaxAttempts: 1, UniqueOpts: uniqueWhileActive()}
}

type ForceRefreshImagesArgs struct {
	LibraryID int64 `json:"library_id"`
}

func (ForceRefreshImagesArgs) Kind() string { return "force_refresh_images" }
func (ForceRefreshImagesArgs) InsertOpts() river.InsertOpts {
	// Re-runnable button — see ForceRefreshMetadataArgs.
	return river.InsertOpts{Queue: "force_refresh_images", MaxAttempts: 1, UniqueOpts: uniqueWhileActive()}
}

// ScanLibraryDiskArgs walks every path of a library and persists per-path
// byte totals into library_disk_usage. The walk is bounded by the library
// path tree; on a 10TB library this can take a few minutes, which is why it
// runs as a background job rather than inline in the Storage page request.
// uniqueWhileActive means click-spamming "Scan disk usage" while one is
// queued or running is a no-op, but the walk stays re-runnable once the
// previous one has finished (the default ByArgs bitmask would keep
// deduping against the completed row until River's job cleaner removes it).
type ScanLibraryDiskArgs struct {
	LibraryID int64 `json:"library_id"`
}

func (ScanLibraryDiskArgs) Kind() string { return "scan_library_disk" }
func (ScanLibraryDiskArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "scan_library_disk",
		MaxAttempts: 1,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// SaveMusicNFOArgs writes artist.nfo + album.nfo files for one artist's
// release tree. Triggered by EnrichMediaItemWorker (music branch) when the
// library's SaveNFO setting is on. Scoped to one artist so a refresh that
// only touched Calvin Harris doesn't rewrite Ado's NFOs unnecessarily.
type SaveMusicNFOArgs struct {
	ArtistID int64 `json:"artist_id"`
}

func (SaveMusicNFOArgs) Kind() string { return "save_music_nfo" }
func (SaveMusicNFOArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "save_music_nfo", MaxAttempts: 2, Priority: PriorityEnrichment, UniqueOpts: uniqueWhileActive()}
}

// ScanTrackLoudnessArgs runs an ebur128 pass on a single audio file and
// writes integrated_lufs / true_peak_db / loudness_range_db / sample_peak_db
// back to its track_files row. CPU-bound, runs on its own `loudness` queue
// at MaxWorkers=1 so it can't starve the rest of the pipeline.
type ScanTrackLoudnessArgs struct {
	TrackFileID     int64  `json:"track_file_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ScanTrackLoudnessArgs) Kind() string { return "scan_track_loudness" }
func (ScanTrackLoudnessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "scan_track_loudness",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ScanTrackFingerprintArgs computes a chromaprint audio fingerprint for a
// single file and writes it back to its track_files row. Light CPU work
// (decodes ≤120s), runs on its own queue at MaxWorkers=1 like the other
// analysis passes.
type ScanTrackFingerprintArgs struct {
	TrackFileID     int64  `json:"track_file_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ScanTrackFingerprintArgs) Kind() string { return "scan_track_fingerprint" }
func (ScanTrackFingerprintArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "scan_track_fingerprint",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ScanMediaSegmentsFileArgs fetches community skip segments (intro /
// recap / credits markers) for one movie/episode file from heya.media
// and stores the duration-gated winners in media_segments. Pure network
// work — no local decode.
type ScanMediaSegmentsFileArgs struct {
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ScanMediaSegmentsFileArgs) Kind() string { return "scan_media_segments_file" }
func (ScanMediaSegmentsFileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "scan_media_segments_file",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ScanAlbumLoudnessArgs concatenates every primary track file in an album
// via ffmpeg's concat demuxer and runs ebur128 over the union — the correct
// way to measure album loudness (averaging per-track LUFS is mathematically
// wrong since LUFS is logarithmic). Enqueued by ScanTrackLoudnessWorker when
// every track in the album has finished individually.
type ScanAlbumLoudnessArgs struct {
	AlbumID         int64  `json:"album_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ScanAlbumLoudnessArgs) Kind() string { return "scan_album_loudness" }
func (ScanAlbumLoudnessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "scan_album_loudness",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// DetectSeasonSegmentsArgs runs Intro-Skipper-style local detection for one
// (series, season) pair: chromaprint-fingerprints every pending episode's
// intro and tail windows, pairs episodes to find their shared regions, and
// writes any resolved intro/credits rows. Heavier than the community
// segments fetch (real audio decode, not just an HTTP round-trip) so it
// gets its own queue at MaxWorkers=1; DetectSeasonSegmentsWorker overrides
// Timeout() to run unbounded since a 25-episode season is many minutes of
// decoding and River's default per-job timeout would kill it mid-run.
type DetectSeasonSegmentsArgs struct {
	MediaItemID     int64  `json:"media_item_id" river:"unique"`
	Season          int    `json:"season" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (DetectSeasonSegmentsArgs) Kind() string { return "detect_segments_season" }
func (DetectSeasonSegmentsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "detect_segments_season",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// DetectMovieCreditsArgs runs ffmpeg blackdetect over one movie's tail
// window to find its credits cut when the community databases had nothing.
// DetectMovieCreditsWorker also overrides Timeout() to run unbounded — an
// 8-minute tail window read over a slow SMB share can outlast River's
// default per-job deadline.
type DetectMovieCreditsArgs struct {
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (DetectMovieCreditsArgs) Kind() string { return "detect_segments_movie" }
func (DetectMovieCreditsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "detect_segments_movie",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}
