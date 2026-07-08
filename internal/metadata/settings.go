package metadata

import "encoding/json"

type LibrarySettings struct {
	Watch              bool   `json:"watch"`
	PreferredLanguage  string `json:"preferred_language"`
	PreferredCountry   string `json:"preferred_country"`
	UseLocalData       bool   `json:"use_local_data"`
	AutoCollections    bool   `json:"auto_collections"`
	FetchRatings       bool   `json:"fetch_ratings"`
	SaveNFO            bool   `json:"save_nfo"`
	SaveImages         bool   `json:"save_images"`
	EnableTrickplay    bool   `json:"enable_trickplay"`
	GenerateThumbnails bool   `json:"generate_thumbnails"`
}

func DefaultSettings(mediaType string) LibrarySettings {
	base := LibrarySettings{
		Watch:             true,
		PreferredLanguage: "en",
		UseLocalData:      true,
		FetchRatings:      true,
	}
	switch mediaType {
	case "movie":
		base.PreferredCountry = "US"
		base.AutoCollections = true
		base.GenerateThumbnails = true
	case "tv", "anime":
		base.PreferredCountry = "US"
		base.GenerateThumbnails = true
	}
	return base
}

func ParseSettings(data []byte) LibrarySettings {
	if len(data) == 0 || string(data) == "{}" {
		return LibrarySettings{UseLocalData: true}
	}
	var s LibrarySettings
	json.Unmarshal(data, &s)
	return s
}

func (s *LibrarySettings) UnmarshalJSON(data []byte) error {
	type alias LibrarySettings
	next := alias{UseLocalData: true}
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	*s = LibrarySettings(next)
	return nil
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
	s.Watch = other.Watch
	s.AutoCollections = other.AutoCollections
	s.UseLocalData = other.UseLocalData
	s.FetchRatings = other.FetchRatings
	s.SaveNFO = other.SaveNFO
	s.SaveImages = other.SaveImages
	s.EnableTrickplay = other.EnableTrickplay
	s.GenerateThumbnails = other.GenerateThumbnails
	return s
}
