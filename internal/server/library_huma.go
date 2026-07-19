package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/secrettext"
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
			app.EnqueueScanLibrary(lib.ID, false)
			return &JSONOutput[libraryView]{Body: toLibraryView(lib, app.EnvManagedLibraries()[lib.ID])}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/libraries/{id}", "get-library", "Get a library", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
		}) (*JSONOutput[libraryView], error) {
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
				Paths []string `json:"paths" minItems:"1" doc:"Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them"`
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
			lib, err := app.UpdateLibrarySettings(ctx, in.ID, in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
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
			for i := range files {
				files[i] = redactLibraryFileForResponse(files[i])
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

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scanner", "library-scanner-view", "scanner latest run, findings, and identities", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Candidates bool `query:"candidates" doc:"Include all metadata match candidates for the library"`
		}) (*JSONOutput[service.ScannerView], error) {
			view, err := app.GetLibraryScannerView(ctx, in.ID, in.Candidates)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scanner/runs", "library-scanner-runs", "scanner run history", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Pagination
		}) (*JSONOutput[[]service.ScannerRunView], error) {
			runs, err := app.ListLibraryScannerRuns(ctx, in.ID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(runs), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/identities/{identity_id}/approve-candidate", "library-scanner-approve-candidate", "Approve a scanner match candidate", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				CandidateID int64 `json:"candidate_id" minimum:"1"`
			}
		}) (*JSONOutput[service.ScannerIdentityView], error) {
			identity, err := app.ApproveScannerCandidate(ctx, in.ID, in.IdentityID, in.Body.CandidateID)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity or candidate not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/bulk-approve-single", "library-scanner-bulk-approve-single", "Approve single scanner candidates above a confidence threshold", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				MinConfidence float64 `json:"min_confidence" minimum:"0" maximum:"1"`
			}
		}) (*JSONOutput[service.ScannerBulkApproveResult], error) {
			result, err := app.BulkApproveSingleScannerCandidates(ctx, in.ID, in.Body.MinConfidence)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scanner/identities/{identity_id}/candidates/{candidate_id}/detail", "library-scanner-candidate-detail", "Fetch scanner match candidate detail", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID  int64 `path:"identity_id" minimum:"1"`
			CandidateID int64 `path:"candidate_id" minimum:"1"`
		}) (*JSONOutput[service.ScannerCandidateDetailView], error) {
			detail, err := app.GetScannerCandidateDetail(ctx, in.ID, in.IdentityID, in.CandidateID)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity or candidate not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(detail), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/identities/{identity_id}/reject", "library-scanner-reject-identity", "Reject a scanner identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				Reason string `json:"reason,omitempty" maxLength:"256"`
			}
		}) (*JSONOutput[service.ScannerIdentityView], error) {
			identity, err := app.RejectScannerIdentity(ctx, in.ID, in.IdentityID, in.Body.Reason)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				if errors.Is(err, service.ErrScannerReviewIdentityApplied) {
					return nil, huma.Error409Conflict(err.Error())
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/identities/{identity_id}/ignore", "library-scanner-ignore-identity", "Ignore a scanner identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				Reason string `json:"reason,omitempty" maxLength:"256"`
			}
		}) (*JSONOutput[service.ScannerIdentityView], error) {
			identity, err := app.IgnoreScannerIdentity(ctx, in.ID, in.IdentityID, in.Body.Reason)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				if errors.Is(err, service.ErrScannerReviewIdentityApplied) {
					return nil, huma.Error409Conflict(err.Error())
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/identities/{identity_id}/rematch", "library-scanner-rematch-identity", "Reset a scanner identity for rematch", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
		}) (*JSONOutput[service.ScannerIdentityView], error) {
			identity, err := app.ResetScannerIdentityReview(ctx, in.ID, in.IdentityID)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(identity), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/libraries/{id}/scanner/identities/{identity_id}/search", "library-scanner-identity-search", "Provider search for a scanner identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64  `path:"identity_id" minimum:"1"`
			Q          string `query:"q" maxLength:"200" doc:"Title query or provider URL/shortcode"`
			Year       string `query:"year" maxLength:"4" pattern:"^[0-9]*$" doc:"Year hint (4-digit)"`
		}) (*JSONOutput[service.IdentifySearchResult], error) {
			result, err := app.SearchScannerIdentity(ctx, in.ID, in.IdentityID, in.Q, in.Year)
			if err != nil {
				if errors.Is(err, service.ErrScannerReviewTargetNotFound) {
					return nil, huma.Error404NotFound("scanner identity not found")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/libraries/{id}/scanner/identities/{identity_id}/assign", "library-scanner-assign-identity", "Assign an arbitrary provider match to a scanner identity", "Libraries")),
		func(ctx context.Context, in *struct {
			IDPath
			IdentityID int64 `path:"identity_id" minimum:"1"`
			Body       struct {
				ProviderName string            `json:"provider_name,omitempty" maxLength:"32"`
				ProviderID   string            `json:"provider_id" minLength:"1" maxLength:"256"`
				Title        string            `json:"title,omitempty" maxLength:"512"`
				Year         string            `json:"year,omitempty" maxLength:"4" pattern:"^[0-9]*$"`
				Description  string            `json:"description,omitempty" maxLength:"4000"`
				PosterURL    string            `json:"poster_url,omitempty" maxLength:"1024"`
				HeyaSlug     string            `json:"heya_slug,omitempty" maxLength:"256"`
				Confidence   float64           `json:"confidence,omitempty" minimum:"0" maximum:"1"`
				ExternalIDs  map[string]string `json:"external_ids,omitempty"`
			}
		}) (*JSONOutput[service.ScannerIdentityView], error) {
			identity, err := app.AssignScannerIdentityProvider(ctx, in.ID, in.IdentityID, service.AssignScannerIdentityReq{
				ProviderName: in.Body.ProviderName,
				ProviderID:   in.Body.ProviderID,
				Title:        in.Body.Title,
				Year:         in.Body.Year,
				Description:  in.Body.Description,
				PosterURL:    in.Body.PosterURL,
				HeyaSlug:     in.Body.HeyaSlug,
				Confidence:   in.Body.Confidence,
				ExternalIDs:  in.Body.ExternalIDs,
			})
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
			for i := range result {
				result[i].File = redactLibraryFileForResponse(result[i].File)
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
	return buildLibraryView(lib, env, secrettext.RedactStrings(lib.Paths))
}

func buildLibraryView(lib sqlc.Library, env service.EnvManagedLibrary, paths []string) libraryView {
	v := libraryView{
		ID:        lib.ID,
		Name:      lib.Name,
		MediaType: string(lib.MediaType),
		Paths:     paths,
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

func redactLibraryFileForResponse(file sqlc.LibraryFile) sqlc.LibraryFile {
	file.Path = secrettext.Redact(file.Path)
	file.ErrorMessage = secrettext.Redact(file.ErrorMessage)
	return file
}

type createLibraryInput struct {
	Body struct {
		Name      string                    `json:"name" minLength:"1" maxLength:"128" example:"Movies"`
		MediaType string                    `json:"media_type" enum:"movie,tv,anime,music,book,comic,podcast,radio" example:"movie"`
		Paths     []string                  `json:"paths" minItems:"1" doc:"Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them"`
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
