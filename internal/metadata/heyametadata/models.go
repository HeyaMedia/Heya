package heyametadata

type canonicalHeader struct {
	SchemaVersion     int          `json:"schema_version"`
	ProjectionVersion int64        `json:"projection_version"`
	ID                string       `json:"id"`
	Kind              string       `json:"kind"`
	Slug              string       `json:"slug"`
	ExternalIDs       []ExternalID `json:"external_ids"`
	// Freshness carries per-provider collection state — the keys are the
	// canonical answer to "which providers fed this document".
	Freshness struct {
		Providers map[string]struct {
			State string `json:"state"`
		} `json:"providers"`
	} `json:"freshness"`
}

type localizedText struct {
	Value    string `json:"value"`
	Language string `json:"language"`
	Country  string `json:"country"`
	Type     string `json:"type"`
}

type rating struct {
	System   string  `json:"system"`
	Provider string  `json:"provider"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int     `json:"votes"`
	RawValue string  `json:"raw_value"`
}

type image struct {
	ID            string  `json:"id"`
	Class         string  `json:"class"`
	Language      string  `json:"language"`
	Country       string  `json:"country"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Provider      string  `json:"provider"`
	ProviderScore float64 `json:"provider_score"`
}

type credit struct {
	PersonEntityID   string `json:"person_entity_id"`
	Provider         string `json:"provider"`
	ProviderPersonID string `json:"provider_person_id"`
	DisplayName      string `json:"display_name"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character"`
	Department       string `json:"department"`
	Job              string `json:"job"`
	Order            int    `json:"order"`
	ProfileImageID   string `json:"profile_image_id"`
}

type movieDocument struct {
	canonicalHeader
	Display struct {
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title"`
		Year          int    `json:"year"`
		ImageID       string `json:"image_id"`
	} `json:"display"`
	Data struct {
		Titles         []localizedText `json:"titles"`
		Overviews      []localizedText `json:"overviews"`
		Taglines       []localizedText `json:"taglines"`
		Classification struct {
			Genres            []string `json:"genres"`
			Keywords          []string `json:"keywords"`
			OriginalLanguage  string   `json:"original_language"`
			SpokenLanguages   []string `json:"spoken_languages"`
			Countries         []string `json:"countries"`
			AnimationEvidence bool     `json:"animation_evidence"`
		} `json:"classification"`
		Release struct {
			RawStatus        string `json:"raw_status"`
			NormalizedStatus string `json:"normalized_status"`
			ReleaseEvents    []struct {
				Country       string `json:"country"`
				Type          string `json:"type"`
				Date          string `json:"date"`
				Certification string `json:"certification"`
				Note          string `json:"note"`
			} `json:"release_events"`
		} `json:"release"`
		Measurements struct {
			RuntimeMinutes *int `json:"runtime_minutes"`
			Budget         *struct {
				Amount int64 `json:"amount"`
			} `json:"budget"`
			Revenue *struct {
				Amount int64 `json:"amount"`
			} `json:"revenue"`
			Popularity *float64 `json:"popularity"`
		} `json:"measurements"`
		Ratings []rating `json:"ratings"`
		Links   []struct {
			Kind     string `json:"kind"`
			Value    string `json:"value"`
			Language string `json:"language"`
			Country  string `json:"country"`
		} `json:"links"`
		Videos []struct {
			Host        string `json:"host"`
			Key         string `json:"key"`
			Type        string `json:"type"`
			Name        string `json:"name"`
			Language    string `json:"language"`
			Country     string `json:"country"`
			Official    bool   `json:"official"`
			PublishedAt string `json:"published_at"`
		} `json:"videos"`
		Studios []struct {
			ProviderID  string `json:"provider_id"`
			Name        string `json:"name"`
			Role        string `json:"role"`
			Country     string `json:"country"`
			LogoImageID string `json:"logo_image_id"`
		} `json:"studios"`
		Credits    []credit `json:"credits"`
		Images     []image  `json:"images"`
		Collection *struct {
			ProviderID string  `json:"provider_id"`
			Name       string  `json:"name"`
			Overview   string  `json:"overview"`
			Images     []image `json:"images"`
			Members    []struct {
				ProviderID string `json:"provider_id"`
				Title      string `json:"title"`
				Year       int    `json:"year"`
				ImageID    string `json:"image_id"`
				Order      int    `json:"order"`
			} `json:"members"`
		} `json:"collection"`
		Recommendations []struct {
			EntityID         string  `json:"entity_id"`
			Provider         string  `json:"provider"`
			ProviderTargetID string  `json:"provider_target_id"`
			Title            string  `json:"title"`
			Year             int     `json:"year"`
			ImageID          string  `json:"image_id"`
			ProviderScore    float64 `json:"provider_score"`
		} `json:"recommendations"`
	} `json:"data"`
}

type episodeNumber struct {
	Scheme   string  `json:"scheme"`
	Season   int     `json:"season"`
	Number   float64 `json:"number"`
	Provider string  `json:"provider"`
}

type episodicEpisode struct {
	ID             string          `json:"id"`
	SeasonID       string          `json:"season_id"`
	ProviderID     string          `json:"provider_id"`
	ExternalIDs    []ExternalID    `json:"external_ids"`
	Titles         []localizedText `json:"titles"`
	Overviews      []localizedText `json:"overviews"`
	Numbers        []episodeNumber `json:"numbers"`
	IsSpecial      bool            `json:"is_special"`
	EpisodeType    string          `json:"episode_type"`
	AirDate        string          `json:"air_date"`
	RuntimeMinutes int             `json:"runtime_minutes"`
	Summary        string          `json:"summary"`
	Ratings        []rating        `json:"ratings"`
	Images         []image         `json:"images"`
}

type episodicSeason struct {
	ID                string          `json:"id"`
	ProviderID        string          `json:"provider_id"`
	Number            int             `json:"number"`
	Name              string          `json:"name"`
	Titles            []localizedText `json:"titles"`
	Overviews         []localizedText `json:"overviews"`
	Status            string          `json:"status"`
	EpisodeOrder      int             `json:"episode_order"`
	EpisodeCount      int             `json:"episode_count"`
	AiredEpisodeCount int             `json:"aired_episode_count"`
	PremiereDate      string          `json:"premiere_date"`
	EndDate           string          `json:"end_date"`
	ExternalIDs       []ExternalID    `json:"external_ids"`
	Images            []image         `json:"images"`
	EpisodeIDs        []string        `json:"episode_ids"`
}

type episodicDocument struct {
	canonicalHeader
	Display struct {
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title"`
		Year          int    `json:"year"`
		ImageID       string `json:"image_id"`
	} `json:"display"`
	Data struct {
		Titles         []localizedText `json:"titles"`
		Overview       string          `json:"overview"`
		Overviews      []localizedText `json:"overviews"`
		Classification struct {
			Format         string   `json:"format"`
			Status         string   `json:"status"`
			Language       string   `json:"language"`
			Countries      []string `json:"countries"`
			Genres         []string `json:"genres"`
			SourceMaterial string   `json:"source_material"`
		} `json:"classification"`
		Lifecycle struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"lifecycle"`
		RuntimeMinutes int `json:"runtime_minutes"`
		EpisodeCount   int `json:"episode_count"`
		SeasonCount    int `json:"season_count"`
		Networks       []struct {
			EntityID    string       `json:"entity_id"`
			Name        string       `json:"name"`
			Country     string       `json:"country"`
			Type        string       `json:"type"`
			ExternalIDs []ExternalID `json:"external_ids"`
			LogoImageID string       `json:"logo_image_id"`
		} `json:"networks"`
		Studios       []string `json:"studios"`
		Organizations []struct {
			EntityID    string       `json:"entity_id"`
			Name        string       `json:"name"`
			Country     string       `json:"country"`
			Type        string       `json:"type"`
			ExternalIDs []ExternalID `json:"external_ids"`
			LogoImageID string       `json:"logo_image_id"`
		} `json:"organizations"`
		Keywords []string                     `json:"keywords"`
		Seasons  []episodicSeason             `json:"seasons"`
		Episodes []episodicEpisode            `json:"episodes"`
		Images   []image                      `json:"images"`
		Ratings  []rating                     `json:"ratings"`
		Credits  []credit                     `json:"credits"`
		Links    []struct{ Type, URL string } `json:"links"`
		Videos   []struct {
			Provider string `json:"provider"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			Key      string `json:"key"`
			URL      string `json:"url"`
			Language string `json:"language"`
			Country  string `json:"country"`
			Official bool   `json:"official"`
		} `json:"videos"`
		Certifications []struct {
			System      string `json:"system"`
			Country     string `json:"country"`
			Rating      string `json:"rating"`
			Description string `json:"description"`
			Order       int    `json:"order"`
		} `json:"certifications"`
		Recommendations []struct {
			Provider      string       `json:"provider"`
			ProviderID    string       `json:"provider_id"`
			EntityID      string       `json:"entity_id"`
			Title         string       `json:"title"`
			OriginalTitle string       `json:"original_title"`
			FirstAirDate  string       `json:"first_air_date"`
			ExternalIDs   []ExternalID `json:"external_ids"`
			ImageID       string       `json:"image_id"`
			ProviderScore float64      `json:"provider_score"`
		} `json:"recommendations"`
	} `json:"data"`
}

type artistDocument struct {
	canonicalHeader
	Display struct {
		Name           string `json:"name"`
		Disambiguation string `json:"disambiguation"`
		ImageID        string `json:"image_id"`
	} `json:"display"`
	Data struct {
		Names []struct {
			Value     string `json:"value"`
			SortValue string `json:"sort_value"`
			Language  string `json:"language"`
			Type      string `json:"type"`
			Primary   bool   `json:"primary"`
		} `json:"names"`
		Classification struct {
			ArtistType string `json:"artist_type"`
			Gender     string `json:"gender"`
		} `json:"classification"`
		Lifecycle struct {
			Dates []struct {
				Value     string `json:"value"`
				Precision string `json:"precision"`
				Type      string `json:"type"`
			} `json:"dates"`
			Ended *bool `json:"ended"`
		} `json:"lifecycle"`
		Areas []struct {
			ProviderID string   `json:"provider_id"`
			Name       string   `json:"name"`
			SortName   string   `json:"sort_name"`
			Role       string   `json:"role"`
			ISOCodes   []string `json:"iso_codes"`
		} `json:"areas"`
		Biographies []localizedText `json:"biographies"`
		Annotations []localizedText `json:"annotations"`
		Genres      []struct {
			Name       string  `json:"name"`
			Weight     float64 `json:"weight"`
			Provider   string  `json:"provider"`
			ProviderID string  `json:"provider_id"`
		} `json:"genres"`
		Tags []struct {
			Name       string  `json:"name"`
			Weight     float64 `json:"weight"`
			Provider   string  `json:"provider"`
			ProviderID string  `json:"provider_id"`
		} `json:"tags"`
		Links []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"links"`
		Images  []image `json:"images"`
		Metrics []struct {
			Name     string  `json:"name"`
			Value    float64 `json:"value"`
			RawValue string  `json:"raw_value"`
			Provider string  `json:"provider"`
		} `json:"metrics"`
		Relationships []struct {
			Type            string   `json:"type"`
			Direction       string   `json:"direction"`
			TargetProvider  string   `json:"target_provider"`
			TargetNamespace string   `json:"target_namespace"`
			TargetID        string   `json:"target_id"`
			TargetName      string   `json:"target_name"`
			BeginDate       string   `json:"begin_date"`
			EndDate         string   `json:"end_date"`
			Ended           *bool    `json:"ended"`
			Attributes      []string `json:"attributes"`
			Provider        string   `json:"provider"`
		} `json:"relationships"`
		SimilarArtists []struct {
			ProviderID string  `json:"provider_id"`
			Name       string  `json:"name"`
			URL        string  `json:"url"`
			Score      float64 `json:"score"`
			Provider   string  `json:"provider"`
		} `json:"similar_artists"`
		// MusicVideos are artist-scoped YouTube links (source: audiodb).
		// No recording ids upstream — track_title is the only association.
		MusicVideos []struct {
			Provider        string `json:"provider"`
			ProviderVideoID string `json:"provider_video_id"`
			TrackTitle      string `json:"track_title"`
			URL             string `json:"url"`
			Description     string `json:"description"`
		} `json:"music_videos"`
	} `json:"data"`
}

type releaseGroupDocument struct {
	canonicalHeader
	Display struct {
		Title          string `json:"title"`
		ArtistCredit   string `json:"artist_credit"`
		Year           int    `json:"year"`
		ImageID        string `json:"image_id"`
		Disambiguation string `json:"disambiguation"`
	} `json:"display"`
	Data struct {
		Titles []struct {
			Value    string `json:"value"`
			Language string `json:"language"`
			Type     string `json:"type"`
		} `json:"titles"`
		ArtistCredits  []artistCredit `json:"artist_credits"`
		Classification struct {
			PrimaryType    string   `json:"primary_type"`
			SecondaryTypes []string `json:"secondary_types"`
		} `json:"classification"`
		Dates []struct {
			Value string `json:"value"`
			Type  string `json:"type"`
		} `json:"dates"`
		Genres   []weightedTerm `json:"genres"`
		Tags     []weightedTerm `json:"tags"`
		Ratings  []rating       `json:"ratings"`
		Images   []image        `json:"images"`
		Editions []struct {
			Provider   string `json:"provider"`
			Namespace  string `json:"namespace"`
			ProviderID string `json:"provider_id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			Date       struct {
				Value string `json:"value"`
			} `json:"date"`
			Country    string   `json:"country"`
			Barcode    string   `json:"barcode"`
			TrackCount int      `json:"track_count"`
			DurationMS int64    `json:"duration_ms"`
			Formats    []string `json:"formats"`
			ImageID    string   `json:"image_id"`
			// Labels come from MusicBrainz/Discogs (with catalog numbers)
			// and Deezer (name-only); Link is the provider's own album page
			// (Bandcamp editions).
			Labels []struct {
				ProviderID    string `json:"provider_id"`
				Name          string `json:"name"`
				CatalogNumber string `json:"catalog_number"`
			} `json:"labels"`
			Link string `json:"link"`
		} `json:"editions"`
		// Descriptions/annotations/metrics — TheAudioDB (2026-07 expansion)
		// ships localized descriptions, an editorial provider_review
		// annotation, and a sales metric alongside the older sources.
		Descriptions []localizedText `json:"descriptions"`
		Annotations  []localizedText `json:"annotations"`
		Metrics      []struct {
			Name     string  `json:"name"`
			Value    float64 `json:"value"`
			RawValue string  `json:"raw_value"`
			Provider string  `json:"provider"`
		} `json:"metrics"`
	} `json:"data"`
}

type artistCredit struct {
	Position        int    `json:"position"`
	Name            string `json:"name"`
	JoinPhrase      string `json:"join_phrase"`
	ArtistProvider  string `json:"artist_provider"`
	ArtistNamespace string `json:"artist_namespace"`
	ArtistID        string `json:"artist_id"`
	ArtistName      string `json:"artist_name"`
}

type weightedTerm struct {
	Name       string  `json:"name"`
	Weight     float64 `json:"weight"`
	Count      int     `json:"count"`
	Provider   string  `json:"provider"`
	ProviderID string  `json:"provider_id"`
}

type releaseDocument struct {
	canonicalHeader
	Display struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	} `json:"display"`
	Data struct {
		Title          string         `json:"title"`
		Disambiguation string         `json:"disambiguation"`
		Status         string         `json:"status"`
		Quality        string         `json:"quality"`
		Packaging      string         `json:"packaging"`
		Date           string         `json:"date"`
		Country        string         `json:"country"`
		Barcode        string         `json:"barcode"`
		ArtistCredits  []artistCredit `json:"artist_credits"`
		// Language/Script/ASIN + release_events are the 2026-07 provider
		// expansion's issued-release facts (mergeIssuedRelease).
		Language      string                       `json:"language"`
		Script        string                       `json:"script"`
		ASIN          string                       `json:"asin"`
		Genres        []weightedTerm               `json:"genres"`
		Tags          []weightedTerm               `json:"tags"`
		Links         []struct{ Type, URL string } `json:"links"`
		ReleaseEvents []struct {
			Date struct {
				Value string `json:"value"`
			} `json:"date"`
			Country string `json:"country"`
		} `json:"release_events"`
		Labels []struct {
			ProviderID    string `json:"provider_id"`
			Name          string `json:"name"`
			CatalogNumber string `json:"catalog_number"`
		} `json:"labels"`
		Media []struct {
			ID         string `json:"id"`
			Position   int    `json:"position"`
			Title      string `json:"title"`
			Format     string `json:"format"`
			TrackCount int    `json:"track_count"`
			Tracks     []struct {
				ID                string         `json:"id"`
				RecordingEntityID string         `json:"recording_entity_id"`
				LyricsAvailable   bool           `json:"lyrics_available"`
				ProviderID        string         `json:"provider_id"`
				Position          string         `json:"position"`
				Number            string         `json:"number"`
				Title             string         `json:"title"`
				Sequence          int            `json:"sequence"`
				DurationMS        int64          `json:"duration_ms"`
				ArtistCredits     []artistCredit `json:"artist_credits"`
				Recording         struct {
					ID         string   `json:"id"`
					Provider   string   `json:"provider"`
					Namespace  string   `json:"namespace"`
					ProviderID string   `json:"provider_id"`
					Title      string   `json:"title"`
					DurationMS int64    `json:"duration_ms"`
					ISRCs      []string `json:"isrcs"`
				} `json:"recording"`
			} `json:"tracks"`
		} `json:"media"`
	} `json:"data"`
}

type bookDocument struct {
	canonicalHeader
	Display struct {
		Title   string `json:"title"`
		Year    int    `json:"year"`
		ImageID string `json:"image_id"`
	} `json:"display"`
	Data struct {
		Subtitle    string `json:"subtitle"`
		Description string `json:"description"`
		Authors     []struct {
			ID          string       `json:"id"`
			Name        string       `json:"name"`
			ExternalIDs []ExternalID `json:"external_ids"`
		} `json:"authors"`
		Subjects         []string `json:"subjects"`
		Languages        []string `json:"languages"`
		FirstPublishYear int      `json:"first_publish_year"`
		PublishedDate    string   `json:"published_date"`
		Publishers       []string `json:"publishers"`
		ISBN10           []string `json:"isbn_10"`
		ISBN13           []string `json:"isbn_13"`
		Format           string   `json:"format"`
		PageCount        int      `json:"page_count"`
		Ratings          []rating `json:"ratings"`
		Series           []struct {
			EntityID   string `json:"entity_id"`
			ProviderID string `json:"provider_id"`
			Name       string `json:"name"`
			Position   string `json:"position"`
			Provider   string `json:"provider"`
			Scope      string `json:"scope"`
		} `json:"series"`
		Images []image `json:"images"`
		WorkID string  `json:"work_id"`
	} `json:"data"`
}
