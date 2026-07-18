package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorAggregatesWithoutArgumentsOrLiterals(t *testing.T) {
	c := newCollector(4, 8)
	ctx := c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM users WHERE email = 'private@example.com' AND id = 42",
		Args: []any{"also-private"},
	})
	time.Sleep(time.Millisecond)
	c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

	snapshot := c.Snapshot()
	require.Len(t, snapshot.TopStatements, 1)
	assert.Equal(t, uint64(1), snapshot.TotalQueries)
	assert.Equal(t, uint64(1), snapshot.TopStatements[0].Calls)
	assert.NotContains(t, snapshot.TopStatements[0].Statement, "private@example.com")
	assert.NotContains(t, snapshot.TopStatements[0].Statement, "also-private")
	assert.NotContains(t, snapshot.TopStatements[0].Statement, "42")
}

func TestCollectorBoundsStatementsAndRecentSamples(t *testing.T) {
	c := newCollector(2, 2)
	for _, sql := range []string{"SELECT one", "SELECT two", "SELECT three"} {
		ctx := c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql})
		c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	}

	snapshot := c.Snapshot()
	assert.Equal(t, uint64(3), snapshot.TotalQueries)
	assert.LessOrEqual(t, snapshot.TrackedStatements, 2)
	assert.LessOrEqual(t, snapshot.QueriesPerSecond, 2.0)
}

func TestCollectorCountsErrors(t *testing.T) {
	c := newCollector(4, 4)
	ctx := c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT broken"})
	c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "23505", Message: "duplicate secret value"}})

	snapshot := c.Snapshot()
	assert.Equal(t, uint64(1), snapshot.TotalErrors)
	assert.Equal(t, uint64(1), snapshot.RecentErrors)
	assert.Equal(t, uint64(1), snapshot.TopStatements[0].Errors)
	assert.Equal(t, uint64(1), snapshot.TopStatements[0].RecentErrors)
	assert.Equal(t, "23505", snapshot.TopStatements[0].LastErrorCode)
	assert.NotContains(t, snapshot.TopStatements[0].LastErrorCode, "secret")
}

func TestCollectorIgnoresDiagnosticsProbes(t *testing.T) {
	c := newCollector(4, 4)
	ctx := c.TraceQueryStart(WithoutQueryTrace(context.Background()), nil, pgx.TraceQueryStartData{SQL: "SELECT pg_database_size(current_database())"})
	c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	snapshot := c.Snapshot()
	assert.Zero(t, snapshot.TotalQueries)
	assert.Empty(t, snapshot.TopStatements)
}

func TestCollectorReportsRecentPerStatementSignals(t *testing.T) {
	c := newCollector(4, 8)
	for range 2 {
		ctx := c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM tracks WHERE id = $1"})
		c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	}

	snapshot := c.Snapshot()
	require.Len(t, snapshot.TopStatements, 1)
	assert.Equal(t, uint64(2), snapshot.TopStatements[0].RecentCalls)
	assert.False(t, snapshot.TopStatements[0].LastSeenAt.IsZero())
	assert.GreaterOrEqual(t, snapshot.TopStatements[0].RecentP95MS, 0.0)
}

func TestSanitizeStatementNormalizesWhitespaceAndLiterals(t *testing.T) {
	got := SanitizeStatement("\n SELECT  *  FROM media WHERE title='Don''t' AND year=2026 AND id=$1 LIMIT 10 ")
	assert.Equal(t, "SELECT * FROM media WHERE title=? AND year=? AND id=$1 LIMIT ?", got)
}

func TestQueryErrorCodeFallsBackWithoutExposingMessage(t *testing.T) {
	assert.Equal(t, "query_error", queryErrorCode(errors.New("credential leaked here")))
}
