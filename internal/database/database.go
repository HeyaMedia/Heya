package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	MaxConns    int32
	MinConns    int32
	QueryTracer pgx.QueryTracer
}

// ResolveHosts returns every host pgx will actually dial for databaseURL, using
// pgx's own parser. This sees through what a naive net/url parse misses — a
// `?host=` query param, the keyword/DSN form (`host=… port=…`), PGHOST env, and
// multi-host fallbacks — so a security check on the result can't be fooled by a
// connstring whose URL authority says localhost while pgx connects elsewhere.
func ResolveHosts(databaseURL string) ([]string, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	hosts := []string{cfg.ConnConfig.Host}
	for _, fb := range cfg.ConnConfig.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	return hosts, nil
}

// HostIsLocal reports whether a pgx-resolved host is on this machine: a loopback
// name, empty, or a unix-socket path. pgx treats a host as a unix socket ONLY
// when it starts with "/" (the socket directory) — anything else, INCLUDING a
// leading "@", is dialed as TCP, so only "/" and the loopback names count.
func HostIsLocal(host string) bool {
	if host == "" || strings.HasPrefix(host, "/") {
		return true
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// AllHostsLocal reports whether EVERY host pgx would dial for databaseURL is
// local. On false it returns the first non-local host (for error messages); on
// a parse error it returns (false, "", err) so callers fail safe.
func AllHostsLocal(databaseURL string) (bool, string, error) {
	hosts, err := ResolveHosts(databaseURL)
	if err != nil {
		return false, "", err
	}
	for _, h := range hosts {
		if !HostIsLocal(h) {
			return false, h, nil
		}
	}
	return true, strings.Join(hosts, ","), nil
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return ConnectWithOptions(ctx, databaseURL, Options{MaxConns: 15, MinConns: 2})
}

func ConnectWithOptions(ctx context.Context, databaseURL string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = 15
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.ConnConfig.Tracer = opts.QueryTracer

	// pgvector 0.8+: let HNSW scans keep iterating (relaxed order) instead of
	// silently truncating at hnsw.ef_search tuples. Without this, any KNN
	// query whose LIMIT exceeds ef_search (40 by default) — e.g. the radio
	// builder's over-fetch — returns ~46 rows no matter the LIMIT once the
	// planner picks the HNSW index. Best-effort: older pgvector (or a DB
	// without the extension) doesn't know the GUC, and that must not break
	// connecting.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET hnsw.iterative_scan = 'relaxed_order'"); err != nil {
			return nil //nolint:nilerr // unsupported GUC — feature simply stays off
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}

// EnsurePGStatStatements installs the SQL extension after the server has been
// configured with shared_preload_libraries=pg_stat_statements. It is
// deliberately best-effort at the service layer: managed PostgreSQL roles may
// not have CREATE EXTENSION permission, and that must not stop Heya booting.
func EnsurePGStatStatements(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool unavailable")
	}
	const extensionLockID int64 = 0x4845594150475353 // "HEYAPGSS"
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", extensionLockID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
