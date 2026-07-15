package matcher

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestStoreRichMetadataReplacesStaleCastAndCrew(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)

	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "authoritative-credit-replacement", MediaType: sqlc.MediaTypeTv,
		Paths: []string{"/tmp/authoritative-credit-replacement"}, CreatedBy: testutil.TestUserID(t, pool),
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeTv, Title: "The Grand Tour",
		SortTitle: "the grand tour", ProviderKind: "heya", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)

	staleJeremy, err := q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: []byte(`{"legacy_tmdb":"81113"}`), Name: "Jeremy Clarkson",
		AlsoKnownAs: []string{}, Popularity: numericFromFloat(1),
	})
	require.NoError(t, err)
	staleHost, err := q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: []byte(`{"legacy_tvmaze":"33361"}`), Name: "Jeremy Clarkson",
		AlsoKnownAs: []string{}, Popularity: numericFromFloat(1),
	})
	require.NoError(t, err)
	staleCrew, err := q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: []byte(`{"legacy_tmdb":"1222153"}`), Name: "James Engelsman",
		AlsoKnownAs: []string{}, Popularity: numericFromFloat(1),
	})
	require.NoError(t, err)

	require.NoError(t, q.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{
		MediaItemID: item.ID, PersonID: staleJeremy.ID, Character: "Jeremy Clarkson", Source: "legacy",
	}))
	require.NoError(t, q.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{
		MediaItemID: item.ID, PersonID: staleHost.ID, Character: "Self - Host", DisplayOrder: 1, Source: "legacy",
	}))
	require.NoError(t, q.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{
		MediaItemID: item.ID, PersonID: staleCrew.ID, Job: "Producer", Department: "Production", Source: "legacy",
	}))

	matcher := New(pool, MatchOptions{}, nil, nil).WithTx(tx)
	vanished, err := q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: []byte(`{"tmdb":"deleted-before-credit-write"}`), Name: "Merged Elsewhere",
		AlsoKnownAs: []string{}, Popularity: numericFromFloat(1),
	})
	require.NoError(t, err)
	require.NoError(t, q.DeletePerson(ctx, vanished.ID))
	err = matcher.replaceMediaPersonCredits(ctx, item.ID, []resolvedPersonCredit{{
		person: vanished,
		credit: richPersonCredit{isCast: true, name: vanished.Name, character: "Lead"},
	}})
	require.ErrorContains(t, err, "people still exist")
	cast, err := q.ListMediaCastSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, cast, 2, "a person merged before locking must preserve the prior cast")
	crew, err := q.ListMediaCrewSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, crew, 1, "a person merged before locking must preserve the prior crew")

	invalid := &metadata.MediaDetail{Cast: []metadata.CastMember{{
		CanonicalID: "not-a-uuid", ExternalIDs: map[string]string{"tmdb": "81113"},
		Name: "Jeremy Clarkson", Character: "Self - Host", Source: "heya",
	}}}
	require.Error(t, matcher.StoreRichMetadata(ctx, item.ID, invalid))
	cast, err = q.ListMediaCastSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, cast, 2, "an invalid current projection must preserve the prior cast")
	crew, err = q.ListMediaCrewSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, crew, 1, "an invalid current projection must preserve the prior crew")

	current := &metadata.MediaDetail{Cast: []metadata.CastMember{
		{
			CanonicalID: "cc9065a1-4a31-4f96-a868-ae278f915a35",
			ExternalIDs: map[string]string{"tmdb": "81113"}, Name: "Jeremy Clarkson",
			Character: "Self - Host", Order: 0, Source: "heya",
		},
		{
			CanonicalID: "53b6a5b0-8d4f-4c01-8e43-c3aa3b7a129a",
			ExternalIDs: map[string]string{"tmdb": "1222151"}, Name: "Richard Hammond",
			Character: "Self - Host", Order: 1, Source: "heya",
		},
	}}
	require.NoError(t, matcher.StoreRichMetadata(ctx, item.ID, current))

	cast, err = q.ListMediaCastSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, cast, 2)
	require.Equal(t, "Jeremy Clarkson", cast[0].Name)
	require.Equal(t, "Richard Hammond", cast[1].Name)
	crew, err = q.ListMediaCrewSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Empty(t, crew, "an authoritative empty crew list must clear stale crew")

	// Retrying the same projection is stable, then a genuinely empty canonical
	// projection clears both relationship sets without deleting person records.
	require.NoError(t, matcher.StoreRichMetadata(ctx, item.ID, current))
	cast, err = q.ListMediaCastSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, cast, 2)
	require.NoError(t, matcher.StoreRichMetadata(ctx, item.ID, &metadata.MediaDetail{}))
	cast, err = q.ListMediaCastSlim(ctx, item.ID)
	require.NoError(t, err)
	require.Empty(t, cast)
}
