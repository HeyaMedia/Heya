package matcher

import "testing"

func TestFieldProvenanceParseMarshal(t *testing.T) {
	// nil / empty / garbage all yield an empty, non-nil map.
	for _, raw := range [][]byte{nil, {}, []byte("{}"), []byte("not json")} {
		fp := ParseFieldProvenance(raw)
		if fp == nil {
			t.Fatalf("ParseFieldProvenance(%q) returned nil map", raw)
		}
		if len(fp) != 0 {
			t.Errorf("ParseFieldProvenance(%q) = %v, want empty", raw, fp)
		}
	}

	fp := ParseFieldProvenance([]byte(`{"title":"user","genres":"remote"}`))
	if fp["title"] != ProvUser || fp["genres"] != ProvRemote {
		t.Fatalf("parsed wrong: %v", fp)
	}

	// round-trip
	again := ParseFieldProvenance(fp.Marshal())
	if again["title"] != ProvUser || again["genres"] != ProvRemote {
		t.Errorf("round-trip lost data: %v", again)
	}

	// Marshal never returns nil.
	if string(FieldProvenance(nil).Marshal()) != "{}" {
		t.Errorf("nil map should marshal to {}")
	}
}

func TestFieldProvenanceOverwriteRules(t *testing.T) {
	fp := FieldProvenance{}.Set("title", ProvUser).Set("year", ProvRemote)

	if !fp.UserLocked("title") {
		t.Error("title should be user-locked")
	}
	if fp.CanEnrichOverwrite("title") {
		t.Error("enrich must NOT overwrite a user-locked field")
	}
	if !fp.CanEnrichOverwrite("year") {
		t.Error("enrich may overwrite a remote field")
	}
	// Unknown field: overwritable.
	if !fp.CanEnrichOverwrite("description") {
		t.Error("enrich may overwrite an unknown field")
	}
	if fp.UserLocked("description") {
		t.Error("unknown field is not user-locked")
	}
}
