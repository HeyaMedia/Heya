package titlematch

import "testing"

func TestFuzzyEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
		why  string
	}{
		{"Usseewa", "Usseewa", true, "identical"},
		{"Usseewa", "うっせぇわ (Usseewa)", true, "parens-stripped right side"},
		{"うっせぇわ", "うっせぇわ (Usseewa)", true, "japanese left, mixed right"},
		{"Usseewa", "うっせぇわ", true, "romanization"},
		{"Stay Gold (From 'BEYBLADE X')", "Stay Gold (from BEYBLADE X)", true, "parens cosmetics"},
		{"UTA'S SONGS ONE PIECE FILM RED", "UTA'S SONGS ONE PIECE FILM RED (All Video Version)", true, "trailing variant suffix"},
		{"Odo", "Odoru Ponpokorin", false, "prefix substring must NOT match"},
		{"Show", "Show me the World", false, "single word prefix must NOT match"},
		{"", "Anything", false, "empty left"},
		{"Anything", "", false, "empty right"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			got := FuzzyEqual(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("FuzzyEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			gotReversed := FuzzyEqual(tc.b, tc.a)
			if gotReversed != tc.want {
				t.Errorf("FuzzyEqual(%q, %q) [reversed] = %v, want %v", tc.b, tc.a, gotReversed, tc.want)
			}
		})
	}
}

func TestNormalizations(t *testing.T) {
	keys := Normalizations("Stay Gold (From 'BEYBLADE X')")
	hasStripped := false
	for _, k := range keys {
		if k == "stay gold" {
			hasStripped = true
		}
	}
	if !hasStripped {
		t.Errorf("expected stripped 'stay gold' in normalizations, got %v", keys)
	}
}
