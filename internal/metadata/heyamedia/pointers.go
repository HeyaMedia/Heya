package heyamedia

// pointer-deref helpers for the generated client.
//
// oapi-codegen models every optional field as a pointer, so the mapper has
// to nil-check every read. These helpers collapse the noise: instead of
// `if x := body.Title; x != nil { *x } else { "" }` everywhere, callers
// write `strPtr(body.Title)` and get a guaranteed-non-nil zero value when
// upstream omitted the field.
//
// Naming follows the Go-idiomatic-but-terse house style used elsewhere in
// the codebase (e.g. internal/parser): one-line helpers stay one-line at
// the call site.

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intPtr64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// intPtr64AsInt is for places we want a plain int — JSON numbers come
// back as int64 from the generated client but we store smaller widths
// (TmdbID, Order, Number, runtime, etc.).
func intPtr64AsInt(p *int64) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

func boolPtr(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func floatPtr(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// strs returns the slice value (nil-safe). nil-vs-empty is preserved —
// the golden tests assert the exact slice shape that mapDetail produced,
// and the pre-refactor mapper used nil-slices for "field absent". This
// helper matches that.
func strs(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

// mapStr returns the value of an optional map pointer (nil-safe).
func mapStr(p *map[string]string) map[string]string {
	if p == nil {
		return nil
	}
	return *p
}
