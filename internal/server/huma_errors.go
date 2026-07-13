package server

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/service"
)

func humaServiceError(err error) error {
	return humaServiceErrorStatus(err, http.StatusInternalServerError)
}

func humaServiceErrorStatus(err error, fallbackStatus int) error {
	if err == nil {
		return nil
	}
	var locked *service.ErrFieldLockedByEnv
	switch {
	case errors.As(err, &locked):
		return huma.Error409Conflict(locked.Error())
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("not found")
	case errors.Is(err, service.ErrRegistrationClosed):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, service.ErrCastAccessDenied):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, service.ErrInvalidCastAllowance):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, service.ErrWrongPassword):
		return huma.Error401Unauthorized(err.Error())
	case errors.Is(err, service.ErrJobNotRetryable), errors.Is(err, service.ErrJobNotCancellable):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, service.ErrSchedulerUnavailable):
		return huma.Error503ServiceUnavailable(err.Error())
	case errors.Is(err, service.ErrNoFacets), errors.Is(err, service.ErrNoRadioSeed):
		return huma.Error404NotFound(err.Error())
	default:
		return huma.NewError(fallbackStatus, err.Error())
	}
}

// facetsErr maps ErrNoFacets to a 404 with per-endpoint copy ("seed track has
// no facets", …); anything else goes through the shared service-error mapper
// with the given fallback status.
func facetsErr(err error, notFoundMsg string, fallbackStatus int) error {
	if errors.Is(err, service.ErrNoFacets) {
		return huma.Error404NotFound(notFoundMsg)
	}
	return humaServiceErrorStatus(err, fallbackStatus)
}
