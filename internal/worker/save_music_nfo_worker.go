package worker

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/karbowiak/heya/internal/titlematch"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// discSubdirRE mirrors matcher.discSubdirRE — duplicated to avoid pulling
// the matcher package into the worker's dependency graph for one regex.
var discSubdirRE = regexp.MustCompile(`(?i)^(?:disc|cd)\s*\d+$`)

func releaseDirOf(filePath string) string {
	parent := filepath.Dir(filePath)
	if discSubdirRE.MatchString(filepath.Base(parent)) {
		return filepath.Dir(parent)
	}
	return parent
}

type SaveMusicNFOWorker struct {
	river.WorkerDefaults[SaveMusicNFOArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *SaveMusicNFOWorker) Work(ctx context.Context, job *river.Job[SaveMusicNFOArgs]) error {
	q := sqlc.New(w.DB)

	artist, err := q.GetArtistByID(ctx, job.Args.ArtistID)
	if err != nil {
		return err
	}
	mediaItem, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return err
	}

	w.Progress.SetCurrentByKind(SaveMusicNFOArgs{}.Kind(), artist.Name)

	albums, err := q.ListAlbumsByArtist(ctx, artist.ID)
	if err != nil {
		return err
	}

	albumTitles := make([]string, 0, len(albums))

	// Use any track's path to derive artist directory (skipping Disc N).
	var artistDir string
	wroteAlbums := 0
	for _, al := range albums {
		tracks, err := q.ListTracksByAlbum(ctx, al.ID)
		if err != nil || len(tracks) == 0 {
			continue
		}
		// Resolve the release directory from the canonical physical-file join.
		samplePath, err := q.GetAlbumReleaseDir(ctx, al.ID)
		if err != nil {
			continue
		}
		if samplePath == "" {
			continue
		}
		releaseDir := releaseDirOf(samplePath)
		candidateArtistDir := filepath.Dir(releaseDir)
		if !musicArtistDirMatches(candidateArtistDir, artist) {
			log.Warn().
				Int64("artist_id", artist.ID).
				Str("artist", artist.Name).
				Str("album", al.Title).
				Str("dir", candidateArtistDir).
				Msg("refusing to write music NFO outside matching artist directory")
			continue
		}
		if artistDir == "" {
			artistDir = candidateArtistDir
		}

		if err := saver.WriteAlbumNFO(releaseDir, artist, al, tracks); err != nil {
			log.Warn().Err(err).Str("dir", releaseDir).Msg("WriteAlbumNFO failed")
			continue
		}
		wroteAlbums++
		albumTitles = append(albumTitles, al.Title)
	}

	if artistDir != "" {
		if err := saver.WriteArtistNFO(artistDir, artist, mediaItem, albumTitles); err != nil {
			log.Warn().Err(err).Str("dir", artistDir).Msg("WriteArtistNFO failed")
		}
	}

	log.Info().
		Int64("artist_id", artist.ID).
		Str("name", artist.Name).
		Int("albums_written", wroteAlbums).
		Str("artist_dir", artistDir).
		Msg("SaveMusicNFO complete")

	return nil
}

// musicArtistDirMatches is the final write-time circuit breaker against a bad
// match becoming authoritative. SaveMusicNFO derives its destination from the
// track path, so the directory itself must plausibly name the artist whose DB
// row is about to be serialized there. Aliases and sort names are accepted;
// parenthetical folder disambiguation is handled by titlematch.FuzzyEqual.
func musicArtistDirMatches(artistDir string, artist sqlc.Artist) bool {
	folder := strings.TrimSpace(filepath.Base(filepath.Clean(artistDir)))
	if folder == "" || folder == "." || folder == string(filepath.Separator) {
		return false
	}
	candidates := make([]string, 0, len(artist.Aliases)+2)
	candidates = append(candidates, artist.Name, artist.SortName)
	candidates = append(candidates, artist.Aliases...)
	for _, candidate := range candidates {
		if titlematch.FuzzyEqual(folder, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}
