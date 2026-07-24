package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/diagnostics"
	"github.com/rs/zerolog/log"
)

// Manual PostgreSQL maintenance: VACUUM / ANALYZE / REINDEX across every
// user table, plus a pg_stat_statements counter reset. Shared by the
// `heya db:maintain` CLI (synchronous) and POST /api/admin/db/maintenance
// (background, progress polled via GET /api/admin/db).
//
// One operation at a time — VACUUM and REINDEX already serialize per table
// inside PostgreSQL, and overlapping runs would only fight for the same
// locks. Statements run outside any transaction (pool.Exec autocommits),
// which VACUUM and REINDEX CONCURRENTLY require.

// DBMaintenanceOp enumerates the supported maintenance operations.
type DBMaintenanceOp string

const (
	// DBMaintenanceVacuumAnalyze runs VACUUM (ANALYZE) per user table:
	// reclaims dead rows for reuse and refreshes planner statistics.
	DBMaintenanceVacuumAnalyze DBMaintenanceOp = "vacuum_analyze"
	// DBMaintenanceAnalyze refreshes planner statistics only — cheap, and
	// the fix when plans go bad after bulk changes (or a pg_upgrade, which
	// resets optimizer stats entirely).
	DBMaintenanceAnalyze DBMaintenanceOp = "analyze"
	// DBMaintenanceReindex rebuilds every index CONCURRENTLY, compacting
	// index bloat without blocking writes.
	DBMaintenanceReindex DBMaintenanceOp = "reindex"
	// DBMaintenanceResetQueryStats zeroes pg_stat_statements so the
	// "expensive statements" panel starts a fresh measurement window.
	DBMaintenanceResetQueryStats DBMaintenanceOp = "reset_query_stats"
)

// ErrDBMaintenanceBusy is returned when an operation is already running.
var ErrDBMaintenanceBusy = errors.New("a database maintenance operation is already running")

// ParseDBMaintenanceOp maps a wire/CLI string onto a DBMaintenanceOp.
func ParseDBMaintenanceOp(raw string) (DBMaintenanceOp, error) {
	switch DBMaintenanceOp(raw) {
	case DBMaintenanceVacuumAnalyze, DBMaintenanceAnalyze, DBMaintenanceReindex, DBMaintenanceResetQueryStats:
		return DBMaintenanceOp(raw), nil
	}
	return "", fmt.Errorf("unknown maintenance operation %q (want vacuum_analyze, analyze, reindex, or reset_query_stats)", raw)
}

// DBMaintenanceProgress is a snapshot of the operation in flight.
type DBMaintenanceProgress struct {
	Op        string    `json:"op"`
	StartedAt time.Time `json:"started_at"`
	Table     string    `json:"table,omitempty" doc:"Table currently being processed"`
	Done      int       `json:"done" doc:"Tables completed so far"`
	Total     int       `json:"total" doc:"Total tables in this run; 0 for reset_query_stats"`
}

// DBMaintenanceResult records how the most recent operation went.
type DBMaintenanceResult struct {
	Op         string    `json:"op"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	Tables     int       `json:"tables" doc:"Tables processed; 0 for reset_query_stats"`
	Errors     []string  `json:"errors,omitempty" doc:"Per-table failures; the run continued past them"`
	Error      string    `json:"error,omitempty" doc:"Fatal failure that aborted the run"`
}

// DBMaintenanceStatus is embedded in the /api/admin/db body so the database
// settings page (which already polls it) sees live progress for free.
type DBMaintenanceStatus struct {
	Running *DBMaintenanceProgress `json:"running,omitempty"`
	Last    *DBMaintenanceResult   `json:"last,omitempty"`
}

type dbMaintenanceState struct {
	mu      sync.Mutex
	running *DBMaintenanceProgress
	last    *DBMaintenanceResult
}

// DBMaintenanceStatus reports the in-flight operation (if any) and the last
// completed result. Both are copies — safe to serialize concurrently.
func (a *App) DBMaintenanceStatus() DBMaintenanceStatus {
	a.dbMaint.mu.Lock()
	defer a.dbMaint.mu.Unlock()
	var status DBMaintenanceStatus
	if a.dbMaint.running != nil {
		running := *a.dbMaint.running
		status.Running = &running
	}
	if a.dbMaint.last != nil {
		last := *a.dbMaint.last
		status.Last = &last
	}
	return status
}

// StartDBMaintenance launches op on a background goroutine and returns the
// claimed status immediately. ErrDBMaintenanceBusy when a run is in flight.
func (a *App) StartDBMaintenance(op DBMaintenanceOp) (DBMaintenanceStatus, error) {
	if err := a.claimDBMaintenance(op); err != nil {
		return a.DBMaintenanceStatus(), err
	}
	started := a.startBackground(func() {
		// Detached from the triggering request on purpose: a whole-database
		// VACUUM outlives any HTTP deadline. Bounded so a wedged statement
		// can't pin the claim forever.
		ctx, cancel := context.WithTimeout(a.lifetimeCtx, 2*time.Hour)
		defer cancel()
		a.runDBMaintenance(ctx, op, nil)
	})
	if !started {
		a.releaseDBMaintenance(DBMaintenanceResult{
			Op: string(op), StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
			Error: "app is shutting down",
		})
		return a.DBMaintenanceStatus(), errors.New("app is shutting down")
	}
	return a.DBMaintenanceStatus(), nil
}

// RunDBMaintenance runs op synchronously — the CLI path. onProgress (optional)
// fires before each table.
func (a *App) RunDBMaintenance(ctx context.Context, op DBMaintenanceOp, onProgress func(DBMaintenanceProgress)) (DBMaintenanceResult, error) {
	if err := a.claimDBMaintenance(op); err != nil {
		return DBMaintenanceResult{}, err
	}
	result := a.runDBMaintenance(ctx, op, onProgress)
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}

func (a *App) claimDBMaintenance(op DBMaintenanceOp) error {
	a.dbMaint.mu.Lock()
	defer a.dbMaint.mu.Unlock()
	if a.dbMaint.running != nil {
		return ErrDBMaintenanceBusy
	}
	a.dbMaint.running = &DBMaintenanceProgress{Op: string(op), StartedAt: time.Now().UTC()}
	return nil
}

func (a *App) releaseDBMaintenance(result DBMaintenanceResult) {
	a.dbMaint.mu.Lock()
	a.dbMaint.running = nil
	a.dbMaint.last = &result
	a.dbMaint.mu.Unlock()
}

func (a *App) updateDBMaintenanceProgress(table string, done, total int, onProgress func(DBMaintenanceProgress)) {
	a.dbMaint.mu.Lock()
	var snapshot DBMaintenanceProgress
	if a.dbMaint.running != nil {
		a.dbMaint.running.Table = table
		a.dbMaint.running.Done = done
		a.dbMaint.running.Total = total
		snapshot = *a.dbMaint.running
	}
	a.dbMaint.mu.Unlock()
	if onProgress != nil {
		onProgress(snapshot)
	}
}

// runDBMaintenance owns the claim made by the caller and always releases it.
// The named return matters: the deferred finalizer stamps FinishedAt on
// every exit path, including early returns.
func (a *App) runDBMaintenance(ctx context.Context, op DBMaintenanceOp, onProgress func(DBMaintenanceProgress)) (result DBMaintenanceResult) {
	ctx = diagnostics.WithoutQueryTrace(ctx)
	result = DBMaintenanceResult{Op: string(op), StartedAt: time.Now().UTC()}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		result.DurationMS = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
		a.releaseDBMaintenance(result)
		evt := log.Info()
		if result.Error != "" || len(result.Errors) > 0 {
			evt = log.Warn().Str("error", result.Error).Strs("table_errors", result.Errors)
		}
		evt.Str("op", string(op)).Int("tables", result.Tables).
			Int64("duration_ms", result.DurationMS).Msg("database maintenance finished")
	}()

	pool := a.DBPool()
	if pool == nil {
		result.Error = "no database pool"
		return result
	}

	log.Info().Str("op", string(op)).Msg("database maintenance started")

	if op == DBMaintenanceResetQueryStats {
		var installed bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&installed); err != nil {
			result.Error = err.Error()
		} else if !installed {
			result.Error = "pg_stat_statements extension is not installed"
		} else if _, err := pool.Exec(ctx, `SELECT pg_stat_statements_reset()`); err != nil {
			result.Error = err.Error()
		}
		return result
	}

	verb := map[DBMaintenanceOp]string{
		DBMaintenanceVacuumAnalyze: "VACUUM (ANALYZE)",
		DBMaintenanceAnalyze:       "ANALYZE",
		DBMaintenanceReindex:       "REINDEX TABLE CONCURRENTLY",
	}[op]
	if verb == "" {
		result.Error = fmt.Sprintf("unknown maintenance operation %q", op)
		return result
	}

	// Biggest tables first so the long tail of tiny tables makes the
	// progress counter finish fast, not stall at 1/N on the giant one.
	// REINDEX skips index-less tables (plain REINDEX would error on them).
	listSQL := `SELECT format('%I.%I', schemaname, relname)
		FROM pg_stat_user_tables
		ORDER BY pg_total_relation_size(relid) DESC`
	if op == DBMaintenanceReindex {
		listSQL = `SELECT format('%I.%I', schemaname, relname)
			FROM pg_stat_user_tables
			WHERE EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = relid)
			ORDER BY pg_total_relation_size(relid) DESC`
	}
	rows, err := pool.Query(ctx, listSQL)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			result.Error = err.Error()
			return result
		}
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		result.Error = err.Error()
		return result
	}

	for i, table := range tables {
		a.updateDBMaintenanceProgress(table, i, len(tables), onProgress)
		if _, err := pool.Exec(ctx, verb+" "+table); err != nil {
			if ctx.Err() != nil {
				result.Error = fmt.Sprintf("aborted at %s: %s", table, ctx.Err())
				result.Tables = i
				return result
			}
			result.Errors = append(result.Errors, table+": "+err.Error())
		}
	}
	result.Tables = len(tables)
	a.updateDBMaintenanceProgress("", len(tables), len(tables), onProgress)
	return result
}
