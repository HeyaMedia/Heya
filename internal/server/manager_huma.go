package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
)

// Manager (acquisition) config: indexers, download clients, quality profiles.
// Everything is admin-gated — these rows hold API keys and steer what the
// pipeline downloads. Connection tests return 200 with {ok:false, error}
// rather than HTTP errors (the request was well-formed; the remote wasn't).
func registerManagerRoutes(api huma.API, app *service.App) {
	// ── Indexers ─────────────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/indexers", "manager-list-indexers", "List manager indexers", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ManagerIndexerView], error) {
			views, err := app.ListManagerIndexers(ctx)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(views), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/indexers", "manager-create-indexer", "Add a manager indexer", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerIndexerInput
		}) (*JSONOutput[service.ManagerIndexerView], error) {
			view, err := app.CreateManagerIndexer(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerIndexerView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/manager/indexers/{id}", "manager-update-indexer", "Update a manager indexer", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.ManagerIndexerInput
		}) (*JSONOutput[service.ManagerIndexerView], error) {
			view, err := app.UpdateManagerIndexer(ctx, in.ID, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerIndexerView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/manager/indexers/{id}", "manager-delete-indexer", "Delete a manager indexer", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*StatusOutput, error) {
			if err := app.DeleteManagerIndexer(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/indexers/{id}/test", "manager-test-indexer", "Test a manager indexer connection", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerTestResult], error) {
			result, err := app.TestManagerIndexer(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return &JSONOutput[service.ManagerTestResult]{Body: result}, nil
		})

	// ── Download clients ─────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/download-clients", "manager-list-download-clients", "List manager download clients", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ManagerDownloadClientView], error) {
			views, err := app.ListManagerDownloadClients(ctx)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(views), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/download-clients", "manager-create-download-client", "Add a manager download client", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerDownloadClientInput
		}) (*JSONOutput[service.ManagerDownloadClientView], error) {
			view, err := app.CreateManagerDownloadClient(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerDownloadClientView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/manager/download-clients/{id}", "manager-update-download-client", "Update a manager download client", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.ManagerDownloadClientInput
		}) (*JSONOutput[service.ManagerDownloadClientView], error) {
			view, err := app.UpdateManagerDownloadClient(ctx, in.ID, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerDownloadClientView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/manager/download-clients/{id}", "manager-delete-download-client", "Delete a manager download client", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*StatusOutput, error) {
			if err := app.DeleteManagerDownloadClient(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/download-clients/{id}/test", "manager-test-download-client", "Test a manager download client connection", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerTestResult], error) {
			result, err := app.TestManagerDownloadClient(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return &JSONOutput[service.ManagerTestResult]{Body: result}, nil
		})

	// ── Quality profiles ─────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/quality-profiles", "manager-list-quality-profiles", "List manager quality profiles", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ManagerQualityProfileView], error) {
			views, err := app.ListManagerQualityProfiles(ctx)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(views), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/quality-profiles", "manager-create-quality-profile", "Add a manager quality profile", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerQualityProfileInput
		}) (*JSONOutput[service.ManagerQualityProfileView], error) {
			view, err := app.CreateManagerQualityProfile(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerQualityProfileView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/manager/quality-profiles/{id}", "manager-update-quality-profile", "Update a manager quality profile", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.ManagerQualityProfileInput
		}) (*JSONOutput[service.ManagerQualityProfileView], error) {
			view, err := app.UpdateManagerQualityProfile(ctx, in.ID, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerQualityProfileView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/manager/quality-profiles/{id}", "manager-delete-quality-profile", "Delete a manager quality profile", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*StatusOutput, error) {
			if err := app.DeleteManagerQualityProfile(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("deleted"), nil
		})
}
