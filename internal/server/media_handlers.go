package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

type mediaItemView struct {
	sqlc.MediaItem
	Available bool `json:"available"`
}

func handleListMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sqlc.New(app.DB)

		limit := int32(50)
		offset := int32(0)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil {
				limit = int32(n)
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, err := strconv.ParseInt(o, 10, 32); err == nil {
				offset = int32(n)
			}
		}

		mediaType := r.URL.Query().Get("type")
		if mediaType == "" {
			writeError(w, http.StatusBadRequest, "?type= parameter is required")
			return
		}

		mt := sqlc.MediaType(mediaType)
		items, err := q.ListMediaItemsByType(r.Context(), sqlc.ListMediaItemsByTypeParams{
			MediaType: mt,
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		unavailableIDs, _ := q.ListUnavailableMediaItemIDs(r.Context(), mt)
		unavailable := make(map[int64]bool, len(unavailableIDs))
		for _, id := range unavailableIDs {
			unavailable[id] = true
		}

		views := make([]mediaItemView, len(items))
		for i, item := range items {
			views[i] = mediaItemView{
				MediaItem: item,
				Available: !unavailable[item.ID],
			}
		}

		writeJSON(w, http.StatusOK, views)
	}
}

func handleGetMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idOrSlug := r.PathValue("id")
		q := sqlc.New(app.DB)

		var item sqlc.MediaItem
		var err error
		if id, parseErr := strconv.ParseInt(idOrSlug, 10, 64); parseErr == nil {
			item, err = q.GetMediaItemByID(r.Context(), id)
		} else {
			item, err = q.GetMediaItemBySlug(r.Context(), idOrSlug)
		}
		if err != nil {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}

		hasFiles := false
		var mediaFiles []map[string]any
		if files, err := q.ListLibraryFilesByMediaItem(r.Context(), pgtype.Int8{Int64: item.ID, Valid: true}); err == nil && len(files) > 0 {
			hasFiles = true
			for _, f := range files {
				mediaFiles = append(mediaFiles, map[string]any{
					"id":   f.ID,
					"size": f.Size,
				})
			}
		}

		result := map[string]any{"media_item": item, "available": hasFiles, "files": mediaFiles}

		switch item.MediaType {
		case sqlc.MediaTypeMovie:
			movie, err := q.GetMovieByMediaItemID(r.Context(), item.ID)
			if err == nil {
				result["movie"] = movie
				if movie.CollectionID.Valid {
					col, colErr := q.GetCollectionByID(r.Context(), movie.CollectionID.Int64)
					if colErr == nil {
						result["collection"] = col
					}
				}
			}
		case sqlc.MediaTypeTv:
			series, err := q.GetTVSeriesByMediaItemID(r.Context(), item.ID)
			if err == nil {
				result["tv_series"] = series
				seasons, _ := q.ListTVSeasonsBySeries(r.Context(), series.ID)
				type seasonWithEpisodes struct {
					sqlc.TvSeason
					Episodes []sqlc.TvEpisode `json:"episodes"`
				}
				var enriched []seasonWithEpisodes
				for _, s := range seasons {
					eps, _ := q.ListTVEpisodesBySeason(r.Context(), s.ID)
					enriched = append(enriched, seasonWithEpisodes{TvSeason: s, Episodes: eps})
				}
				result["seasons"] = enriched
			}
		case sqlc.MediaTypeMusic:
			artist, err := q.GetArtistByMediaItemID(r.Context(), item.ID)
			if err == nil {
				result["artist"] = artist
				albums, _ := q.ListAlbumsByArtist(r.Context(), artist.ID)
				result["albums"] = albums
			}
		case sqlc.MediaTypeBook:
			book, err := q.GetBookByMediaItemID(r.Context(), item.ID)
			if err == nil {
				result["book"] = book
				if book.AuthorID.Valid {
					author, _ := q.GetAuthorByID(r.Context(), book.AuthorID.Int64)
					result["author"] = author
				}
			}
		}

		if cast, err := q.ListMediaCastSlim(r.Context(), item.ID); err == nil && len(cast) > 0 {
			result["cast"] = cast
		}

		if crew, err := q.ListMediaCrewSlim(r.Context(), item.ID); err == nil && len(crew) > 0 {
			result["crew"] = crew
		}

		if keywords, err := q.ListMediaKeywords(r.Context(), item.ID); err == nil && len(keywords) > 0 {
			result["keywords"] = keywords
		}

		if videos, err := q.ListMediaVideos(r.Context(), item.ID); err == nil && len(videos) > 0 {
			result["videos"] = videos
		}

		if certs, err := q.ListMediaCertifications(r.Context(), item.ID); err == nil && len(certs) > 0 {
			result["certifications"] = certs
		}

		if recs, err := q.ListMediaRecommendationsWithLibrary(r.Context(), item.ID); err == nil && len(recs) > 0 {
			result["recommendations"] = recs
		}

		if companies, err := q.ListMediaProductionCompanies(r.Context(), item.ID); err == nil && len(companies) > 0 {
			result["production_companies"] = companies
		}

		if assets, err := q.ListMediaAssets(r.Context(), item.ID); err == nil && len(assets) > 0 {
			result["assets"] = assets
		}

		if extras, err := q.ListMediaExtras(r.Context(), item.ID); err == nil && len(extras) > 0 {
			result["extras"] = extras
		}

		if ratings, err := q.ListExternalRatings(r.Context(), item.ID); err == nil && len(ratings) > 0 {
			result["external_ratings"] = ratings
		}

		if item.MediaType == sqlc.MediaTypeTv {
			if epFiles, err := q.ListEpisodeFiles(r.Context(), pgtype.Int8{Int64: item.ID, Valid: true}); err == nil && len(epFiles) > 0 {
				episodeFileMap := buildEpisodeFileMap(epFiles)
				if len(episodeFileMap) > 0 {
					result["episode_files"] = episodeFileMap
				}
			}
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetPerson(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idOrSlug := r.PathValue("id")
		q := sqlc.New(app.DB)

		var person sqlc.Person
		var err error
		if id, parseErr := strconv.ParseInt(idOrSlug, 10, 64); parseErr == nil {
			person, err = q.GetPersonByID(r.Context(), id)
		} else {
			person, err = q.GetPersonBySlug(r.Context(), idOrSlug)
		}
		if err != nil {
			writeError(w, http.StatusNotFound, "person not found")
			return
		}

		result := map[string]any{"person": person}

		if castCredits, err := q.ListPersonCastCredits(r.Context(), person.ID); err == nil && len(castCredits) > 0 {
			result["cast_credits"] = castCredits
		}

		if crewCredits, err := q.ListPersonCrewCredits(r.Context(), person.ID); err == nil && len(crewCredits) > 0 {
			result["crew_credits"] = crewCredits
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleRefreshMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		if err := app.RefreshMediaItem(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
	}
}

func handleResolveMatch(app *service.App) http.HandlerFunc {
	type resolveReq struct {
		CandidateID int64 `json:"candidate_id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		var req resolveReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.ResolveMatch(r.Context(), id, req.CandidateID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "matched"})
	}
}

func handleListUnmatched(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		q := sqlc.New(app.DB)
		files, err := q.ListLibraryFilesByStatus(r.Context(), sqlc.ListLibraryFilesByStatusParams{
			LibraryID: id,
			Status:    sqlc.FileStatusUnmatched,
			Limit:     100,
			Offset:    0,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		type unmatchedFile struct {
			File       sqlc.LibraryFile     `json:"file"`
			Candidates []sqlc.MatchCandidate `json:"candidates"`
		}

		var result []unmatchedFile
		for _, f := range files {
			candidates, _ := q.ListMatchCandidatesByFile(r.Context(), f.ID)
			result = append(result, unmatchedFile{File: f, Candidates: candidates})
		}

		writeJSON(w, http.StatusOK, result)
	}
}

type episodeFileEntry struct {
	FileID int64 `json:"file_id"`
	Size   int64 `json:"size"`
}

func buildEpisodeFileMap(files []sqlc.ListEpisodeFilesRow) map[string]episodeFileEntry {
	type parseResult struct {
		Parsed struct {
			Release struct {
				Seasons  []int `json:"seasons"`
				Episodes []int `json:"episodes"`
			} `json:"release"`
		} `json:"parsed"`
	}

	result := make(map[string]episodeFileEntry)
	for _, f := range files {
		if len(f.ParseResult) == 0 {
			continue
		}
		var pr parseResult
		if err := json.Unmarshal(f.ParseResult, &pr); err != nil {
			continue
		}
		for _, s := range pr.Parsed.Release.Seasons {
			for _, e := range pr.Parsed.Release.Episodes {
				key := fmt.Sprintf("s%de%d", s, e)
				result[key] = episodeFileEntry{FileID: f.ID, Size: f.Size}
			}
		}
	}
	return result
}
