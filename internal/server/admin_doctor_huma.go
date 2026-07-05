package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

// registerAdminDoctorRoutes mounts GET /api/admin/doctor — the support-bundle
// endpoint behind the "Download support bundle" button on Settings →
// Diagnostics. It's a thin wrapper around service.App.BuildDoctorReport,
// which does all the actual work and degrades gracefully section-by-section
// (see internal/service/doctor.go). buf is the server's in-process log ring
// buffer so the bundle's Logs section is populated when running inside
// `heya serve`; `heya doctor` on the CLI passes nil instead and that section
// reports how to get logs another way.
//
// Read-only like every admin_*_huma.go route here: never mutates state,
// never triggers a scan or disk walk.
func registerAdminDoctorRoutes(api huma.API, app *service.App, buf *logbuf.RingBuffer) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/doctor", "admin-doctor", "Full diagnostic support bundle (redacted, safe to paste)", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.DoctorReport], error) {
			return noStoreJSON(app.BuildDoctorReport(ctx, buf)), nil
		})
}
