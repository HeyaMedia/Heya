package mediafile

import "testing"

func TestExtraTypeFromPath(t *testing.T) {
	cases := map[string]string{
		"Movie (2024)/Trailers/Teaser.mkv":                          "trailer",
		"Movie (2024)/Featurettes/Making Of.mkv":                    "featurette",
		"Movie (2024)/Behind The Scenes/Practical Effects.mkv":      "behindthescenes",
		"Movie (2024)/Deleted Scenes/Alt Ending.mkv":                "deleted",
		"Movie (2024)/Theatrical Trailer-trailer.mkv":               "trailer",
		"Movie (2024)/Interview With Cast-interview.mp4":            "interview",
		"Movie (2024)/sample.mkv":                                   "sample",
		"Show With Extras (2020)/samples/sample.mkv":                "sample",
		"Movie (2024)/Movie (2024).mkv":                             "",
		"Show (2024)/Season 01/Show (2024) - S01E01 - Pilot.mkv":    "",
		"Show (2024)/Season 01/Show (2024) - S01E01-trailer.mkv":    "trailer",
		"Show (2024)/Season 01/Show (2024) - S01E01 Featurette.mkv": "",
	}
	for path, want := range cases {
		if got := ExtraTypeFromPath(path); got != want {
			t.Fatalf("ExtraTypeFromPath(%q): got %q, want %q", path, got, want)
		}
	}
}
