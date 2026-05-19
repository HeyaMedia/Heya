package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/rs/zerolog/log"
)

func (m *Matcher) createOrLinkMediaItem(ctx context.Context, detail *metadata.MediaDetail, kind metadata.MediaKind, libraryID int64, filePath string) (int64, error) {
	extJSON, _ := json.Marshal(detail.ExternalIDs)

	existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
		LibraryID: libraryID,
		Column2:   extJSON,
	})
	if err == nil {
		log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item")
		return existing.ID, nil
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
		return 0, fmt.Errorf("creating media item: %w", err)
	}

	if m.downloader != nil {
		go m.downloadImages(detail, string(mediaType), item.ID)
	}

	switch kind {
	case metadata.KindMovie:
		return item.ID, m.createMovie(ctx, item.ID, detail)
	case metadata.KindTV:
		return item.ID, m.createTVSeries(ctx, item.ID, detail)
	case metadata.KindMusic:
		return item.ID, m.createMusic(ctx, item.ID, detail)
	case metadata.KindBook:
		return item.ID, m.createBook(ctx, item.ID, detail, filePath)
	}

	return item.ID, nil
}

func (m *Matcher) createMovie(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	castJSON, _ := json.Marshal(d.Cast)
	crewJSON, _ := json.Marshal(d.Crew)

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
		ProductionCompanies: emptyIfNil(d.ProductionCompanies),
		CastData:            castJSON,
		CrewData:            crewJSON,
	})
	return err
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
			})
			if err != nil {
				log.Warn().Err(err).Int("episode", ep.Number).Msg("error creating episode")
			}
		}
	}

	return nil
}

func (m *Matcher) createMusic(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	artist, err := m.q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:   mediaItemID,
		MusicbrainzID: d.ExternalIDs["musicbrainz_artist"],
		SortName:      d.ArtistName,
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

func (m *Matcher) downloadImages(detail *metadata.MediaDetail, mediaType string, mediaItemID int64) {
	ctx := context.Background()

	if detail.PosterURL != "" {
		if path, err := m.downloader.Download(ctx, detail.PosterURL, mediaType, mediaItemID, "poster.jpg"); err == nil && path != "" {
			m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:          mediaItemID,
				Title:       detail.Title,
				SortTitle:   strings.ToLower(detail.Title),
				Year:        detail.Year,
				Description: detail.Description,
				PosterPath:  path,
				BackdropPath: detail.BackdropURL,
				ExternalIds: mustJSON(detail.ExternalIDs),
			})
		}
	}

	if detail.BackdropURL != "" {
		if path, err := m.downloader.Download(ctx, detail.BackdropURL, mediaType, mediaItemID, "backdrop.jpg"); err == nil && path != "" {
			m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:          mediaItemID,
				Title:       detail.Title,
				SortTitle:   strings.ToLower(detail.Title),
				Year:        detail.Year,
				Description: detail.Description,
				PosterPath:  detail.PosterURL,
				BackdropPath: path,
				ExternalIds: mustJSON(detail.ExternalIDs),
			})
		}
	}
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
