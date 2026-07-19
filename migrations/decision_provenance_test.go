package migrations_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/karbowiak/heya/migrations"
	"github.com/stretchr/testify/require"
)

func TestDecisionProvenanceMigrationPreservesOnlyProvenManualAccepts(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Run the migration's exact UPDATE against an isolated miniature schema so
	// this regression cannot relabel identities owned by concurrently running
	// integration tests in the shared development database.
	schema := fmt.Sprintf("migration_59_%d", time.Now().UnixNano())
	qualified := pgx.Identifier{schema}.Sanitize()
	_, err = tx.Exec(ctx, "CREATE SCHEMA "+qualified)
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE %s.local_media_identities (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			library_id bigint NOT NULL, media_type text NOT NULL,
			review_status text NOT NULL, decision_provenance text NOT NULL
		)`,
		`CREATE TABLE %s.metadata_match_candidates (
			identity_id bigint NOT NULL, scan_run_id bigint,
			status text NOT NULL, rank integer NOT NULL, updated_at timestamptz NOT NULL
		)`,
		`CREATE TABLE %s.scan_runs (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			library_id bigint NOT NULL, media_type text NOT NULL,
			created_at timestamptz NOT NULL, finished_at timestamptz
		)`,
		`CREATE TABLE %s.scan_findings (
			identity_id bigint NOT NULL, code text NOT NULL, resolved_at timestamptz
		)`,
	} {
		_, err = tx.Exec(ctx, fmt.Sprintf(ddl, qualified))
		require.NoError(t, err)
	}

	createdAt := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	finishedAt := createdAt.Add(10 * time.Minute)
	var scanRunID int64
	require.NoError(t, tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO %s.scan_runs (library_id, media_type, created_at, finished_at)
		VALUES (42, 'music', $1, $2) RETURNING id
	`, qualified), createdAt, finishedAt).Scan(&scanRunID))

	identityNames := make(map[int64]string)
	insertIdentity := func(key, status string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, tx.QueryRow(ctx, fmt.Sprintf(`
			INSERT INTO %s.local_media_identities
				(library_id, media_type, review_status, decision_provenance)
			VALUES (42, 'music', $1, 'legacy') RETURNING id
		`, qualified), status).Scan(&id))
		// Keep readable scenario names outside the intentionally minimal table.
		identityNames[id] = key
		return id
	}
	insertSelected := func(identityID int64, rank int, updatedAt time.Time) {
		t.Helper()
		_, insertErr := tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s.metadata_match_candidates
				(identity_id, scan_run_id, status, rank, updated_at)
			VALUES ($1, $2, 'selected', $3, $4)
		`, qualified), identityID, scanRunID, rank, updatedAt)
		require.NoError(t, insertErr)
	}
	insertFinding := func(identityID int64, code string, resolvedAt time.Time) {
		t.Helper()
		_, insertErr := tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s.scan_findings (identity_id, code, resolved_at)
			VALUES ($1, $2, $3)
		`, qualified), identityID, code, resolvedAt)
		require.NoError(t, insertErr)
	}

	rankZero := insertIdentity("rank-zero manual search", "accepted")
	insertSelected(rankZero, 0, createdAt.Add(time.Minute))
	postScanSelection := insertIdentity("selected after scan", "accepted")
	insertSelected(postScanSelection, 1, finishedAt.Add(time.Minute))
	manualResolution := insertIdentity("manual search-rejected resolution", "accepted")
	insertSelected(manualResolution, 1, createdAt.Add(time.Minute))
	insertFinding(manualResolution, "search_rejected", finishedAt.Add(2*time.Minute))
	manualNonSearchResolution := insertIdentity("manual music issue resolution", "accepted")
	insertSelected(manualNonSearchResolution, 1, createdAt.Add(time.Minute))
	insertFinding(manualNonSearchResolution, "music_track_issue", finishedAt.Add(2*time.Minute))
	automaticBoundary := insertIdentity("automatic scan-boundary resolution", "accepted")
	insertSelected(automaticBoundary, 1, createdAt.Add(time.Minute))
	insertFinding(automaticBoundary, "search_rejected", finishedAt)
	bulkMaterializationResolution := insertIdentity("bulk-approved materialization finding", "accepted")
	insertSelected(bulkMaterializationResolution, 1, createdAt.Add(time.Minute))
	insertFinding(bulkMaterializationResolution, "materialization_blocked", finishedAt.Add(2*time.Minute))
	rejected := insertIdentity("explicit rejection", "rejected")
	ignored := insertIdentity("explicit ignore", "ignored")

	migrationSQL, err := migrations.FS.ReadFile("00059_scanner_decision_provenance.sql")
	require.NoError(t, err)
	updateStart := strings.Index(string(migrationSQL), "WITH scan_boundaries AS MATERIALIZED")
	require.NotEqual(t, -1, updateStart)
	updateEnd := strings.Index(string(migrationSQL)[updateStart:], ";")
	require.NotEqual(t, -1, updateEnd)
	update := string(migrationSQL)[updateStart : updateStart+updateEnd+1]
	update = strings.ReplaceAll(update, "public.", qualified+".")
	_, err = tx.Exec(ctx, update)
	require.NoError(t, err)

	rows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT id, decision_provenance FROM %s.local_media_identities ORDER BY id
	`, qualified))
	require.NoError(t, err)
	defer rows.Close()
	got := make(map[string]string)
	for rows.Next() {
		var id int64
		var provenance string
		require.NoError(t, rows.Scan(&id, &provenance))
		got[identityNames[id]] = provenance
	}
	require.NoError(t, rows.Err())
	require.Equal(t, "manual", got[identityNames[rankZero]])
	require.Equal(t, "manual", got[identityNames[postScanSelection]])
	require.Equal(t, "manual", got[identityNames[manualResolution]])
	require.Equal(t, "manual", got[identityNames[manualNonSearchResolution]])
	require.Equal(t, "legacy", got[identityNames[automaticBoundary]])
	require.Equal(t, "manual", got[identityNames[bulkMaterializationResolution]])
	require.Equal(t, "manual", got[identityNames[rejected]])
	require.Equal(t, "manual", got[identityNames[ignored]])
}
