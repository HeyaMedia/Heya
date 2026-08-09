package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFirstLiveEpisodesInBurstSurvivesReplacement(t *testing.T) {
	first := time.Date(2026, time.August, 9, 9, 44, 0, 0, time.UTC)
	replacement := first.Add(2*time.Hour + 50*time.Minute)
	episode := seasonEpisode{season: 3, episode: 2}
	burst := []recentTVFile{
		{createdAt: first, episodes: []seasonEpisode{episode}},
		{createdAt: replacement, live: true, episodes: []seasonEpisode{episode}},
	}

	got := firstLiveEpisodesInBurst(burst, map[seasonEpisode]time.Time{episode: first}, map[seasonEpisode]bool{episode: true})
	assert.Equal(t, map[seasonEpisode]time.Time{episode: first}, got)
}

func TestFirstLiveEpisodesInBurstSkipsRemovedEpisode(t *testing.T) {
	first := time.Date(2026, time.August, 9, 9, 44, 0, 0, time.UTC)
	episode := seasonEpisode{season: 3, episode: 2}
	burst := []recentTVFile{{createdAt: first, episodes: []seasonEpisode{episode}}}

	got := firstLiveEpisodesInBurst(burst, map[seasonEpisode]time.Time{episode: first}, nil)
	assert.Empty(t, got)
}
