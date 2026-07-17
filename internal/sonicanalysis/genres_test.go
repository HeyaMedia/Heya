package sonicanalysis

import "testing"

func TestCanonicalGenreName(t *testing.T) {
	tests := []struct {
		in        string
		want      string
		wantFound bool
	}{
		{"Rock---Metalcore", "Rock---Metalcore", true},
		{"rock---metalcore", "Rock---Metalcore", true},
		{"ELECTRONIC---TECHNO", "Electronic---Techno", true},
		// Metadata folksonomy names are not classifier labels.
		{"melodic metalcore", "", false},
		{"Metalcore", "", false}, // leaf alone is not a class name
		{"", "", false},
	}
	for _, tt := range tests {
		got, found := CanonicalGenreName(tt.in)
		if got != tt.want || found != tt.wantFound {
			t.Errorf("CanonicalGenreName(%q) = (%q, %v), want (%q, %v)",
				tt.in, got, found, tt.want, tt.wantFound)
		}
	}
}
