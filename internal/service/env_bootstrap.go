package service

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// EnvLibrary holds the three identity fields that may be declared via
// HEYA_LIBRARY_<N>_* env vars. Per-library tunables (trickplay, NFO,
// scan schedule, etc.) stay DB-editable in the Settings UI — env only
// touches name/paths/type.
type EnvLibrary struct {
	Index     int
	Name      string
	Paths     []string
	MediaType sqlc.MediaType
}

// EnvManagedLibrary tracks which fields of a live library row were
// set by env. The handler layer reads this when deciding whether to
// reject UI edits (409) and what to send as `sources` on GET responses.
type EnvManagedLibrary struct {
	LibraryID int64
	NameEnv   string // env var that locked name, e.g. HEYA_LIBRARY_0_NAME
	PathsEnv  string
	TypeEnv   string
}

// EnvManagedLibraries returns the in-memory map of env-managed library IDs
// → their per-field env var names. Lookups by library ID. The map is set
// once at boot in BootstrapLibrariesFromEnv and never mutated after.
func (a *App) EnvManagedLibraries() map[int64]EnvManagedLibrary {
	return a.envLibraries
}

var libEnvRegex = regexp.MustCompile(`^HEYA_LIBRARY_(\d+)_NAME$`)

// scanEnvLibraries walks os.Environ() finding HEYA_LIBRARY_<N>_NAME and
// returns one EnvLibrary per matched index, with sibling _PATHS and _TYPE
// fields resolved. Indices may be sparse — the result is sorted by index.
func scanEnvLibraries() ([]EnvLibrary, error) {
	indices := map[int]bool{}
	for _, kv := range os.Environ() {
		k := kv
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			k = kv[:eq]
		}
		if m := libEnvRegex.FindStringSubmatch(k); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil {
				indices[n] = true
			}
		}
	}

	sorted := make([]int, 0, len(indices))
	for n := range indices {
		sorted = append(sorted, n)
	}
	sort.Ints(sorted)

	out := make([]EnvLibrary, 0, len(sorted))
	for _, n := range sorted {
		name := strings.TrimSpace(os.Getenv(fmt.Sprintf("HEYA_LIBRARY_%d_NAME", n)))
		pathsRaw := strings.TrimSpace(os.Getenv(fmt.Sprintf("HEYA_LIBRARY_%d_PATHS", n)))
		typeRaw := strings.TrimSpace(os.Getenv(fmt.Sprintf("HEYA_LIBRARY_%d_TYPE", n)))

		if name == "" || pathsRaw == "" || typeRaw == "" {
			return nil, fmt.Errorf("HEYA_LIBRARY_%d_* is incomplete (NAME=%q PATHS=%q TYPE=%q — all three are required)", n, name, pathsRaw, typeRaw)
		}

		paths := []string{}
		for _, p := range strings.Split(pathsRaw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}

		mt, err := ParseMediaType(typeRaw)
		if err != nil {
			return nil, fmt.Errorf("HEYA_LIBRARY_%d_TYPE: %w", n, err)
		}

		out = append(out, EnvLibrary{
			Index:     n,
			Name:      name,
			Paths:     paths,
			MediaType: mt,
		})
	}
	return out, nil
}

// BootstrapLibrariesFromEnv upserts every env-declared library and stores
// the resulting library IDs in a.envLibraries for later lock checks.
// Lookups are by name (the natural key for env management) — renaming a
// library via env creates a new row rather than mutating the old one.
//
// Requires at least one user to exist (libraries.created_by FK is NOT NULL).
// Returns nil when no env-declared libraries are configured.
func (a *App) BootstrapLibrariesFromEnv(ctx context.Context) error {
	a.envLibraries = map[int64]EnvManagedLibrary{}

	envLibs, err := scanEnvLibraries()
	if err != nil {
		return err
	}
	if len(envLibs) == 0 {
		return nil
	}

	q := sqlc.New(a.db)
	count, err := q.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count == 0 {
		log.Warn().Int("env_libraries", len(envLibs)).Msg("env-declared libraries skipped: no users exist (libraries.created_by is NOT NULL — create an admin first)")
		return nil
	}

	users, err := q.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	creatorID := users[0].ID
	for _, u := range users {
		if u.IsAdmin {
			creatorID = u.ID
			break
		}
	}

	for _, env := range envLibs {
		existing, err := q.GetLibraryByName(ctx, env.Name)
		if err != nil {
			lib, cerr := a.CreateLibrary(ctx, env.Name, env.MediaType, env.Paths, creatorID, nil)
			if cerr != nil {
				return fmt.Errorf("create env library %q: %w", env.Name, cerr)
			}
			log.Info().Int64("library_id", lib.ID).Str("name", env.Name).Strs("paths", env.Paths).Msg("env library created from HEYA_LIBRARY_* vars")
			a.recordEnvLibrary(lib.ID, env)
			continue
		}
		needsUpdate := !stringSlicesEqual(existing.Paths, env.Paths) || existing.MediaType != env.MediaType
		if needsUpdate {
			updated, uerr := q.UpdateLibraryIdentity(ctx, sqlc.UpdateLibraryIdentityParams{
				ID:        existing.ID,
				Paths:     env.Paths,
				MediaType: env.MediaType,
			})
			if uerr != nil {
				return fmt.Errorf("update env library %q: %w", env.Name, uerr)
			}
			log.Info().Int64("library_id", updated.ID).Str("name", env.Name).Msg("env library updated to match HEYA_LIBRARY_* vars")
			a.recordEnvLibrary(updated.ID, env)
		} else {
			log.Info().Int64("library_id", existing.ID).Str("name", env.Name).Msg("env library already matches")
			a.recordEnvLibrary(existing.ID, env)
		}
	}
	return nil
}

func (a *App) recordEnvLibrary(id int64, env EnvLibrary) {
	a.envLibraries[id] = EnvManagedLibrary{
		LibraryID: id,
		NameEnv:   fmt.Sprintf("HEYA_LIBRARY_%d_NAME", env.Index),
		PathsEnv:  fmt.Sprintf("HEYA_LIBRARY_%d_PATHS", env.Index),
		TypeEnv:   fmt.Sprintf("HEYA_LIBRARY_%d_TYPE", env.Index),
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BootstrapAdminFromEnv creates the admin user from HEYA_ADMIN_USERNAME +
// HEYA_ADMIN_PASSWORD when no admin currently exists. Strictly first-boot
// — once any admin row exists the env vars are ignored and the admin
// rotates the password normally via the UI.
//
// HEYA_ADMIN_EMAIL is optional and defaults to "<user>@local".
func (a *App) BootstrapAdminFromEnv(ctx context.Context) error {
	username := strings.TrimSpace(os.Getenv("HEYA_ADMIN_USERNAME"))
	password := os.Getenv("HEYA_ADMIN_PASSWORD")
	if username == "" || password == "" {
		return nil
	}

	q := sqlc.New(a.db)
	users, err := q.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		if u.IsAdmin {
			log.Info().Str("admin", u.Username).Msg("env admin bootstrap skipped (admin exists)")
			return nil
		}
	}

	email := strings.TrimSpace(os.Getenv("HEYA_ADMIN_EMAIL"))
	if email == "" {
		email = username + "@local"
	}

	if _, err := a.CreateUser(ctx, username, email, password, true); err != nil {
		return fmt.Errorf("create env admin %q: %w", username, err)
	}
	log.Info().Str("admin", username).Msg("env admin bootstrap created admin user")
	return nil
}
