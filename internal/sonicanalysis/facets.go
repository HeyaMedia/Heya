package sonicanalysis

// GenreScore is one (name, score) pair from the Discogs-400
// classifier. JSON-marshaled into track_facets.top_genres.
type GenreScore struct {
	Name  string  `json:"name"`
	Score float32 `json:"score"`
}

// Facets is the complete output of one Analyzer.Analyze call —
// every per-track value the integration produces.
//
// Loudness is intentionally NOT here: the canonical EBU R128 pipeline
// lives in track_files / albums and runs as soon as a music file is
// probed (long before sonic analysis starts). Read-side APIs join
// track_files for the LUFS chip instead of duplicating the work.
//
// Vectors are NOT L2-normalized for the Discogs heads (their
// training already produces well-behaved cosine geometry). TextEmbed
// (CLAP audio side) IS L2-normalized so cosine reduces to a dot
// product against L2-normalized CLAP text vectors.
type Facets struct {
	TrackEmbed   []float32 // 512 — discogs_track_embeddings
	ArtistEmbed  []float32 // 512 — discogs_artist_embeddings
	ReleaseEmbed []float32 // 512 — discogs_release_embeddings
	TextEmbed    []float32 // 512 — CLAP audio (L2-normalized)

	BPM           float64
	BPMConfidence float64

	Key        Key
	KeyClarity float64

	TopGenres []GenreScore
	MoodTags  MoodScores

	Waveform   []float32 // 2000 peaks [0..1]
	Boundaries *Boundaries

	ElapsedMs int // wall-clock for one Analyze call
}
