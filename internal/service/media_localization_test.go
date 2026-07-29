package service

import "testing"

func TestEpisodeLocalizationQueryLanguages(t *testing.T) {
	tests := []struct {
		name      string
		preferred string
		want      []string
	}{
		{name: "English", preferred: "en-US", want: []string{"en"}},
		{name: "non-English with English fallback", preferred: "da_DK", want: []string{"da", "en"}},
		{name: "empty", preferred: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := episodeLocalizationQueryLanguages(tt.preferred)
			if len(got) != len(tt.want) {
				t.Fatalf("languages = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("languages = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestPickEpisodeLocalization(t *testing.T) {
	values := map[string]string{
		"ja":    "Japanese synopsis",
		"en-GB": "British English synopsis",
		"en-US": "American English synopsis",
		"da_DK": "Danish synopsis",
	}

	tests := []struct {
		name     string
		language string
		country  string
		want     string
	}{
		{name: "country-specific preferred language", language: "en", country: "US", want: "American English synopsis"},
		{name: "underscore tags normalize", language: "da", country: "DK", want: "Danish synopsis"},
		{name: "English regional fallback", language: "de", country: "GB", want: "British English synopsis"},
		{name: "English beats original language", language: "de", country: "DE", want: "British English synopsis"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickEpisodeLocalization(values, tt.language, tt.country); got != tt.want {
				t.Fatalf("localization = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPickEpisodeLocalizationDoesNotInventOriginalLanguageFallback(t *testing.T) {
	values := map[string]string{"ja": "Japanese synopsis"}
	if got := pickEpisodeLocalization(values, "en", "US"); got != "" {
		t.Fatalf("localization = %q, want empty so caller can choose the raw fallback", got)
	}
}
