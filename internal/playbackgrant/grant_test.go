package playbackgrant

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type fakeSessions struct {
	session sqlc.Session
	user    sqlc.User
	revoked bool
}

func (f *fakeSessions) GetSessionWithUserByToken(_ context.Context, tokenHash string) (sqlc.GetSessionWithUserByTokenRow, error) {
	if f.revoked || tokenHash != f.session.TokenHash {
		return sqlc.GetSessionWithUserByTokenRow{}, pgx.ErrNoRows
	}
	return sqlc.GetSessionWithUserByTokenRow{Session: f.session, User: f.user}, nil
}

func (f *fakeSessions) GetUserByID(_ context.Context, id int64) (sqlc.User, error) {
	if f.revoked || id != f.user.ID {
		return sqlc.User{}, pgx.ErrNoRows
	}
	return f.user, nil
}

func (*fakeSessions) TouchSession(context.Context, string) error { return nil }

func TestGrantIsOpaqueSessionBoundAndSubtreeScoped(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	mgr := New()
	mgr.now = func() time.Time { return now }
	sessions := &fakeSessions{
		session: sqlc.Session{UserID: 7, TokenHash: auth.TokenHash("session-secret")},
		user:    sqlc.User{ID: 7, Username: "alice"},
	}

	grant, expiresAt, err := mgr.Issue(7, "session-secret", "/api/playback/native/media/file-a", true, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if grant == "session-secret" || len(grant) != 64 {
		t.Fatalf("grant was not an independent opaque token: %q", grant)
	}
	if want := now.Add(2 * time.Hour); !expiresAt.Equal(want) {
		t.Fatalf("expiry = %s, want %s", expiresAt, want)
	}
	for _, path := range []string{
		"/api/playback/native/media/file-a",
		"/api/playback/native/media/file-a/hls/master.m3u8",
		"/api/playback/native/media/file-a/hls/seg_0001.ts",
		"/api/playback/native/media/file-a/subtitles/2",
	} {
		if userID, err := mgr.Validate(context.Background(), sessions, grant, path); err != nil || userID != 7 {
			t.Fatalf("validate %q = user %d, err %v", path, userID, err)
		}
	}
	for _, path := range []string{
		"/api/playback/native/media/file-ab",
		"/api/playback/native/media/file-b/hls/master.m3u8",
		"/api/stream/file-a",
	} {
		if _, err := mgr.Validate(context.Background(), sessions, grant, path); err == nil {
			t.Fatalf("grant escaped scope to %q", path)
		}
	}

	sessions.revoked = true
	if _, err := mgr.Validate(context.Background(), sessions, grant, "/api/playback/native/media/file-a"); err == nil {
		t.Fatal("revoked auth session retained native playback access")
	}
}

func TestGrantExpiryAndInputValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	mgr := New()
	mgr.now = func() time.Time { return now }
	sessions := &fakeSessions{
		session: sqlc.Session{UserID: 7, TokenHash: auth.TokenHash("session-secret")},
		user:    sqlc.User{ID: 7},
	}
	grant, expiresAt, err := mgr.Issue(7, "session-secret", "/api/playback/native/media/file-a", false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(minTTL); !expiresAt.Equal(want) {
		t.Fatalf("minimum expiry = %s, want %s", expiresAt, want)
	}
	now = expiresAt
	if _, err := mgr.Validate(context.Background(), sessions, grant, "/api/playback/native/media/file-a"); err == nil {
		t.Fatal("expired grant was accepted")
	}

	for _, scope := range []string{"", "relative", "/path?query=1", "/path#fragment", `/path\\escape`} {
		if _, _, err := mgr.Issue(7, "session-secret", scope, false, time.Hour); err == nil {
			t.Fatalf("invalid scope %q was accepted", scope)
		}
	}
}
