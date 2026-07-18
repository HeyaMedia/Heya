package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeSubtitleFilename(t *testing.T) {
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("movie.en.srt"))
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("../movie.en.srt"))
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("dir\\movie.en.srt"))
	assert.Empty(t, safeSubtitleFilename("movie.exe"))
	assert.Empty(t, safeSubtitleFilename(".."))
}

func TestSafeSubtitleLanguage(t *testing.T) {
	assert.Equal(t, "en", safeSubtitleLanguage("en"))
	assert.Equal(t, "pt-BR", safeSubtitleLanguage("pt-BR"))
	assert.Equal(t, "zh_Hant", safeSubtitleLanguage("zh_Hant"))
	assert.Equal(t, "und", safeSubtitleLanguage("../evil"))
	assert.Equal(t, "und", safeSubtitleLanguage(""))
}

func TestSafeSubtitleDownloadURL(t *testing.T) {
	u, err := safeSubtitleDownloadURL("https://dl.opensubtitles.com/en/subtitle.srt?download=1")
	assert.NoError(t, err)
	assert.Equal(t, "https://dl.opensubtitles.com/en/subtitle.srt?download=1", u)

	for _, raw := range []string{
		"",
		"/relative/subtitle.srt",
		"http://example.com/subtitle.srt",
		"file:///etc/passwd",
		"https://127.0.0.1/private.srt",
		"https://169.254.169.254/latest/meta-data",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := safeSubtitleDownloadURL(raw)
			assert.Error(t, err)
		})
	}
}

func TestSaveDownloadedSubtitlePublishesAfterAssetCreation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "movie.en.srt")
	store := &subtitleAssetStoreStub{createAsset: sqlc.MediaAsset{ID: 42, AssetType: sqlc.AssetTypeSubtitle}}

	asset, err := saveDownloadedSubtitle(t.Context(), store, destination, []byte("new subtitle"), sqlc.CreateMediaAssetParams{
		MediaItemID: 7,
		AssetType:   sqlc.AssetTypeSubtitle,
		Source:      "opensubtitles",
		Language:    "en",
	})
	require.NoError(t, err)
	require.Equal(t, int64(42), asset.ID)
	require.Equal(t, destination, store.createParams.LocalPath)
	require.Equal(t, int64(len("new subtitle")), store.createParams.FileSize)
	stored, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "new subtitle", string(stored))
}

func TestSaveDownloadedSubtitleRestoresExistingFileOnDatabaseFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "movie.en.srt")
	require.NoError(t, os.WriteFile(destination, []byte("existing subtitle"), 0o640))
	store := &subtitleAssetStoreStub{createErr: errors.New("database unavailable")}

	_, err := saveDownloadedSubtitle(t.Context(), store, destination, []byte("replacement"), sqlc.CreateMediaAssetParams{
		MediaItemID: 7,
		AssetType:   sqlc.AssetTypeSubtitle,
	})
	require.Error(t, err)
	stored, readErr := os.ReadFile(destination)
	require.NoError(t, readErr)
	require.Equal(t, "existing subtitle", string(stored))
}

func TestSaveDownloadedSubtitleRemovesNewFileOnDatabaseFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "movie.en.srt")
	store := &subtitleAssetStoreStub{createErr: errors.New("database unavailable")}

	_, err := saveDownloadedSubtitle(t.Context(), store, destination, []byte("subtitle"), sqlc.CreateMediaAssetParams{
		MediaItemID: 7,
		AssetType:   sqlc.AssetTypeSubtitle,
	})
	require.Error(t, err)
	_, statErr := os.Stat(destination)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestSaveDownloadedSubtitleResolvesIdempotentAssetConflict(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "movie.en.srt")
	require.NoError(t, os.WriteFile(destination, []byte("old"), 0o640))
	existing := sqlc.MediaAsset{ID: 99, MediaItemID: 7, AssetType: sqlc.AssetTypeSubtitle, LocalPath: destination}
	store := &subtitleAssetStoreStub{createErr: pgx.ErrNoRows, listedAssets: []sqlc.MediaAsset{existing}}

	asset, err := saveDownloadedSubtitle(t.Context(), store, destination, []byte("updated"), sqlc.CreateMediaAssetParams{
		MediaItemID: 7,
		AssetType:   sqlc.AssetTypeSubtitle,
	})
	require.NoError(t, err)
	require.Equal(t, existing.ID, asset.ID)
	require.Equal(t, int64(len("updated")), asset.FileSize)
	require.Equal(t, int64(len("updated")), store.updateParams.FileSize)
	stored, readErr := os.ReadFile(destination)
	require.NoError(t, readErr)
	require.Equal(t, "updated", string(stored))
}

func TestSaveDownloadedSubtitleRollsBackUnresolvedConflict(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "movie.en.srt")
	require.NoError(t, os.WriteFile(destination, []byte("old"), 0o640))
	store := &subtitleAssetStoreStub{createErr: pgx.ErrNoRows, listedAssets: []sqlc.MediaAsset{{
		ID: 1, MediaItemID: 7, AssetType: sqlc.AssetTypeSubtitle, LocalPath: filepath.Join(filepath.Dir(destination), "different.srt"),
	}}}

	_, err := saveDownloadedSubtitle(t.Context(), store, destination, []byte("updated"), sqlc.CreateMediaAssetParams{
		MediaItemID: 7,
		AssetType:   sqlc.AssetTypeSubtitle,
	})
	require.Error(t, err)
	stored, readErr := os.ReadFile(destination)
	require.NoError(t, readErr)
	require.Equal(t, "old", string(stored))
}

type subtitleAssetStoreStub struct {
	createAsset  sqlc.MediaAsset
	createErr    error
	createParams sqlc.CreateMediaAssetParams
	listedAssets []sqlc.MediaAsset
	listErr      error
	updateParams sqlc.UpdateMediaAssetMaterializationParams
	updateErr    error
}

func (s *subtitleAssetStoreStub) CreateMediaAsset(_ context.Context, params sqlc.CreateMediaAssetParams) (sqlc.MediaAsset, error) {
	s.createParams = params
	return s.createAsset, s.createErr
}

func (s *subtitleAssetStoreStub) ListMediaAssets(context.Context, int64) ([]sqlc.MediaAsset, error) {
	return s.listedAssets, s.listErr
}

func (s *subtitleAssetStoreStub) UpdateMediaAssetMaterialization(_ context.Context, params sqlc.UpdateMediaAssetMaterializationParams) (sqlc.MediaAsset, error) {
	s.updateParams = params
	if s.updateErr != nil {
		return sqlc.MediaAsset{}, s.updateErr
	}
	for _, asset := range s.listedAssets {
		if asset.ID == params.ID {
			asset.LocalPath = params.LocalPath
			asset.FileSize = params.FileSize
			return asset, nil
		}
	}
	return sqlc.MediaAsset{}, pgx.ErrNoRows
}
