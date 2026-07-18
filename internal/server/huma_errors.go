package server

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/service"
)

// redactHumaErrorResponse is the final API error boundary. Huma constructs an
// ErrorModel for handler, middleware, and validation failures before response
// transformers run, so sanitizing a cloned model here covers both modern
// humaServiceError call sites and legacy handlers that still pass err.Error().
func redactHumaErrorResponse(_ huma.Context, _ string, value any) (any, error) {
	model, ok := value.(*huma.ErrorModel)
	if !ok || model == nil {
		return value, nil
	}

	redacted := *model
	redacted.Type = secrettext.Redact(model.Type)
	redacted.Title = secrettext.Redact(model.Title)
	redacted.Detail = secrettext.Redact(model.Detail)
	redacted.Instance = secrettext.Redact(model.Instance)
	if model.Errors != nil {
		redacted.Errors = make([]*huma.ErrorDetail, len(model.Errors))
		for i, detail := range model.Errors {
			if detail == nil {
				continue
			}
			copy := *detail
			copy.Message = secrettext.Redact(detail.Message)
			copy.Location = secrettext.Redact(detail.Location)
			copy.Value = redactHumaErrorValue(detail.Value)
			redacted.Errors[i] = &copy
		}
	}
	return &redacted, nil
}

func redactHumaErrorValue(value any) any {
	switch value := value.(type) {
	case string:
		return secrettext.Redact(value)
	case []string:
		return secrettext.RedactStrings(value)
	case []any:
		redacted := make([]any, len(value))
		for i := range value {
			redacted[i] = redactHumaErrorValue(value[i])
		}
		return redacted
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for key, child := range value {
			redacted[secrettext.Redact(key)] = redactHumaErrorValue(child)
		}
		return redacted
	default:
		return value
	}
}

func humaServiceError(err error) error {
	return humaServiceErrorStatus(err, http.StatusInternalServerError)
}

func humaServiceErrorStatus(err error, fallbackStatus int) error {
	if err == nil {
		return nil
	}
	var locked *service.ErrFieldLockedByEnv
	message := secrettext.Redact(err.Error())
	switch {
	case errors.As(err, &locked):
		return huma.Error409Conflict(secrettext.Redact(locked.Error()))
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("not found")
	case errors.Is(err, service.ErrRegistrationClosed):
		return huma.Error403Forbidden(message)
	case errors.Is(err, service.ErrCastAccessDenied):
		return huma.Error403Forbidden(message)
	case errors.Is(err, service.ErrInvalidCastAllowance):
		return huma.Error400BadRequest(message)
	case errors.Is(err, images.ErrImageTooLarge):
		return huma.NewError(http.StatusRequestEntityTooLarge, message)
	case errors.Is(err, images.ErrInvalidImage), errors.Is(err, service.ErrInvalidImageUpload):
		return huma.Error400BadRequest(message)
	case errors.Is(err, service.ErrWrongPassword):
		return huma.Error401Unauthorized(message)
	case errors.Is(err, service.ErrJobNotRetryable), errors.Is(err, service.ErrJobNotCancellable):
		return huma.Error404NotFound(message)
	case errors.Is(err, service.ErrSchedulerUnavailable):
		return huma.Error503ServiceUnavailable(message)
	case errors.Is(err, service.ErrNoFacets), errors.Is(err, service.ErrNoRadioSeed):
		return huma.Error404NotFound(message)
	default:
		return huma.NewError(fallbackStatus, message)
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
