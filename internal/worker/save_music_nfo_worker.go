package worker

import (
	"context"
	"path/filepath"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
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
	DB *pgxpool.Pool
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

	albums, err := q.ListAlbumsByArtist(ctx, artist.ID)
	if err != nil {
		return err
	}

	albumTitles := make([]string, 0, len(albums))
	for _, al := range albums {
		albumTitles = append(albumTitles, al.Title)
	}

	// Use any track's path to derive artist directory (skipping Disc N).
	var artistDir string
	wroteAlbums := 0
	for _, al := range albums {
		tracks, err := q.ListTracksByAlbum(ctx, al.ID)
		if err != nil || len(tracks) == 0 {
			continue
		}
		// Pick the first track with a non-empty file_path (some may be empty
		// if the file was soft-deleted and the primary refresh hasn't fired).
		samplePath := ""
		for _, t := range tracks {
			if t.FilePath != "" {
				samplePath = t.FilePath
				break
			}
		}
		if samplePath == "" {
			continue
		}
		releaseDir := releaseDirOf(samplePath)
		if artistDir == "" {
			artistDir = filepath.Dir(releaseDir)
		}

		if err := saver.WriteAlbumNFO(releaseDir, artist, al, tracks); err != nil {
			log.Warn().Err(err).Str("dir", releaseDir).Msg("WriteAlbumNFO failed")
			continue
		}
		wroteAlbums++
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
