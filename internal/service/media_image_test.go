package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func asset(at, label string, sort int, local, remote string) sqlc.MediaAsset {
	return sqlc.MediaAsset{
		AssetType: sqlc.AssetType(at),
		Label:     label,
		SortOrder: int32(sort),
		LocalPath: local,
		RemoteUrl: remote,
	}
}

func TestPickMediaAsset(t *testing.T) {
	// Ordered like ListMediaAssets (asset_type, sort_order): primary poster
	// before season poster; primary backdrop before carousel extras.
	assets := []sqlc.MediaAsset{
		asset("backdrop", "", 0, "", "http://cdn/bd0.webp"),
		asset("backdrop", "en", 11, "", "http://cdn/bd11.webp"),
		asset("backdrop", "en", 12, "", "http://cdn/bd12.webp"),
		asset("poster", "", 0, "", "http://cdn/p0.webp"),
		asset("poster", "en", 11, "", "http://cdn/poster-en.webp"),
		asset("poster", "season-1", 1001, "", "http://cdn/s1.webp"),
	}

	// Bare poster request (sort=-1, no label) must pick the primary, never the
	// labeled season poster.
	if got := pickMediaAsset(assets, "poster", -1, ""); got == nil || got.RemoteUrl != "http://cdn/p0.webp" {
		t.Fatalf("bare poster: want primary p0, got %+v", got)
	}
	// Labeled request picks the season row.
	if got := pickMediaAsset(assets, "poster", -1, "season-1"); got == nil || got.RemoteUrl != "http://cdn/s1.webp" {
		t.Fatalf("season poster: want s1, got %+v", got)
	}
	// Sort-exact request picks the carousel backdrop.
	if got := pickMediaAsset(assets, "backdrop", 11, ""); got == nil || got.RemoteUrl != "http://cdn/bd11.webp" {
		t.Fatalf("backdrop sort 11: want bd11, got %+v", got)
	}
	// A label used by several assets must still respect both type and sort.
	if got := pickMediaAsset(assets, "backdrop", 12, "en"); got == nil || got.RemoteUrl != "http://cdn/bd12.webp" {
		t.Fatalf("English backdrop sort 12: want bd12, got %+v", got)
	}
	if got := pickMediaAsset(assets, "poster", 11, "en"); got == nil || got.RemoteUrl != "http://cdn/poster-en.webp" {
		t.Fatalf("English poster sort 11: want poster-en, got %+v", got)
	}
	// Bare backdrop picks the primary.
	if got := pickMediaAsset(assets, "backdrop", -1, ""); got == nil || got.RemoteUrl != "http://cdn/bd0.webp" {
		t.Fatalf("bare backdrop: want bd0, got %+v", got)
	}
	// No match.
	if got := pickMediaAsset(assets, "logo", -1, ""); got != nil {
		t.Fatalf("logo: want nil, got %+v", got)
	}
}

func TestPickMediaAsset_LocalWins(t *testing.T) {
	assets := []sqlc.MediaAsset{asset("poster", "", 0, "/data/p.jpg", "http://cdn/p.webp")}
	got := pickMediaAsset(assets, "poster", -1, "")
	if got == nil || got.LocalPath != "/data/p.jpg" {
		t.Fatalf("want row with local path, got %+v", got)
	}
}

func TestImageCacheFilename(t *testing.T) {
	cases := []struct {
		at   string
		sort int
		url  string
		want string
	}{
		{"poster", 0, "https://media.heya.media/x/abc.webp", "poster.webp"},
		{"backdrop", 0, "https://media.heya.media/x/abc.webp", "backdrop.webp"},
		{"poster", 1001, "https://media.heya.media/x/s1.webp", "poster1001.webp"},
		{"still", 2103, "https://media.heya.media/x/e.jpg", "still2103.jpg"},
		{"poster", 0, "https://media.heya.media/noext", "poster.jpg"},
	}
	for _, c := range cases {
		if got := imageCacheFilename(c.at, c.sort, c.url); got != c.want {
			t.Errorf("imageCacheFilename(%q,%d,%q) = %q, want %q", c.at, c.sort, c.url, got, c.want)
		}
	}
}

// Reproduces the divergence NEEDY GIRL OVERDOSE exhibited in a live library:
// a local sidecar poster in media_assets, while media_items.poster_path still
// held an upstream CDN URL that no asset row referenced. Rails read the column
// and detail pages read the endpoint, so one title showed two different
// posters depending on the screen.
func TestGetMediaImagePathReconcilesPrimaryArtColumn(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "primary-art-reconcile-test",
		MediaType:    sqlc.MediaTypeTv,
		Paths:        []string{"/tmp/primary-art-reconcile"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte(`{"use_local_data":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	const orphanedCDN = "https://heya.media/api/v2/images/2b454d68/variants/webp/1920"
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ProviderKind: "heya",
		Title: "Two Posters", SortTitle: "two posters",
		PosterPath: orphanedCDN,
	})
	require.NoError(t, err)

	// The sidecar the endpoint actually serves, plus a season poster that must
	// stay out of the primary slot.
	const sidecar = "/storage/TV/Two Posters/poster.jpg"
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster, Source: "local",
		LocalPath: sidecar,
	})
	require.NoError(t, err)
	const seasonPoster = "/data/images/two-posters/season01-poster.jpg"
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster, Source: "local",
		LocalPath: seasonPoster, Label: "season-1",
	})
	require.NoError(t, err)

	app := &App{db: pool, config: &config.Config{DataDir: config.Field[string]{Value: t.TempDir()}}}

	served, ok := app.GetMediaImagePath(ctx, item.ID, "poster", -1, "")
	require.True(t, ok)
	require.Equal(t, sidecar, served)

	reconciled, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, sidecar, reconciled.PosterPath,
		"poster_path must follow what the image endpoint serves, not the orphaned CDN URL")

	// A season poster is not the item's primary art and must not claim the slot.
	served, ok = app.GetMediaImagePath(ctx, item.ID, "poster", -1, "season-1")
	require.True(t, ok)
	require.Equal(t, seasonPoster, served)

	after, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, sidecar, after.PosterPath, "a labeled request must not overwrite primary art")
	require.Equal(t, reconciled.UpdatedAt, after.UpdatedAt,
		"an already-reconciled column must not bump updated_at — clients read it as an image-cache revision")
}

func TestMetadataImagePathWaitsForCanonicalBytes(t *testing.T) {
	t.Parallel()
	var canonical bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixel.Set(0, 0, color.RGBA{R: 120, G: 40, B: 200, A: 255})
	if err := jpeg.Encode(&canonical, pixel, nil); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(canonical.Bytes())
	}))
	defer metadataServer.Close()

	dataDir := t.TempDir()
	app := &App{
		config:     &config.Config{HeyaMetadataURL: config.Field[string]{Value: metadataServer.URL}},
		downloader: images.NewDownloader(dataDir, images.TrustedSource{BaseURL: metadataServer.URL}),
	}
	path, ok := app.GetMetadataImagePath(context.Background(), "00000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatal("canonical metadata image was not materialized")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, canonical.Bytes()) {
		t.Fatal("stored body does not match canonical JPEG")
	}
	if requests.Load() != 2 {
		t.Fatalf("metadata requests = %d, want 2", requests.Load())
	}
}
