package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

func (m *Matcher) createOrLinkMediaItem(ctx context.Context, detail *metadata.MediaDetail, kind metadata.MediaKind, libraryID int64, filePath string) (int64, bool, error) {
	extJSON, _ := json.Marshal(detail.ExternalIDs)

	existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
		LibraryID: libraryID,
		Column2:   extJSON,
	})
	if err == nil {
		log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item")
		return existing.ID, false, nil
	}

	mediaType := kindToMediaType(kind)
	sortTitle := strings.ToLower(detail.Title)

	item, err := m.q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    libraryID,
		MediaType:    mediaType,
		Title:        detail.Title,
		SortTitle:    sortTitle,
		Year:         detail.Year,
		Description:  detail.Description,
		PosterPath:   detail.PosterURL,
		BackdropPath: detail.BackdropURL,
		ExternalIds:  extJSON,
	})
	if err != nil {
		existing, retryErr := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
			LibraryID: libraryID,
			Column2:   extJSON,
		})
		if retryErr == nil {
			log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item (race resolved)")
			return existing.ID, false, nil
		}
		return 0, false, fmt.Errorf("creating media item: %w", err)
	}

	itemSlug := slug.GenerateUnique(ctx, detail.Title, detail.Year, item.ID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			r, err := m.q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
			if err != nil {
				return false, err
			}
			return r, nil
		})
	m.q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: itemSlug})

	if m.downloader != nil {
		m.processMediaImages(detail, string(mediaType), item.ID, filePath)
	}

	var createErr error
	switch kind {
	case metadata.KindMovie:
		createErr = m.createMovie(ctx, item.ID, detail)
	case metadata.KindTV:
		createErr = m.createTVSeries(ctx, item.ID, detail)
	case metadata.KindMusic:
		createErr = m.createMusic(ctx, item.ID, detail)
	case metadata.KindBook:
		createErr = m.createBook(ctx, item.ID, detail, filePath)
	}

	return item.ID, true, createErr
}

func (m *Matcher) createMovie(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	castJSON, _ := json.Marshal(d.Cast)
	crewJSON, _ := json.Marshal(d.Crew)

	companyNames := make([]string, len(d.ProductionCompanies))
	for i, c := range d.ProductionCompanies {
		companyNames[i] = c.Name
	}

	_, err := m.q.CreateMovie(ctx, sqlc.CreateMovieParams{
		MediaItemID:         mediaItemID,
		TmdbID:              pgInt4FromString(d.ExternalIDs["tmdb"]),
		ImdbID:              d.ExternalIDs["imdb"],
		RuntimeMinutes:      int32(d.RuntimeMinutes),
		Tagline:             d.Tagline,
		Genres:              emptyIfNil(d.Genres),
		Rating:              numericFromFloat(d.Rating),
		ReleaseDate:         pgDateFromString(d.ReleaseDate),
		OriginalTitle:       d.OriginalTitle,
		OriginalLanguage:    d.OriginalLanguage,
		Budget:              d.Budget,
		Revenue:             d.Revenue,
		Popularity:          numericFromFloat(d.Popularity),
		VoteCount:           int32(d.VoteCount),
		ProductionCompanies: emptyIfNil(companyNames),
		CastData:            castJSON,
		CrewData:            crewJSON,
	})
	if err != nil {
		return err
	}

	m.storeRichMetadata(ctx, mediaItemID, d)
	return nil
}

func (m *Matcher) storeRichMetadata(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) {
	for _, c := range d.Cast {
		person, err := m.q.CreatePerson(ctx, sqlc.CreatePersonParams{
			TmdbID:      pgInt4(int32(c.TmdbID)),
			Name:        c.Name,
			AlsoKnownAs: []string{},
			Gender:      int32(c.Gender),
			ProfilePath: c.ProfilePath,
			Popularity:  numericFromFloat(c.Popularity),
		})
		if err != nil {
			log.Debug().Err(err).Str("name", c.Name).Int("tmdb", c.TmdbID).Msg("failed to create person for cast")
			continue
		}
		m.q.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{
			MediaItemID:  mediaItemID,
			PersonID:     person.ID,
			Character:    c.Character,
			DisplayOrder: int32(c.Order),
		})
	}

	for _, c := range d.Crew {
		person, err := m.q.CreatePerson(ctx, sqlc.CreatePersonParams{
			TmdbID:      pgInt4(int32(c.TmdbID)),
			Name:        c.Name,
			AlsoKnownAs: []string{},
			Gender:      int32(c.Gender),
			ProfilePath: c.ProfilePath,
			Popularity:  numericFromFloat(0),
		})
		if err != nil {
			continue
		}
		m.q.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{
			MediaItemID: mediaItemID,
			PersonID:    person.ID,
			Job:         c.Job,
			Department:  c.Department,
		})
	}

	for _, k := range d.Keywords {
		kw, err := m.q.CreateKeyword(ctx, sqlc.CreateKeywordParams{
			TmdbID: pgInt4(int32(k.TmdbID)),
			Name:   k.Name,
		})
		if err != nil {
			continue
		}
		m.q.LinkMediaKeyword(ctx, sqlc.LinkMediaKeywordParams{
			MediaItemID: mediaItemID,
			KeywordID:   kw.ID,
		})
	}

	for _, pc := range d.ProductionCompanies {
		co, err := m.q.CreateProductionCompany(ctx, sqlc.CreateProductionCompanyParams{
			TmdbID:        pgInt4(int32(pc.TmdbID)),
			Name:          pc.Name,
			LogoPath:      pc.LogoPath,
			OriginCountry: pc.OriginCountry,
		})
		if err != nil {
			continue
		}
		m.q.LinkMediaProductionCompany(ctx, sqlc.LinkMediaProductionCompanyParams{
			MediaItemID: mediaItemID,
			CompanyID:   co.ID,
		})
	}

	for _, v := range d.Videos {
		m.q.CreateMediaVideo(ctx, sqlc.CreateMediaVideoParams{
			MediaItemID: mediaItemID,
			TmdbKey:     v.TmdbKey,
			Name:        v.Name,
			Site:        v.Site,
			VideoKey:    v.Key,
			VideoType:   v.Type,
			Language:    v.Language,
			Official:    v.Official,
		})
	}

	for _, c := range d.Certifications {
		m.q.CreateMediaCertification(ctx, sqlc.CreateMediaCertificationParams{
			MediaItemID:   mediaItemID,
			Country:       c.Country,
			Certification: c.Certification,
			ReleaseDate:   pgDateFromString(c.ReleaseDate),
			ReleaseType:   int32(c.ReleaseType),
		})
	}

	for _, r := range d.Recommendations {
		m.q.CreateMediaRecommendation(ctx, sqlc.CreateMediaRecommendationParams{
			MediaItemID:       mediaItemID,
			RecommendedTmdbID: int32(r.TmdbID),
			Title:             r.Title,
			PosterPath:        r.PosterPath,
			MediaType:         r.MediaType,
			VoteAverage:       numericFromFloat(r.VoteAverage),
			ReleaseDate:       r.ReleaseDate,
		})
	}

	if d.Collection != nil && m.shouldAutoCollect(ctx, mediaItemID) {
		m.q.CreateCollection(ctx, sqlc.CreateCollectionParams{
			TmdbID:       pgInt4(int32(d.Collection.TmdbID)),
			Name:         d.Collection.Name,
			Overview:     d.Collection.Overview,
			PosterPath:   d.Collection.PosterPath,
			BackdropPath: d.Collection.BackdropPath,
		})
	}

	if d.WikidataID != "" || d.FacebookID != "" || d.InstagramID != "" || d.TwitterID != "" || d.Homepage != "" {
		item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
		if err == nil {
			m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:           item.ID,
				Title:        item.Title,
				SortTitle:    item.SortTitle,
				Year:         item.Year,
				Description:  item.Description,
				PosterPath:   item.PosterPath,
				BackdropPath: item.BackdropPath,
				ExternalIds:  item.ExternalIds,
			})
		}
	}

	log.Info().Int64("media_id", mediaItemID).
		Int("cast", len(d.Cast)).
		Int("crew", len(d.Crew)).
		Int("keywords", len(d.Keywords)).
		Int("videos", len(d.Videos)).
		Int("recs", len(d.Recommendations)).
		Msg("stored rich metadata")
}

func (m *Matcher) shouldAutoCollect(ctx context.Context, mediaItemID int64) bool {
	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return true
	}
	lib, err := m.q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return true
	}
	settings := metadata.ParseSettings(lib.Settings)
	if settings.IsEmpty() {
		return true
	}
	return settings.AutoCollections
}

func pgInt4(v int32) pgtype.Int4 {
	if v == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: v, Valid: true}
}

func (m *Matcher) createTVSeries(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	castJSON, _ := json.Marshal(d.Cast)

	series, err := m.q.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
		MediaItemID:      mediaItemID,
		TmdbID:           pgInt4FromString(d.ExternalIDs["tmdb"]),
		ImdbID:           d.ExternalIDs["imdb"],
		Status:           d.Status,
		Genres:           emptyIfNil(d.Genres),
		Rating:           numericFromFloat(d.Rating),
		FirstAirDate:     pgDateFromString(d.FirstAirDate),
		LastAirDate:      pgDateFromString(d.LastAirDate),
		OriginalName:     d.OriginalName,
		OriginalLanguage: d.OriginalLanguage,
		Networks:         emptyIfNil(d.Networks),
		CreatedBy:        emptyIfNil(d.CreatedBy),
		NumberOfSeasons:  int32(d.NumberOfSeasons),
		NumberOfEpisodes: int32(d.NumberOfEpisodes),
		Popularity:       numericFromFloat(d.Popularity),
		VoteCount:        int32(d.VoteCount),
		CastData:         castJSON,
	})
	if err != nil {
		return fmt.Errorf("creating tv series: %w", err)
	}

	for _, sd := range d.Seasons {
		season, err := m.q.CreateTVSeason(ctx, sqlc.CreateTVSeasonParams{
			SeriesID:     series.ID,
			SeasonNumber: int32(sd.Number),
			Title:        sd.Title,
			Overview:     sd.Overview,
			PosterPath:   sd.PosterURL,
			AirDate:      pgDateFromString(sd.AirDate),
		})
		if err != nil {
			log.Warn().Err(err).Int("season", sd.Number).Msg("error creating season")
			continue
		}

		for _, ep := range sd.Episodes {
			_, err := m.q.CreateTVEpisode(ctx, sqlc.CreateTVEpisodeParams{
				SeasonID:       season.ID,
				EpisodeNumber:  int32(ep.Number),
				Title:          ep.Title,
				Overview:       ep.Overview,
				StillPath:      ep.StillURL,
				RuntimeMinutes: int32(ep.RuntimeMinutes),
				AirDate:        pgDateFromString(ep.AirDate),
				Rating:         numericFromFloat(ep.Rating),
				VoteCount:      int32(ep.VoteCount),
			})
			if err != nil {
				log.Warn().Err(err).Int("episode", ep.Number).Msg("error creating episode")
			}
		}
	}

	m.storeRichMetadata(ctx, mediaItemID, d)

	return nil
}

func (m *Matcher) createMusic(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	artist, err := m.q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:   mediaItemID,
		MusicbrainzID: d.ExternalIDs["musicbrainz_artist"],
		SortName:      d.ArtistName,
		Biography:     d.ArtistBio,
	})
	if err != nil {
		return fmt.Errorf("creating artist: %w", err)
	}

	album, err := m.q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID:      artist.ID,
		Title:         d.AlbumTitle,
		Year:          d.Year,
		MusicbrainzID: d.ExternalIDs["musicbrainz"],
		AlbumType:     d.AlbumType,
		Genres:        emptyIfNil(d.Genres),
		CoverPath:     d.CoverURL,
		ReleaseDate:   pgDateFromString(d.PublishDate),
		Label:         d.Label,
		Country:       d.Country,
		Barcode:       d.Barcode,
		TotalTracks:   int32(len(d.Tracks)),
		TotalDiscs:    int32(d.TotalDiscs),
		Tags:          emptyIfNil(d.Tags),
	})
	if err != nil {
		return fmt.Errorf("creating album: %w", err)
	}

	for _, t := range d.Tracks {
		_, err := m.q.CreateTrack(ctx, sqlc.CreateTrackParams{
			AlbumID:     album.ID,
			DiscNumber:  int32(t.DiscNumber),
			TrackNumber: int32(t.TrackNumber),
			Title:       t.Title,
			DurationMs:  int32(t.DurationMs),
		})
		if err != nil {
			log.Warn().Err(err).Str("track", t.Title).Msg("error creating track")
		}
	}

	return nil
}

func (m *Matcher) createBook(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail, filePath string) error {
	var authorID pgtype.Int8

	if d.AuthorName != "" {
		existing, err := m.q.GetAuthorByName(ctx, d.AuthorName)
		if err == nil {
			authorID = pgtype.Int8{Int64: existing.ID, Valid: true}
		} else {
			author, err := m.q.CreateAuthor(ctx, sqlc.CreateAuthorParams{
				Name:          d.AuthorName,
				OpenlibraryID: d.ExternalIDs["openlibrary_author"],
				Biography:     d.AuthorBio,
				BirthDate:     d.AuthorBirthDate,
				DeathDate:     d.AuthorDeathDate,
			})
			if err != nil {
				log.Warn().Err(err).Str("author", d.AuthorName).Msg("error creating author")
			} else {
				authorID = pgtype.Int8{Int64: author.ID, Valid: true}
			}
		}
	}

	ext := ""
	if idx := strings.LastIndex(filePath, "."); idx >= 0 {
		ext = filePath[idx+1:]
	}

	_, err := m.q.CreateBook(ctx, sqlc.CreateBookParams{
		MediaItemID:   mediaItemID,
		AuthorID:      authorID,
		Isbn:          d.ISBN,
		OpenlibraryID: d.ExternalIDs["openlibrary"],
		PageCount:     int32(d.PageCount),
		Publisher:     d.Publisher,
		PublishDate:   pgDateFromString(d.PublishDate),
		FilePath:      filePath,
		Subjects:      emptyIfNil(d.Subjects),
		Language:      d.Language,
		SeriesName:    d.SeriesName,
		SeriesNumber:  int32(d.SeriesNum),
		Format:        ext,
		Description:   d.Description,
	})
	return err
}

var (
	imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

	knownImages = map[string]sqlc.AssetType{
		"poster":    sqlc.AssetTypePoster,
		"fanart":    sqlc.AssetTypeFanart,
		"banner":    sqlc.AssetTypeBanner,
		"clearart":  sqlc.AssetTypeClearart,
		"clearlogo": sqlc.AssetTypeClearlogo,
		"landscape": sqlc.AssetTypeLandscape,
		"logo":      sqlc.AssetTypeLogo,
		"folder":    sqlc.AssetTypeFolder,
		"backdrop":  sqlc.AssetTypeBackdrop,
		"disc":      sqlc.AssetTypeDisc,
		"discart":   sqlc.AssetTypeDisc,
		"cdart":     sqlc.AssetTypeDisc,
	}

	backdropNumRE  = regexp.MustCompile(`^backdrop(\d+)\.`)
	seasonImageRE  = regexp.MustCompile(`^season(\d+)-(poster|banner)\.`)
	seasonSpecialRE = regexp.MustCompile(`^season-specials-(poster|banner)\.`)
)

func (m *Matcher) processMediaImages(detail *metadata.MediaDetail, mediaType string, mediaItemID int64, filePath string) {
	ctx := context.Background()
	mediaDir := vfs.Dir(filePath)
	if strings.HasPrefix(strings.ToLower(vfs.Base(mediaDir)), "season") {
		mediaDir = vfs.Dir(mediaDir)
	}

	cacheDir := filepath.Join(m.downloader.CacheDir(), "images", mediaType, fmt.Sprintf("%d", mediaItemID))
	os.MkdirAll(cacheDir, 0o755)

	var primaryPoster, primaryBackdrop string

	source, err := vfs.Open(mediaDir)
	if err != nil {
		log.Warn().Err(err).Str("dir", mediaDir).Msg("cannot open media directory for images")
	}

	if source != nil {
		defer source.Close()

		entries, err := fs.ReadDir(source.FS, ".")
		if err != nil {
			log.Warn().Err(err).Str("dir", mediaDir).Msg("cannot read media directory for images")
			entries = nil
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !imageExts[ext] {
				continue
			}

			nameNoExt := strings.TrimSuffix(strings.ToLower(name), ext)
			dstPath := filepath.Join(cacheDir, name)

			if err := copyFromFS(source.FS, name, dstPath); err != nil {
				continue
			}

			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}

			if at, ok := knownImages[nameNoExt]; ok {
				m.q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   at,
					Source:      "local",
					LocalPath:   dstPath,
					FileSize:    size,
				})
				if at == sqlc.AssetTypePoster && primaryPoster == "" {
					primaryPoster = dstPath
				}
				if (at == sqlc.AssetTypeBackdrop || at == sqlc.AssetTypeFanart) && primaryBackdrop == "" {
					primaryBackdrop = dstPath
				}
				log.Debug().Str("file", name).Str("type", string(at)).Msg("cached local image")
				continue
			}

			if sub := backdropNumRE.FindStringSubmatch(strings.ToLower(name)); sub != nil {
				order := 0
				if len(sub) > 1 {
					for _, c := range sub[1] {
						order = order*10 + int(c-'0')
					}
				}
				m.q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   sqlc.AssetTypeBackdrop,
					Source:      "local",
					LocalPath:   dstPath,
					SortOrder:   int32(order),
					FileSize:    size,
				})
				log.Debug().Str("file", name).Int("order", order).Msg("cached numbered backdrop")
				continue
			}

			if sub := seasonImageRE.FindStringSubmatch(strings.ToLower(name)); sub != nil {
				seasonNum := 0
				for _, c := range sub[1] {
					seasonNum = seasonNum*10 + int(c-'0')
				}
				label := fmt.Sprintf("season%02d-%s", seasonNum, sub[2])
				assetType := sqlc.AssetTypeBanner
				if sub[2] == "poster" {
					assetType = sqlc.AssetTypeSeasonPoster
				}
				m.q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   assetType,
					Source:      "local",
					LocalPath:   dstPath,
					Label:       label,
					FileSize:    size,
				})
				log.Debug().Str("file", name).Str("label", label).Msg("cached season image")
				continue
			}

			if sub := seasonSpecialRE.FindStringSubmatch(strings.ToLower(name)); sub != nil {
				label := fmt.Sprintf("season00-%s", sub[1])
				assetType := sqlc.AssetTypeBanner
				if sub[1] == "poster" {
					assetType = sqlc.AssetTypeSeasonPoster
				}
				m.q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   assetType,
					Source:      "local",
					LocalPath:   dstPath,
					Label:       label,
					FileSize:    size,
				})
				log.Debug().Str("file", name).Str("label", label).Msg("cached specials image")
				continue
			}
		}

		m.detectExtras(ctx, mediaItemID, source.FS, mediaDir)
		m.scanSeasonImages(ctx, mediaItemID, source.FS, cacheDir)
	}

	if primaryPoster == "" && detail.PosterURL != "" {
		if path, err := m.downloader.Download(ctx, detail.PosterURL, mediaType, mediaItemID, "poster.jpg"); err == nil && path != "" {
			primaryPoster = path
		}
	}
	if primaryBackdrop == "" && detail.BackdropURL != "" {
		if path, err := m.downloader.Download(ctx, detail.BackdropURL, mediaType, mediaItemID, "backdrop.jpg"); err == nil && path != "" {
			primaryBackdrop = path
		}
	}

	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return
	}
	p := item.PosterPath
	b := item.BackdropPath
	if primaryPoster != "" {
		p = primaryPoster
	}
	if primaryBackdrop != "" {
		b = primaryBackdrop
	}
	if p != item.PosterPath || b != item.BackdropPath {
		m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
			ID:           item.ID,
			Title:        item.Title,
			SortTitle:    item.SortTitle,
			Year:         item.Year,
			Description:  item.Description,
			PosterPath:   p,
			BackdropPath: b,
			ExternalIds:  item.ExternalIds,
		})
	}

	log.Info().
		Int64("media_id", mediaItemID).
		Str("poster", p).
		Str("backdrop", b).
		Msg("processed media images")
}

var thumbRE = regexp.MustCompile(`(?i)S(\d+)E(\d+).*-thumb\.`)

func (m *Matcher) scanSeasonImages(ctx context.Context, mediaItemID int64, fsys fs.FS, cacheDir string) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		if !strings.HasPrefix(nameLower, "season") {
			continue
		}

		seasonEntries, err := fs.ReadDir(fsys, e.Name())
		if err != nil {
			continue
		}

		for _, se := range seasonEntries {
			if se.IsDir() {
				continue
			}
			name := se.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !imageExts[ext] {
				continue
			}

			if sub := thumbRE.FindStringSubmatch(name); sub != nil {
				seasonNum := 0
				epNum := 0
				for _, c := range sub[1] {
					seasonNum = seasonNum*10 + int(c-'0')
				}
				for _, c := range sub[2] {
					epNum = epNum*10 + int(c-'0')
				}

				fsPath := e.Name() + "/" + name
				label := fmt.Sprintf("s%02de%02d", seasonNum, epNum)
				dstPath := filepath.Join(cacheDir, label+"-thumb"+ext)

				if err := copyFromFS(fsys, fsPath, dstPath); err != nil {
					continue
				}

				info, _ := se.Info()
				size := int64(0)
				if info != nil {
					size = info.Size()
				}

				m.q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   sqlc.AssetTypeBackdrop,
					Source:      "local",
					LocalPath:   dstPath,
					Label:       label,
					SortOrder:   int32(2000 + seasonNum*100 + epNum),
					FileSize:    size,
				})
				log.Debug().Str("file", name).Str("label", label).Msg("cached episode thumbnail")
			}
		}
	}
}

var (
	videoExts = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".wmv": true}

	extraFolders = map[string]sqlc.ExtraType{
		"trailers":          sqlc.ExtraTypeTrailer,
		"trailer":           sqlc.ExtraTypeTrailer,
		"behind the scenes": sqlc.ExtraTypeBehindTheScenes,
		"deleted scenes":    sqlc.ExtraTypeDeletedScene,
		"featurettes":       sqlc.ExtraTypeFeaturette,
		"interviews":        sqlc.ExtraTypeInterview,
		"scenes":            sqlc.ExtraTypeScene,
		"shorts":            sqlc.ExtraTypeShort,
		"other":             sqlc.ExtraTypeOther,
	}
)

func (m *Matcher) detectExtras(ctx context.Context, mediaItemID int64, fsys fs.FS, mediaDir string) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folderName := strings.ToLower(e.Name())
		extraType, ok := extraFolders[folderName]
		if !ok {
			continue
		}

		extraEntries, err := fs.ReadDir(fsys, e.Name())
		if err != nil {
			continue
		}

		for _, ee := range extraEntries {
			if ee.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(ee.Name()))
			if !videoExts[ext] {
				continue
			}
			title := strings.TrimSuffix(ee.Name(), filepath.Ext(ee.Name()))
			info, _ := ee.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			m.q.CreateMediaExtra(ctx, sqlc.CreateMediaExtraParams{
				MediaItemID: mediaItemID,
				ExtraType:   extraType,
				Title:       title,
				FilePath:    vfs.Join(mediaDir, e.Name(), ee.Name()),
				FileSize:    size,
			})
		}
	}
}

func copyFromFS(fsys fs.FS, name string, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func kindToMediaType(kind metadata.MediaKind) sqlc.MediaType {
	switch kind {
	case metadata.KindMovie:
		return sqlc.MediaTypeMovie
	case metadata.KindTV:
		return sqlc.MediaTypeTv
	case metadata.KindMusic:
		return sqlc.MediaTypeMusic
	case metadata.KindBook:
		return sqlc.MediaTypeBook
	default:
		return sqlc.MediaTypeMovie
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if b == nil {
		return []byte("{}")
	}
	return b
}

func (m *Matcher) StoreEntityMetadata(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail) {
	switch kind {
	case metadata.KindMovie:
		m.createMovie(ctx, mediaItemID, detail)
	case metadata.KindTV:
		m.createTVSeries(ctx, mediaItemID, detail)
	case metadata.KindMusic:
		m.createMusic(ctx, mediaItemID, detail)
	case metadata.KindBook:
		m.createBook(ctx, mediaItemID, detail, "")
	}
}

func (m *Matcher) StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail) {
	m.storeRichMetadata(ctx, mediaItemID, detail)
}
