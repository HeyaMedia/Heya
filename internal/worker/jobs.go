package worker

import "github.com/riverqueue/river"

type ScanLibraryArgs struct {
	LibraryID int64 `json:"library_id"`
	Force     bool  `json:"force"`
}

func (ScanLibraryArgs) Kind() string { return "scan_library" }
func (ScanLibraryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "scan", MaxAttempts: 3}
}

type ProcessFileArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	LibraryID     int64  `json:"library_id"`
	FilePath      string `json:"file_path"`
}

func (ProcessFileArgs) Kind() string { return "process_file" }
func (ProcessFileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "process", MaxAttempts: 3}
}

type MetadataMatchArgs struct {
	LibraryFileID int64  `json:"library_file_id"`
	LibraryID     int64  `json:"library_id"`
	MediaType     string `json:"media_type"`
}

func (MetadataMatchArgs) Kind() string { return "metadata_match" }
func (MetadataMatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 3}
}

type MetadataFetchArgs struct {
	MediaItemID  int64  `json:"media_item_id"`
	LibraryID    int64  `json:"library_id"`
	LibraryFileID int64 `json:"library_file_id"`
	FilePath     string `json:"file_path"`
	MediaType    string `json:"media_type"`
	ProviderName string `json:"provider_name"`
	ProviderID   string `json:"provider_id"`
}

func (MetadataFetchArgs) Kind() string { return "metadata_fetch" }
func (MetadataFetchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata", MaxAttempts: 3}
}

type PersonFetchArgs struct {
	PersonID int64 `json:"person_id"`
	TmdbID   int32 `json:"tmdb_id"`
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

type DetectLocalAssetsArgs struct {
	MediaItemID   int64  `json:"media_item_id"`
	LibraryFileID int64  `json:"library_file_id"`
	FilePath      string `json:"file_path"`
	MediaType     string `json:"media_type"`
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
	return river.InsertOpts{Queue: "scan", MaxAttempts: 3}
}
