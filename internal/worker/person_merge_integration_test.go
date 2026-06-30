package worker

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercises the person merge against a real Postgres inside a rolled-back
// transaction (no DB mutation). Skips in -short mode or when no DB is reachable.

func personMergeTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping DB integration test in short mode")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}
	pool, err := database.Connect(context.Background(), url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	return pool
}

func seedPerson(t *testing.T, ctx context.Context, qtx *sqlc.Queries, name string) int64 {
	t.Helper()
	p, err := qtx.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: []byte("{}"), Name: name, AlsoKnownAs: []string{},
		Popularity: pgtype.Numeric{Valid: true},
	})
	require.NoError(t, err)
	return p.ID
}

func seedMovieItem(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID int64, title string) int64 {
	t.Helper()
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: libID, MediaType: sqlc.MediaTypeMovie, Title: title, SortTitle: title,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	return item.ID
}

func TestMergePersonIntoTx(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	user, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "pmerge", Email: "pmerge@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "Movies", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)

	m1 := seedMovieItem(t, ctx, qtx, lib.ID, "Film One")
	m2 := seedMovieItem(t, ctx, qtx, lib.ID, "Film Two")
	dst := seedPerson(t, ctx, qtx, "Canonical")
	src := seedPerson(t, ctx, qtx, "Duplicate")

	// Cast: (m1,dst,Hero) + (m1,src,Hero) collide on reparent; (m2,src,Villain) is unique.
	require.NoError(t, qtx.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{MediaItemID: m1, PersonID: dst, Character: "Hero"}))
	require.NoError(t, qtx.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{MediaItemID: m1, PersonID: src, Character: "Hero"}))
	require.NoError(t, qtx.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{MediaItemID: m2, PersonID: src, Character: "Villain"}))
	// Crew: (m1,dst,Director) + (m1,src,Director) collide; (m2,src,Writer) is unique.
	require.NoError(t, qtx.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{MediaItemID: m1, PersonID: dst, Job: "Director"}))
	require.NoError(t, qtx.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{MediaItemID: m1, PersonID: src, Job: "Director"}))
	require.NoError(t, qtx.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{MediaItemID: m2, PersonID: src, Job: "Writer"}))

	require.NoError(t, mergePersonIntoTx(ctx, qtx, dst, src))

	// src person is gone.
	_, err = qtx.GetPersonByID(ctx, src)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// m1 keeps one cast/crew row, now pointing at dst (the collider was dropped).
	cast1, err := qtx.ListMediaCastSlim(ctx, m1)
	require.NoError(t, err)
	require.Len(t, cast1, 1)
	require.Equal(t, dst, cast1[0].ID)

	// m2's unique credit moved to dst.
	cast2, err := qtx.ListMediaCastSlim(ctx, m2)
	require.NoError(t, err)
	require.Len(t, cast2, 1)
	require.Equal(t, dst, cast2[0].ID)
	require.Equal(t, "Villain", cast2[0].Character)

	crew1, err := qtx.ListMediaCrewSlim(ctx, m1)
	require.NoError(t, err)
	require.Len(t, crew1, 1)
	require.Equal(t, dst, crew1[0].ID)

	crew2, err := qtx.ListMediaCrewSlim(ctx, m2)
	require.NoError(t, err)
	require.Len(t, crew2, 1)
	require.Equal(t, dst, crew2[0].ID)
	require.Equal(t, "Writer", crew2[0].Job)
}
