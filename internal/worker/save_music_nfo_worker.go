package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/karbowiak/heya/internal/titlematch"
	"github.com/karbowiak/heya/internal/vfs"
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
	DB              *pgxpool.Pool
	Progress        *TaskProgressBroadcaster
	GeneratedWrites GeneratedWriteSuppressor
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
	allowed, err := generatedWriteAllowed(ctx, q, mediaItem.LibraryID, generatedWriteNFO)
	if err != nil {
		return err
	}
	if !allowed {
		log.Debug().Int64("library_id", mediaItem.LibraryID).Int64("artist_id", artist.ID).Msg("save_music_nfo: disabled before execution, skipping")
		return nil
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
		if err != nil {
			return fmt.Errorf("save_music_nfo: list tracks for album %d: %w", al.ID, err)
		}
		if len(tracks) == 0 {
			continue
		}
		// Resolve the release directory from the canonical physical-file join.
		samplePath, err := q.GetAlbumReleaseDir(ctx, al.ID)
		if err != nil {
			return fmt.Errorf("save_music_nfo: resolve release directory for album %d: %w", al.ID, err)
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
				Str("dir", vfs.RedactPath(candidateArtistDir)).
				Msg("refusing to write music NFO outside matching artist directory")
			continue
		}
		if artistDir == "" {
			artistDir = candidateArtistDir
		}
		allowed, err = generatedWriteAllowed(ctx, q, mediaItem.LibraryID, generatedWriteNFO)
		if err != nil {
			return err
		}
		if !allowed {
			log.Debug().Int64("library_id", mediaItem.LibraryID).Int64("artist_id", artist.ID).Msg("save_music_nfo: disabled while executing, stopping")
			return nil
		}

		prepared, err := saver.PrepareAlbumNFO(releaseDir, artist, al, tracks)
		if err == nil {
			err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, mediaItem.LibraryID, generatedWriteNFO, prepared, func(validateCtx context.Context) (bool, error) {
				currentPath, currentErr := q.GetAlbumReleaseDir(validateCtx, al.ID)
				if currentErr != nil {
					return false, currentErr
				}
				return currentPath != "" && filepath.Clean(releaseDirOf(currentPath)) == filepath.Clean(releaseDir), nil
			})
		}
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Str("dir", vfs.RedactPath(releaseDir)).Msg("WriteAlbumNFO failed")
			return err
		}
		wroteAlbums++
		albumTitles = append(albumTitles, al.Title)
	}

	if artistDir != "" {
		allowed, err = generatedWriteAllowed(ctx, q, mediaItem.LibraryID, generatedWriteNFO)
		if err != nil {
			return err
		}
		if !allowed {
			log.Debug().Int64("library_id", mediaItem.LibraryID).Int64("artist_id", artist.ID).Msg("save_music_nfo: disabled while executing, stopping")
			return nil
		}
		prepared, err := saver.PrepareArtistNFO(artistDir, artist, mediaItem, albumTitles)
		if err == nil {
			err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, mediaItem.LibraryID, generatedWriteNFO, prepared, func(validateCtx context.Context) (bool, error) {
				return musicArtistStillOwnsDir(validateCtx, q, artist.ID, artistDir)
			})
		}
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Str("dir", vfs.RedactPath(artistDir)).Msg("WriteArtistNFO failed")
			return err
		}
	}

	log.Info().
		Int64("artist_id", artist.ID).
		Str("name", artist.Name).
		Int("albums_written", wroteAlbums).
		Str("artist_dir", vfs.RedactPath(artistDir)).
		Msg("SaveMusicNFO complete")

	return nil
}

func musicArtistStillOwnsDir(ctx context.Context, q *sqlc.Queries, artistID int64, artistDir string) (bool, error) {
	albums, err := q.ListAlbumsByArtist(ctx, artistID)
	if err != nil {
		return false, err
	}
	want := filepath.Clean(artistDir)
	for _, album := range albums {
		path, pathErr := q.GetAlbumReleaseDir(ctx, album.ID)
		if pathErr != nil {
			return false, pathErr
		}
		if path != "" && filepath.Clean(filepath.Dir(releaseDirOf(path))) == want {
			return true, nil
		}
	}
	return false, nil
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
