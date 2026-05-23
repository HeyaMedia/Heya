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

type ProcessFileArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	LibraryID     int64  `json:"library_id"`
	FilePath      string `json:"file_path"`
}

func (ProcessFileArgs) Kind() string { return "process_file" }
func (ProcessFileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "process", MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type MetadataMatchArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	LibraryID     int64  `json:"library_id"`
	MediaType     string `json:"media_type"`
}

func (MetadataMatchArgs) Kind() string { return "metadata_match" }
func (MetadataMatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type MetadataFetchArgs struct {
	MediaItemID   int64  `json:"media_item_id"`
	LibraryID     int64  `json:"library_id"`
	LibraryFileID int64  `json:"library_file_id"`
	FilePath      string `json:"file_path"`
	MediaType     string `json:"media_type"`
	ProviderName  string `json:"provider_name"`
	ProviderID    string `json:"provider_id"`
}

func (MetadataFetchArgs) Kind() string { return "metadata_fetch" }
func (MetadataFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 3}
}

type PersonFetchArgs struct {
	PersonID int64  `json:"person_id"`
	TmdbID   int32  `json:"tmdb_id"`
	Language string `json:"language,omitempty"`
}

func (PersonFetchArgs) Kind() string { return "person_fetch" }
func (PersonFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "metadata",
		MaxAttempts: 3,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
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
	return river.InsertOpts{Queue: "images", MaxAttempts: 5}
}

type FFProbeArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	FilePath      string `json:"file_path"`
}

func (FFProbeArgs) Kind() string { return "ffprobe" }
func (FFProbeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "process", MaxAttempts: 3}
}

type PendingImage struct {
	URL       string `json:"url"`
	AssetType string `json:"asset_type"`
	Label     string `json:"label,omitempty"`
	SortOrder int    `json:"sort_order"`
	Priority  int    `json:"priority"`
}

type DetectLocalAssetsArgs struct {
	MediaItemID   int64          `json:"media_item_id"`
	LibraryFileID int64          `json:"library_file_id"`
	FilePath      string         `json:"file_path"`
	MediaType     string         `json:"media_type"`
	PendingImages []PendingImage `json:"pending_images,omitempty"`
	QueueEnrich   bool           `json:"queue_enrich,omitempty"`
	LibraryID     int64          `json:"library_id,omitempty"`
}

func (DetectLocalAssetsArgs) Kind() string { return "detect_local_assets" }
func (DetectLocalAssetsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "process", MaxAttempts: 3}
}

type EnrichmentArgs struct {
	MediaItemID int64  `json:"media_item_id"`
	MediaType   string `json:"media_type"`
}

func (EnrichmentArgs) Kind() string { return "enrichment" }
func (EnrichmentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 3}
}

type SaveNFOArgs struct {
	MediaItemID   int64  `json:"media_item_id"`
	LibraryFileID int64  `json:"library_file_id"`
	FilePath      string `json:"file_path"`
	MediaType     string `json:"media_type"`
}

func (SaveNFOArgs) Kind() string { return "save_nfo" }
func (SaveNFOArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "saver", MaxAttempts: 2}
}

type SaveImagesArgs struct {
	MediaItemID int64  `json:"media_item_id"`
	FilePath    string `json:"file_path"`
	CachedPath  string `json:"cached_path"`
	AssetType   string `json:"asset_type"`
}

func (SaveImagesArgs) Kind() string { return "save_images" }
func (SaveImagesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "saver", MaxAttempts: 2}
}

type RatingsFetchArgs struct {
	MediaItemID int64 `json:"media_item_id"`
	LibraryID   int64 `json:"library_id"`
}

func (RatingsFetchArgs) Kind() string { return "ratings_fetch" }
func (RatingsFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "ratings", MaxAttempts: 3}
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
	return river.InsertOpts{MaxAttempts: 3}
}

type ForceRefreshMetadataArgs struct {
	LibraryID int64 `json:"library_id"`
}

func (ForceRefreshMetadataArgs) Kind() string { return "force_refresh_metadata" }
func (ForceRefreshMetadataArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type ForceRefreshImagesArgs struct {
	LibraryID int64 `json:"library_id"`
}

func (ForceRefreshImagesArgs) Kind() string { return "force_refresh_images" }
func (ForceRefreshImagesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "images", MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}
