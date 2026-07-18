package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/testutil"
)

func TestSetUserPlaylistCoverUsesDecodedFormatAndPreservesValidCover(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	playlist, err := q.CreateUserPlaylist(ctx, sqlc.CreateUserPlaylistParams{
		UserID: userID, Name: "Safe cover", Slug: fmt.Sprintf("safe-cover-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = q.DeleteUserPlaylist(ctx, sqlc.DeleteUserPlaylistParams{ID: playlist.ID, UserID: userID})
	})
	dataDir := t.TempDir()
	app := &App{db: pool, config: &config.Config{DataDir: config.Field[string]{Value: dataDir}}}

	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := app.SetUserPlaylistCover(ctx, userID, playlist.ID, bytes.NewReader(body.Bytes())); err != nil {
		t.Fatal(err)
	}
	stored, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlist.ID, UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(stored.CoverPath) != ".png" {
		t.Fatalf("cover path = %q, want decoded .png extension", stored.CoverPath)
	}
	coverPath := filepath.Join(dataDir, stored.CoverPath)
	publicPath, err := app.GetUserPlaylistCoverPath(ctx, 0, playlist.ID)
	realCoverPath, evalErr := filepath.EvalSymlinks(coverPath)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if err != nil || publicPath != realCoverPath {
		t.Fatalf("public cover path = %q, %v; want %q", publicPath, err, realCoverPath)
	}
	want, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatal(err)
	}

	err = app.SetUserPlaylistCover(ctx, userID, playlist.ID, strings.NewReader("not an image"))
	if !errors.Is(err, images.ErrInvalidImage) {
		t.Fatalf("invalid replacement error = %v, want ErrInvalidImage", err)
	}
	got, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("rejected replacement changed the existing playlist cover")
	}

}

func TestPlaylistCoverPathRejectsTraversalAndUnexpectedShape(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	if _, ok := managedPlaylistCoverPath(dataDir, 1, 2, "../../etc/passwd"); ok {
		t.Fatal("accepted traversal path")
	}
	if _, ok := managedPlaylistCoverPath(dataDir, 1, 2, "images/playlists/1/other.png"); ok {
		t.Fatal("accepted another playlist's filename")
	}
	if _, ok := managedPlaylistCoverPath(dataDir, 1, 2, "images/playlists/9/2.png"); ok {
		t.Fatal("accepted another owner's directory")
	}
	if _, ok := managedPlaylistCoverPath(dataDir, 1, 2, "images/playlists/1/2.gif"); ok {
		t.Fatal("accepted an unbounded animated GIF cover")
	}
}
