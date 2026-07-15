package matcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
)

// These benchmarks model the two payloads that exposed the original
// row-at-a-time persistence cost: a long-running TV catalog and a title with a
// very large canonical credit list. Run with:
//
//	go test ./internal/matcher -run '^$' -bench 'BenchmarkMetadataPersistence' -benchtime=3x
func BenchmarkMetadataPersistence(b *testing.B) {
	pool := testutil.SetupDB(b)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "metadata-persistence-benchmark", MediaType: sqlc.MediaTypeTv,
		Paths:        []string{"/tmp/metadata-persistence-benchmark"},
		CreatedBy:    testutil.TestUserID(b, pool),
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		Settings:     []byte("{}"),
	})
	if err != nil {
		b.Fatal(err)
	}
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeTv, Title: "Persistence Benchmark",
		SortTitle: "persistence benchmark", ProviderKind: "heya", ExternalIds: []byte("{}"),
	})
	if err != nil {
		b.Fatal(err)
	}
	m := New(pool, MatchOptions{}, nil, nil).WithTx(tx)

	b.Run("tv_structure_750_episodes", func(b *testing.B) {
		detail := benchmarkTVDetail(25, 30)
		if err := m.StoreEntityMetadata(ctx, item.ID, metadata.KindTV, detail); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for range b.N {
			if err := m.StoreEntityMetadata(ctx, item.ID, metadata.KindTV, detail); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(750, "episodes/op")
	})

	b.Run("rich_projection_2000_credits_warm", func(b *testing.B) {
		detail := benchmarkCreditDetail(2000, "warm")
		if err := m.StoreRichMetadata(ctx, item.ID, detail); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for range b.N {
			if err := m.StoreRichMetadata(ctx, item.ID, detail); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(2000, "credits/op")
	})

	b.Run("rich_projection_2000_credits_new", func(b *testing.B) {
		b.ReportMetric(2000, "credits/op")
		for range b.N {
			detail := benchmarkCreditDetail(2000, "new-"+uuid.NewString())
			if err := m.StoreRichMetadata(ctx, item.ID, detail); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkTVDetail(seasons, episodesPerSeason int) *metadata.MediaDetail {
	detail := &metadata.MediaDetail{SchemaVersion: 1, ProjectionVersion: 1}
	for seasonNumber := 1; seasonNumber <= seasons; seasonNumber++ {
		season := metadata.SeasonDetail{
			CanonicalID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("season-%d", seasonNumber))).String(),
			Number:      seasonNumber, Title: fmt.Sprintf("Season %d", seasonNumber),
		}
		for episodeNumber := 1; episodeNumber <= episodesPerSeason; episodeNumber++ {
			identity := fmt.Sprintf("episode-%d-%d", seasonNumber, episodeNumber)
			season.Episodes = append(season.Episodes, metadata.EpisodeDetail{
				CanonicalID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String(),
				Number:      episodeNumber, Title: identity, Overview: "Canonical overview",
				Titles: []metadata.TitleEntry{
					{Title: identity, Language: "en", Source: "heya"},
					{Title: "Localized " + identity, Language: "da", Source: "heya"},
				},
				Overviews: map[string]string{"en": "English", "da": "Danish"},
			})
		}
		detail.Seasons = append(detail.Seasons, season)
	}
	detail.NumberOfSeasons = seasons
	detail.NumberOfEpisodes = seasons * episodesPerSeason
	return detail
}

func benchmarkCreditDetail(count int, namespace string) *metadata.MediaDetail {
	detail := &metadata.MediaDetail{}
	for i := 0; i < count; i++ {
		identity := fmt.Sprintf("%s-person-%d", namespace, i)
		canonicalID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String()
		if i%3 == 0 {
			detail.Crew = append(detail.Crew, metadata.CrewMember{
				CanonicalID: canonicalID, ExternalIDs: map[string]string{"tmdb": identity},
				Name: "Person " + identity, Job: "Producer", Department: "Production", Source: "heya",
			})
			continue
		}
		detail.Cast = append(detail.Cast, metadata.CastMember{
			CanonicalID: canonicalID, ExternalIDs: map[string]string{"tmdb": identity},
			Name: "Person " + identity, Character: fmt.Sprintf("Character %d", i),
			Order: i, Source: "heya",
		})
	}
	return detail
}
