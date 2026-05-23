package metadata

import "encoding/json"

type LibrarySettings struct {
	Watch               bool   `json:"watch"`
	PreferredLanguage   string `json:"preferred_language"`
	PreferredCountry    string `json:"preferred_country"`
	AutoCollections     bool   `json:"auto_collections"`
	MetadataRefreshDays int    `json:"metadata_refresh_days"`
	FetchRatings        bool   `json:"fetch_ratings"`
	SaveNFO             bool   `json:"save_nfo"`
	SaveImages          bool   `json:"save_images"`
	EnableTrickplay     bool   `json:"enable_trickplay"`
	GenerateThumbnails  bool   `json:"generate_thumbnails"`
}

func DefaultSettings(mediaType string) LibrarySettings {
	base := LibrarySettings{
		Watch:             true,
		PreferredLanguage: "en",
		FetchRatings:      true,
	}
	switch mediaType {
	case "movie":
		base.PreferredCountry = "US"
		base.AutoCollections = true
		base.GenerateThumbnails = true
	case "tv":
		base.PreferredCountry = "US"
		base.GenerateThumbnails = true
	}
	return base
}

func ParseSettings(data []byte) LibrarySettings {
	if len(data) == 0 || string(data) == "{}" {
		return LibrarySettings{}
	}
	var s LibrarySettings
	json.Unmarshal(data, &s)
	return s
}

func (s LibrarySettings) MarshalJSON() ([]byte, error) {
	type alias LibrarySettings
	return json.Marshal(alias(s))
}

func (s LibrarySettings) IsEmpty() bool {
	return s.PreferredLanguage == "" && s.PreferredCountry == ""
}

func (s LibrarySettings) Merge(other LibrarySettings) LibrarySettings {
	if other.PreferredLanguage != "" {
		s.PreferredLanguage = other.PreferredLanguage
	}
	if other.PreferredCountry != "" {
		s.PreferredCountry = other.PreferredCountry
	}
	if other.MetadataRefreshDays != 0 {
		s.MetadataRefreshDays = other.MetadataRefreshDays
	}
	s.Watch = other.Watch
	s.AutoCollections = other.AutoCollections
	s.FetchRatings = other.FetchRatings
	s.SaveNFO = other.SaveNFO
	s.SaveImages = other.SaveImages
	s.EnableTrickplay = other.EnableTrickplay
	s.GenerateThumbnails = other.GenerateThumbnails
	return s
}
