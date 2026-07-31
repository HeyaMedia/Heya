package server

import (
	"context"
	"net/http"
	"time"

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

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/indexers/{id}/stats", "manager-indexer-stats", "Live per-indexer stats from a Prowlarr app", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[[]service.ManagerIndexerStatsView], error) {
			stats, err := app.ManagerIndexerStats(ctx, in.ID)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadGateway)
			}
			return noStoreJSON(stats), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/indexers/{id}/history", "manager-indexer-history", "Daily query/grab activity bucketed from Prowlarr's history", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerIndexerHistoryView], error) {
			history, err := app.ManagerIndexerHistory(ctx, in.ID)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadGateway)
			}
			return noStoreJSON(*history), nil
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

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/download-clients/{id}/activity", "manager-download-client-activity", "Live queue, history, and transfer totals", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerClientActivityView], error) {
			activity, err := app.ManagerDownloadClientActivity(ctx, in.ID)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadGateway)
			}
			return noStoreJSON(*activity), nil
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

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/quality-ladders", "manager-quality-ladders", "Canonical per-domain quality ladder templates for new profiles", "Manager")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[map[string][]service.ManagerQualityItem], error) {
			return &JSONOutput[map[string][]service.ManagerQualityItem]{Body: app.ManagerQualityLadders()}, nil
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

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/quality-profiles/{id}/clone", "manager-clone-quality-profile", "Duplicate a manager quality profile", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerQualityProfileView], error) {
			view, err := app.CloneManagerQualityProfile(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
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

	// ── Custom formats ───────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/custom-formats", "manager-list-custom-formats", "List manager custom formats", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ManagerCustomFormatView], error) {
			views, err := app.ListManagerCustomFormats(ctx)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(views), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/custom-formats", "manager-create-custom-format", "Add a manager custom format", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerCustomFormatInput
		}) (*JSONOutput[service.ManagerCustomFormatView], error) {
			view, err := app.CreateManagerCustomFormat(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerCustomFormatView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/manager/custom-formats/{id}", "manager-update-custom-format", "Update a manager custom format", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.ManagerCustomFormatInput
		}) (*JSONOutput[service.ManagerCustomFormatView], error) {
			view, err := app.UpdateManagerCustomFormat(ctx, in.ID, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerCustomFormatView]{Body: view}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/manager/custom-formats/{id}", "manager-delete-custom-format", "Delete a manager custom format", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*StatusOutput, error) {
			if err := app.DeleteManagerCustomFormat(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/custom-formats/import", "manager-import-custom-formats", "Import custom formats from an arr instance or pasted JSON", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerFormatImportInput
		}) (*JSONOutput[service.ManagerFormatImportResult], error) {
			result, err := app.ImportManagerCustomFormats(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerFormatImportResult]{Body: result}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/custom-formats/test-release", "manager-test-release", "Parse a release title and score it against formats and profiles", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerReleaseTestInput
		}) (*JSONOutput[service.ManagerReleaseTestView], error) {
			view, err := app.TestManagerRelease(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerReleaseTestView]{Body: view}, nil
		})

	// ── Calendar ─────────────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/calendar", "manager-calendar", "Release calendar: episodes airing, movies and albums releasing, books publishing", "Manager")),
		func(ctx context.Context, in *struct {
			From      string  `query:"from" doc:"YYYY-MM-DD; default one week back"`
			To        string  `query:"to" doc:"YYYY-MM-DD; default 30 days ahead"`
			Libraries []int64 `query:"libraries" doc:"Library ids to include; omit for all"`
			Monitored bool    `query:"monitored" doc:"Monitored items only"`
		}) (*JSONOutput[[]service.CalendarEventView], error) {
			now := time.Now()
			from := now.AddDate(0, 0, -7)
			to := now.AddDate(0, 0, 30)
			var err error
			if in.From != "" {
				if from, err = time.Parse("2006-01-02", in.From); err != nil {
					return nil, huma.Error400BadRequest("from must be YYYY-MM-DD")
				}
			}
			if in.To != "" {
				if to, err = time.Parse("2006-01-02", in.To); err != nil {
					return nil, huma.Error400BadRequest("to must be YYYY-MM-DD")
				}
			}
			events, err := app.CalendarEvents(ctx, service.CalendarParams{
				From:       from,
				To:         to,
				LibraryIDs: in.Libraries,
				Monitored:  in.Monitored,
			})
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(events), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/calendar/events", "manager-calendar-event-details", "Expanded details for calendar events (the click-through modal)", "Manager")),
		func(ctx context.Context, in *struct {
			IDs []string `query:"ids" maxItems:"50" doc:"Opaque calendar event ids (ep:N, movie:N, album:dN, book:N), comma-separated"`
		}) (*JSONOutput[[]service.CalendarEventDetailView], error) {
			details, err := app.CalendarEventDetails(ctx, in.IDs)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(details), nil
		})

	// ── Acquisition (dry-run) ────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/media/{id}/search", "manager-media-search", "Shadow-search a movie or TV season/episode across enabled indexers: evaluate, rank, and record what would be grabbed and why (no download)", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			// Huma rejects pointer query params; -1 = unset (season 0 is a
			// real season — specials).
			Season        int   `query:"season" default:"-1" doc:"TV: search this season's wanted episodes (-1 = unset)"`
			EpisodeID     int64 `query:"episode_id" doc:"TV: search one episode by its id (0 = unset)"`
			MusicTargetID int64 `query:"music_target_id" doc:"Music: search one release group by its manager_music_targets id (0 = unset)"`
		}) (*JSONOutput[service.ManagerSearchRunView], error) {
			scope := service.ManagerSearchScope{}
			if in.Season >= 0 {
				season := in.Season
				scope.Season = &season
			}
			if in.EpisodeID > 0 {
				episodeID := in.EpisodeID
				scope.EpisodeID = &episodeID
			}
			if in.MusicTargetID > 0 {
				musicTargetID := in.MusicTargetID
				scope.MusicTargetID = &musicTargetID
			}
			view, err := app.SearchManagerMedia(ctx, in.ID, scope, "api")
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/wanted", "manager-wanted", "Monitored units the pipeline still owes: missing, below cutoff, or misconfigured", "Manager")),
		func(ctx context.Context, in *struct {
			Tab       string  `query:"tab" enum:",missing,cutoff,problems" required:"false"`
			Libraries []int64 `query:"libraries" doc:"Library ids, comma-separated; omit for all"`
			Page      int     `query:"page" minimum:"1" default:"1"`
			PerPage   int     `query:"per_page" minimum:"1" maximum:"200" default:"50"`
		}) (*JSONOutput[service.ManagerWantedPage], error) {
			page, err := app.ManagerWanted(ctx, service.ManagerWantedParams{
				Tab: in.Tab, Libraries: in.Libraries, Page: in.Page, PerPage: in.PerPage,
			})
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(page), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/lookup", "manager-lookup", "Search the metadata provider for new items to add to a library", "Manager")),
		func(ctx context.Context, in *struct {
			LibraryID int64  `query:"library_id" minimum:"1"`
			Q         string `query:"q" minLength:"1" maxLength:"300"`
		}) (*JSONOutput[[]service.ManagerLookupResult], error) {
			results, err := app.ManagerLookup(ctx, in.LibraryID, in.Q)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(results), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/add", "manager-add", "Add a new item to a library from a lookup result: monitored and fileless until the pipeline (or a scan) delivers files", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerAddInput
		}) (*JSONOutput[service.ManagerAddResult], error) {
			result, err := app.ManagerAdd(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusConflict)
			}
			return &JSONOutput[service.ManagerAddResult]{Body: *result}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/queue", "manager-queue", "Merged download-client queue + recent history, each item annotated with Heya's shadow verdict", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.ManagerQueueView], error) {
			view, err := app.ManagerQueue(ctx)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadGateway)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/activity", "manager-activity", "Pipeline runs feed: RSS sweeps, searches, adds — with per-indexer accounting", "Manager")),
		func(ctx context.Context, in *struct {
			Page    int `query:"page" minimum:"1" default:"1"`
			PerPage int `query:"per_page" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[service.ManagerActivityPage], error) {
			page, err := app.ManagerActivity(ctx, in.Page, in.PerPage)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(page), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/rss/run", "manager-rss-run", "Run an RSS sweep now: ingest recent releases, match monitored items, record dry-run decisions", "Manager")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.ManagerRSSRunView], error) {
			view, err := app.RunManagerRSS(ctx, "api")
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusConflict)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/history", "manager-history", "The decision ledger: every evaluation run's verdicts, newest first", "Manager")),
		func(ctx context.Context, in *struct {
			Verdicts []string `query:"verdicts" doc:"Verdict filter, comma-separated"`
			Domains  []string `query:"domains" doc:"Domain filter, comma-separated"`
			Library  int64    `query:"library" doc:"Library id; 0 = all"`
			Before   string   `query:"before" doc:"Keyset cursor: RFC3339 decided_at from next_before"`
			BeforeID int64    `query:"before_id" doc:"Keyset cursor: decision id from next_id"`
			Limit    int      `query:"limit" minimum:"1" maximum:"200" default:"50"`
		}) (*JSONOutput[service.ManagerHistoryPage], error) {
			params := service.ManagerHistoryParams{
				Verdicts: in.Verdicts, Domains: in.Domains,
				LibraryID: in.Library, BeforeID: in.BeforeID, Limit: in.Limit,
			}
			if in.Before != "" {
				t, err := time.Parse(time.RFC3339Nano, in.Before)
				if err != nil {
					return nil, huma.Error400BadRequest("before must be RFC3339")
				}
				params.Before = &t
			}
			page, err := app.ManagerHistory(ctx, params)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(page), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/runs/{id}", "manager-run-detail", "One evaluation run's full accountability record: indexers queried, every candidate, every rejection", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
		}) (*JSONOutput[service.ManagerRunDetailView], error) {
			view, err := app.ManagerRunDetail(ctx, in.ID)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusNotFound)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/media/{id}/decisions", "manager-media-decisions", "Decision history for one media item (entity accountability)", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Page    int `query:"page" minimum:"1" default:"1"`
			PerPage int `query:"per_page" minimum:"1" maximum:"100" default:"25"`
		}) (*JSONOutput[managerItemDecisionsPage], error) {
			decisions, total, err := app.ManagerItemDecisions(ctx, in.ID, in.Page, in.PerPage)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(managerItemDecisionsPage{Decisions: decisions, Total: total}), nil
		})

	// ── Library lens ─────────────────────────────────────────────────────

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/library/{id}/items", "manager-library-items", "Managed view over a library: completeness, monitoring, profiles", "Manager")),
		func(ctx context.Context, in *struct {
			IDPath
			Search    string `query:"search" maxLength:"200" doc:"Title substring filter"`
			Monitored string `query:"monitored" enum:",monitored,unmonitored" required:"false"`
			FileState string `query:"file_state" enum:",missing,complete" required:"false"`
			Status    string `query:"status" maxLength:"40" doc:"Metadata status filter (e.g. returning_series)"`
			Profile   string `query:"profile" maxLength:"20" doc:"Quality profile id, or 'none' for unassigned"`
			Sort      string `query:"sort" enum:",title,year,added,size,missing,units,progress,status" required:"false"`
			Dir       string `query:"dir" enum:",asc,desc" required:"false"`
			Page      int    `query:"page" minimum:"1" default:"1"`
			PerPage   int    `query:"per_page" minimum:"1" maximum:"10000" default:"60"`
		}) (*JSONOutput[service.ManagerLibraryItemsPage], error) {
			page, err := app.ManagerLibraryItems(ctx, in.ID, service.ManagerLibraryItemsParams{
				Search:    in.Search,
				Monitored: in.Monitored,
				FileState: in.FileState,
				Status:    in.Status,
				Profile:   in.Profile,
				Sort:      in.Sort,
				Dir:       in.Dir,
				Page:      in.Page,
				PerPage:   in.PerPage,
			})
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(*page), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/media/{id}", "manager-media-detail", "Arr-style item detail: hero facts plus seasons, files, or albums", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerMediaDetailView], error) {
			view, err := app.GetManagerMediaDetail(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/album/{ref}", "manager-album-detail", "Album page: tracklist with per-track file and quality state", "Manager")),
		func(ctx context.Context, in *struct {
			Ref string `path:"ref" maxLength:"24" doc:"Local album id, or d<id> for a catalog-only discography entry"`
		}) (*JSONOutput[service.ManagerAlbumDetailView], error) {
			view, err := app.GetManagerAlbumDetail(ctx, in.Ref)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/queue/{client_id}/{nzo_id}/files", "manager-queue-files", "List the files a finished download produced on disk", "Manager")),
		func(ctx context.Context, in *struct {
			ClientID int64  `path:"client_id"`
			NzoID    string `path:"nzo_id" maxLength:"128"`
		}) (*JSONOutput[service.ManagerQueueFilesView], error) {
			view, err := app.ManagerQueueFiles(ctx, in.ClientID, in.NzoID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/manager/queue/{client_id}/{nzo_id}/import", "manager-queue-import", "Import a completed download: move its media files into the matched library item's folder and queue a scan", "Manager")),
		func(ctx context.Context, in *struct {
			ClientID int64  `path:"client_id"`
			NzoID    string `path:"nzo_id" maxLength:"128"`
		}) (*JSONOutput[service.ManagerImportView], error) {
			view, err := app.ManagerImport(ctx, in.ClientID, in.NzoID)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/manager/queue/{client_id}/{nzo_id}", "manager-queue-delete", "Remove an entry from a download client's queue or history", "Manager")),
		func(ctx context.Context, in *struct {
			ClientID int64  `path:"client_id"`
			NzoID    string `path:"nzo_id" maxLength:"128"`
			History  bool   `query:"history" doc:"true removes a history record; false cancels an active download (and deletes its partial files)"`
		}) (*struct{}, error) {
			if err := app.ManagerQueueDelete(ctx, in.ClientID, in.NzoID, in.History); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/media/{id}/metadata", "manager-media-metadata", "Full metadata dump: every known field with per-field provenance", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerMetadataView], error) {
			view, err := app.ManagerMetadata(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/manager/file/{id}", "manager-file-detail", "Full probe detail for one library file: absolute path and every stream", "Manager")),
		func(ctx context.Context, in *struct{ IDPath }) (*JSONOutput[service.ManagerFileDetailView], error) {
			view, err := app.ManagerFile(ctx, in.ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(*view), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/manager/media", "manager-update-media", "Bulk-set monitored state and quality profile on media items", "Manager")),
		func(ctx context.Context, in *struct {
			Body service.ManagerMediaBulkInput
		}) (*JSONOutput[service.ManagerMediaBulkResult], error) {
			result, err := app.UpdateManagerMedia(ctx, in.Body)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return &JSONOutput[service.ManagerMediaBulkResult]{Body: result}, nil
		})
}

// managerItemDecisionsPage wraps the entity accountability slice with its
// total for pagination.
type managerItemDecisionsPage struct {
	Decisions []service.ManagerDecisionView `json:"decisions"`
	Total     int64                         `json:"total"`
}
