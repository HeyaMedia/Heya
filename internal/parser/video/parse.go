package video

type ParsedMovie struct {
	Title         string
	Year          string
	Resolution    Resolution
	Sources       []Source
	VideoCodec    VideoCodec
	AudioCodec    AudioCodec
	AudioChannels Channels
	Revision      QualityRevision
	Group         string
	Edition       Edition
	Languages     []Language
	Multi         bool
	Complete      bool
}

type ParsedShow struct {
	Title           string
	Year            string
	Resolution      Resolution
	Sources         []Source
	VideoCodec      VideoCodec
	AudioCodec      AudioCodec
	AudioChannels   Channels
	Revision        QualityRevision
	Group           string
	Edition         Edition
	Languages       []Language
	Multi           bool
	Complete        bool
	Seasons         []int
	EpisodeNumbers  []int
	FullSeason      bool
	IsPartialSeason bool
	IsMultiSeason   bool
	IsSeasonExtra   bool
	IsSpecial       bool
	SeasonPart      int
	IsTv            bool
}

func FilenameParse(name string, isTv bool) interface{} {
	var title string
	var year string

	if !isTv {
		tay := ParseTitleAndYear(name)
		title = tay.Title
		year = tay.Year
	}

	edition := ParseEdition(name)
	videoCodecResult := ParseVideoCodec(name)
	audioCodecResult := ParseAudioCodec(name)
	audioChannelsResult := ParseAudioChannels(name)
	group := ParseGroup(name)
	languages := ParseLanguage(name)
	quality := ParseQuality(name)
	multi := IsMulti(name)
	complete := IsComplete(name)

	if isTv {
		season := ParseSeason(name)
		if season != nil {
			seriesTitle := season.SeriesTitle
			if seriesTitle == "" {
				seriesTitle = title
			}
			return &ParsedShow{
				Title:           seriesTitle,
				Year:            year,
				Resolution:      quality.Resolution,
				Sources:         quality.Sources,
				VideoCodec:      videoCodecResult.Codec,
				AudioCodec:      audioCodecResult.Codec,
				AudioChannels:   audioChannelsResult.Channels,
				Revision:        quality.Revision,
				Group:           group,
				Edition:         edition,
				Languages:       languages,
				Multi:           multi,
				Complete:        complete,
				Seasons:         season.Seasons,
				EpisodeNumbers:  season.EpisodeNumbers,
				FullSeason:      season.FullSeason,
				IsPartialSeason: season.IsPartialSeason,
				IsMultiSeason:   season.IsMultiSeason,
				IsSeasonExtra:   season.IsSeasonExtra,
				IsSpecial:       season.IsSpecial,
				SeasonPart:      season.SeasonPart,
				IsTv:            true,
			}
		}
	}

	return &ParsedMovie{
		Title:         title,
		Year:          year,
		Resolution:    quality.Resolution,
		Sources:       quality.Sources,
		VideoCodec:    videoCodecResult.Codec,
		AudioCodec:    audioCodecResult.Codec,
		AudioChannels: audioChannelsResult.Channels,
		Revision:      quality.Revision,
		Group:         group,
		Edition:       edition,
		Languages:     languages,
		Multi:         multi,
		Complete:      complete,
	}
}

func FilenameParseShow(name string) *ParsedShow {
	result := FilenameParse(name, true)
	if show, ok := result.(*ParsedShow); ok {
		return show
	}
	movie := result.(*ParsedMovie)
	return &ParsedShow{
		Title:         movie.Title,
		Year:          movie.Year,
		Resolution:    movie.Resolution,
		Sources:       movie.Sources,
		VideoCodec:    movie.VideoCodec,
		AudioCodec:    movie.AudioCodec,
		AudioChannels: movie.AudioChannels,
		Revision:      movie.Revision,
		Group:         movie.Group,
		Edition:       movie.Edition,
		Languages:     movie.Languages,
		Multi:         movie.Multi,
		Complete:      movie.Complete,
	}
}

func FilenameParseMovie(name string) *ParsedMovie {
	result := FilenameParse(name, false)
	if movie, ok := result.(*ParsedMovie); ok {
		return movie
	}
	return &ParsedMovie{}
}
