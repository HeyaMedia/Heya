package server

// ErrorModel is the JSON shape returned for all error responses through Huma.
// Matches the FE expectation of `{ "detail": "..." }` (Huma's default).
type ErrorModel struct {
	Detail string `json:"detail" doc:"Error description"`
}
