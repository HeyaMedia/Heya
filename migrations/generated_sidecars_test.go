package migrations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSidecarMigrationRejectsIncompleteSignatures(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	digest := make([]byte, 32)

	tests := []struct {
		name       string
		path       string
		query      string
		constraint string
		args       []any
	}{
		{
			name: "published digest missing alongside valid pending intent",
			path: "/migration-test/incomplete-published-" + uuid.NewString(),
			query: `
				INSERT INTO generated_sidecar_publications (
					path,
					published_size, published_mtime, published_sha256, published_at,
					pending_intent_id, pending_size, pending_sha256,
					pending_staged_path, pending_previous_path, pending_lease_expires_at
				) VALUES ($1, 12, now(), NULL, now(), $2, 12, $3, $4, $5, now())
			`,
			constraint: "generated_sidecar_publications_published_complete",
			args: []any{
				uuid.New(), digest,
				"/migration-test/staged-" + uuid.NewString(),
				"/migration-test/previous-" + uuid.NewString(),
			},
		},
		{
			name: "pending size missing alongside valid published signature",
			path: "/migration-test/incomplete-pending-" + uuid.NewString(),
			query: `
				INSERT INTO generated_sidecar_publications (
					path,
					published_size, published_mtime, published_sha256, published_at,
					pending_intent_id, pending_size, pending_sha256,
					pending_staged_path, pending_previous_path, pending_lease_expires_at
				) VALUES ($1, 12, now(), $2, now(), $3, NULL, $2, $4, $5, now())
			`,
			constraint: "generated_sidecar_publications_pending_complete",
			args: []any{
				digest, uuid.New(),
				"/migration-test/staged-" + uuid.NewString(),
				"/migration-test/previous-" + uuid.NewString(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append([]any{test.path}, test.args...)
			_, err := pool.Exec(ctx, test.query, args...)
			require.Error(t, err)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, test.constraint, pgErr.ConstraintName)
		})
	}
}
