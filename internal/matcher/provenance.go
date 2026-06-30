package matcher

import "encoding/json"

// Field-provenance source values for media_items.field_provenance. A field with
// no entry is "unknown" and treated as overwritable by enrichment (same as
// local). Only ProvUser is protected from enrich writes.
const (
	ProvLocal  = "local"
	ProvRemote = "remote"
	ProvUser   = "user"
)

// FieldProvenance is the per-field source map stored in
// media_items.field_provenance (jsonb). It records who last set each base field
// so the enrich writers (Phase 2) can fill only empty/local fields and never
// clobber a manual user edit.
type FieldProvenance map[string]string

// ParseFieldProvenance decodes a media_items.field_provenance blob. A nil,
// empty, or unparseable blob yields an empty (non-nil) map.
func ParseFieldProvenance(raw []byte) FieldProvenance {
	fp := FieldProvenance{}
	if len(raw) == 0 {
		return fp
	}
	if err := json.Unmarshal(raw, &fp); err != nil || fp == nil {
		return FieldProvenance{}
	}
	return fp
}

// Marshal encodes the map for storage, never returning nil/null (a nil map
// JSON-encodes to "null"; we normalize to "{}" so the jsonb column stays a
// valid object).
func (fp FieldProvenance) Marshal() []byte {
	if fp == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(fp)
	if err != nil || len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// UserLocked reports whether a field was set by a manual user edit.
func (fp FieldProvenance) UserLocked(field string) bool {
	return fp[field] == ProvUser
}

// CanEnrichOverwrite reports whether an enrich pass may write field — true
// unless the field is user-locked.
func (fp FieldProvenance) CanEnrichOverwrite(field string) bool {
	return fp[field] != ProvUser
}

// Set records the source of a field and returns the map for chaining.
func (fp FieldProvenance) Set(field, source string) FieldProvenance {
	fp[field] = source
	return fp
}
