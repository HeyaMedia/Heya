package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
)

// registerLibraryRoutes mounts /api/libraries/* — CRUD + per-library
// settings + scan triggers + the unmatched-files workflow.
//
// Libraries declared via HEYA_LIBRARY_<N>_* env vars have their three
// identity fields (name/paths/media_type) locked: PUT and DELETE on the
// row return 409 with a tooltip-ready error. Per-library settings stay
// editable regardless.
func registerLibraryRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/libraries", "list-libraries", "List all libraries", "Libraries")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]libraryView], error) {
			libs, err := app.ListLibraries(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to list libraries")
			}
			env := app.EnvManagedLibraries()
			views := make([]libraryView, len(libs))
			for i, lib := range libs {
				views[i] = toLibraryView(lib, env[lib.ID])
			}
			return cachedJSON(views, 10), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries", "create-library", "Create a library", "Libraries")),
		func(ctx context.Context, in *createLibraryInput) (*JSONOutput[libraryView], error) {
			if in.Body.Name == "" || in.Body.MediaType == "" || len(in.Body.Paths) == 0 {
				return nil, huma.Error400BadRequest("name, media_type, and paths are required")
			}
			mt, err := service.ParseMediaType(in.Body.MediaType)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			user := userFrom(ctx)
			lib, err := app.CreateLibrary(ctx, in.Body.Name, mt, in.Body.Paths, user.ID, in.Body.Settings)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			settings := metadata.ParseSettings(lib.Settings)
			if settings.Watch {
				for _, p := range lib.Paths {
					if !strings.HasPrefix(p, "smb://") {
						app.WatcherManager().Watch(ctx, lib.ID, p)
					}
				}
			}
			app.EnqueueScanLibrary(lib.ID, false)
			return &JSONOutput[libraryView]{Body: toLibraryView(lib, app.EnvManagedLibraries()[lib.ID])}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/libraries/{id}", "get-library", "Get a library", "Libraries")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[libraryView], error) {
			lib, err := app.GetLibrary(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("library not found")
			}
			return cachedJSON(toLibraryView(lib, app.EnvManagedLibraries()[lib.ID]), 10), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/libraries/{id}", "update-library", "Update a library", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Name  string   `json:"name" minLength:"1" maxLength:"128"`
				Paths []string `json:"paths" minItems:"1"`
			}
		}) (*JSONOutput[libraryView], error) {
			if env, ok := app.EnvManagedLibraries()[in.ID]; ok {
				return nil, huma.Error409Conflict("library is locked by environment variables " + env.NameEnv + " / " + env.PathsEnv + " / " + env.TypeEnv)
			}
			lib, err := app.UpdateLibrary(ctx, in.ID, in.Body.Name, in.Body.Paths)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &JSONOutput[libraryView]{Body: toLibraryView(lib, service.EnvManagedLibrary{})}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/libraries/{id}", "delete-library", "Delete a library", "Libraries")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if env, ok := app.EnvManagedLibraries()[in.ID]; ok {
				return nil, huma.Error409Conflict("library is locked by " + env.NameEnv + " — remove the env var to delete")
			}
			app.WatcherManager().Unwatch(in.ID)
			if err := app.DeleteLibrary(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError("failed to delete library")
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/settings", "get-library-settings", "Get library settings", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Type string `query:"type" enum:",movie,tv,music,book,comic,podcast,radio" doc:"Media type for default settings"`
		}) (*JSONOutput[librarySettingsBody], error) {
			settings, err := app.GetLibrarySettings(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("library not found")
			}
			return cachedJSON(librarySettingsBody{
				Settings: settings,
				Defaults: metadata.DefaultSettings(in.Type),
			}, 10), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/libraries/{id}/settings", "update-library-settings", "Update library settings", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Body metadata.LibrarySettings
		}) (*JSONOutput[libraryView], error) {
			oldSettings, _ := app.GetLibrarySettings(ctx, in.ID)
			lib, err := app.UpdateLibrarySettings(ctx, in.ID, in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			switch {
			case in.Body.Watch && !oldSettings.Watch:
				for _, p := range lib.Paths {
					if !strings.HasPrefix(p, "smb://") {
						app.WatcherManager().Watch(ctx, lib.ID, p)
					}
				}
			case !in.Body.Watch && oldSettings.Watch:
				app.WatcherManager().Unwatch(lib.ID)
			}
			return &JSONOutput[libraryView]{Body: toLibraryView(lib, app.EnvManagedLibraries()[lib.ID])}, nil
		})

	// --- Scan triggers + refresh ---
	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan", "scan-library", "Enqueue a library scan", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Force bool `query:"force" doc:"Force re-match of already-matched files"`
		}) (*StatusOutput, error) {
			app.EnqueueScanLibrary(in.ID, in.Force)
			return statusOK("queued"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan/cancel", "cancel-library-scan", "Cancel queued scan jobs for this library", "Libraries")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[cancelBody], error) {
			n, err := app.CancelLibraryJobs(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[cancelBody]{Body: cancelBody{Status: "cancelled", Cancelled: n}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/scan/cancel-all", "cancel-all-scans", "Cancel all queued scan jobs", "Libraries")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[cancelBody], error) {
			n, err := app.CancelAllPendingJobs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[cancelBody]{Body: cancelBody{Status: "cancelled", Cancelled: n}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/refresh-metadata", "refresh-library-metadata", "Force a metadata refresh", "Libraries")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.EnqueueForceRefreshMetadata(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("queued"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/refresh-images", "refresh-library-images", "Force an image refresh", "Libraries")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.EnqueueForceRefreshImages(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("queued"), nil
		})

	// --- File browser (matched + unmatched) ---
	// Per-file scan state changes constantly during an active library scan;
	// no-store keeps the file browser from showing stale row counts.
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/files", "list-library-files", "Paginated library files", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Pagination
		}) (*JSONOutput[[]sqlc.LibraryFile], error) {
			files, err := app.ListLibraryFiles(ctx, in.ID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(files), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/files/stats", "library-file-stats", "File status counts", "Libraries")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]sqlc.CountLibraryFilesByStatusRow], error) {
			stats, err := app.LibraryFileStats(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(stats), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scan-v2", "library-scan-v2-view", "Scanner V2 latest run, findings, and identities", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Candidates bool `query:"candidates" doc:"Include all metadata match candidates for the library"`
		}) (*JSONOutput[service.ScannerV2View], error) {
			view, err := app.GetLibraryScannerV2View(ctx, in.ID, in.Candidates)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scan-v2/runs", "library-scan-v2-runs", "Scanner V2 run history", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Pagination
		}) (*JSONOutput[[]service.ScannerV2RunView], error) {
			runs, err := app.ListLibraryScannerV2Runs(ctx, in.ID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(runs), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan-v2/identities/{identity_id}/approve-candidate", "library-scan-v2-approve-candidate", "Approve a scanner V2 match candidate", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				CandidateID int64 `json:"candidate_id" minimum:"1"`
			}
		}) (*JSONOutput[service.ScannerV2IdentityView], error) {
			identity, err := app.ApproveScannerV2Candidate(ctx, in.ID, in.IdentityID, in.Body.CandidateID)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity or candidate not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan-v2/identities/{identity_id}/reject", "library-scan-v2-reject-identity", "Reject a scanner V2 identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				Reason string `json:"reason,omitempty" maxLength:"256"`
			}
		}) (*JSONOutput[service.ScannerV2IdentityView], error) {
			identity, err := app.RejectScannerV2Identity(ctx, in.ID, in.IdentityID, in.Body.Reason)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan-v2/identities/{identity_id}/ignore", "library-scan-v2-ignore-identity", "Ignore a scanner V2 identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				Reason string `json:"reason,omitempty" maxLength:"256"`
			}
		}) (*JSONOutput[service.ScannerV2IdentityView], error) {
			identity, err := app.IgnoreScannerV2Identity(ctx, in.ID, in.IdentityID, in.Body.Reason)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scan-v2/identities/{identity_id}/rematch", "library-scan-v2-rematch-identity", "Reset a scanner V2 identity for rematch", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
		}) (*JSONOutput[service.ScannerV2IdentityView], error) {
			identity, err := app.ResetScannerV2IdentityReview(ctx, in.ID, in.IdentityID)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/unmatched", "list-unmatched", "Unmatched files with match candidates", "Libraries")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]service.UnmatchedFile], error) {
			result, err := app.ListUnmatched(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/library-files/{id}/resolve", "resolve-match", "Accept a match candidate for an unmatched file", "Matching")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				CandidateID int64 `json:"candidate_id" doc:"Match candidate ID"`
			}
		}) (*StatusOutput, error) {
			if err := app.ResolveMatch(ctx, in.ID, in.Body.CandidateID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("matched"), nil
		})
}

type libraryView struct {
	ID        int64                    `json:"id"`
	Name      string                   `json:"name"`
	MediaType string                   `json:"media_type"`
	Paths     []string                 `json:"paths"`
	CreatedBy int64                    `json:"created_by"`
	Settings  metadata.LibrarySettings `json:"settings"`
	// Sources marks any of the three identity fields as env-locked. UI
	// disables the matching input when source=="env" and surfaces the
	// env var name in the tooltip. Per-library `settings` are never
	// env-locked — that field is always editable.
	Sources libraryViewSources `json:"sources"`
}

// libraryViewSources mirrors the per-field provenance shape used by
// /api/config/sources. Source is "env" when the field was bootstrapped
// from a HEYA_LIBRARY_<N>_* env var; otherwise omitted (meaning DB).
type libraryViewSources struct {
	Name      *fieldSource `json:"name,omitempty"`
	Paths     *fieldSource `json:"paths,omitempty"`
	MediaType *fieldSource `json:"media_type,omitempty"`
}

type fieldSource struct {
	Source string `json:"source"`
	EnvVar string `json:"env_var,omitempty"`
}

func toLibraryView(lib sqlc.Library, env service.EnvManagedLibrary) libraryView {
	v := libraryView{
		ID:        lib.ID,
		Name:      lib.Name,
		MediaType: string(lib.MediaType),
		Paths:     lib.Paths,
		CreatedBy: lib.CreatedBy,
		Settings:  metadata.ParseSettings(lib.Settings),
	}
	if env.LibraryID != 0 {
		v.Sources.Name = &fieldSource{Source: "env", EnvVar: env.NameEnv}
		v.Sources.Paths = &fieldSource{Source: "env", EnvVar: env.PathsEnv}
		v.Sources.MediaType = &fieldSource{Source: "env", EnvVar: env.TypeEnv}
	}
	return v
}

type createLibraryInput struct {
	Body struct {
		Name      string                    `json:"name" minLength:"1" maxLength:"128" example:"Movies"`
		MediaType string                    `json:"media_type" enum:"movie,tv,anime,music,book,comic,podcast,radio" example:"movie"`
		Paths     []string                  `json:"paths" minItems:"1" doc:"Absolute filesystem paths or smb://… URIs"`
		Settings  *metadata.LibrarySettings `json:"settings,omitempty"`
	}
}

type librarySettingsBody struct {
	Settings metadata.LibrarySettings `json:"settings"`
	Defaults metadata.LibrarySettings `json:"defaults"`
}

type cancelBody struct {
	Status    string `json:"status"`
	Cancelled int64  `json:"cancelled"`
}
