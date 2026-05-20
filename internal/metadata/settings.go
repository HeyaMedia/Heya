package metadata

import "encoding/json"

type LibrarySettings struct {
	Watch               bool     `json:"watch"`
	MetadataProviders   []string `json:"metadata_providers"`
	ArtworkProviders    []string `json:"artwork_providers"`
	RatingsProviders    []string `json:"ratings_providers"`
	PreferredLanguage   string   `json:"preferred_language"`
	PreferredCountry    string   `json:"preferred_country"`
	AutoCollections     bool     `json:"auto_collections"`
	MetadataRefreshDays int      `json:"metadata_refresh_days"`
	SaveNFO             bool     `json:"save_nfo"`
	SaveImages          bool     `json:"save_images"`
}

func DefaultSettings(mediaType string) LibrarySettings {
	switch mediaType {
	case "movie":
		return LibrarySettings{
			Watch:             true,
			MetadataProviders: []string{"tmdb"},
			ArtworkProviders:  []string{"tmdb", "fanart.tv"},
			RatingsProviders:  []string{"omdb"},
			PreferredLanguage: "en",
			PreferredCountry:  "US",
			AutoCollections:   true,
		}
	case "tv":
		return LibrarySettings{
			Watch:             true,
			MetadataProviders: []string{"tmdb", "tvdb"},
			ArtworkProviders:  []string{"tmdb", "fanart.tv"},
			RatingsProviders:  []string{"omdb"},
			PreferredLanguage: "en",
			PreferredCountry:  "US",
		}
	case "music":
		return LibrarySettings{
			Watch:             true,
			MetadataProviders: []string{"musicbrainz"},
			PreferredLanguage: "en",
		}
	case "book":
		return LibrarySettings{
			Watch:             true,
			MetadataProviders: []string{"openlibrary"},
			PreferredLanguage: "en",
		}
	default:
		return LibrarySettings{
			Watch:             true,
			PreferredLanguage: "en",
		}
	}
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
	return len(s.MetadataProviders) == 0 &&
		len(s.ArtworkProviders) == 0 &&
		len(s.RatingsProviders) == 0 &&
		s.PreferredLanguage == "" &&
		s.PreferredCountry == ""
}

func (s LibrarySettings) Merge(other LibrarySettings) LibrarySettings {
	if len(other.MetadataProviders) > 0 {
		s.MetadataProviders = other.MetadataProviders
	}
	if len(other.ArtworkProviders) > 0 {
		s.ArtworkProviders = other.ArtworkProviders
	}
	if len(other.RatingsProviders) > 0 {
		s.RatingsProviders = other.RatingsProviders
	}
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
	s.SaveNFO = other.SaveNFO
	s.SaveImages = other.SaveImages
	return s
}
