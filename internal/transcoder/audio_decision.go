package transcoder

import "strings"

// AudioCaps describes what a client can natively decode in an <audio> element.
// Mirrors the bool block in web/app/composables/useClientCaps.ts so the
// server can make the same picks the browser would.
type AudioCaps struct {
	FLAC   bool
	ALAC   bool
	MP3    bool
	AAC    bool
	Vorbis bool
	Opus   bool
	WavPCM bool
}

// AudioPlayPlan tells the stream handler what to do for a single track_file.
type AudioPlayPlan int

const (
	// AudioPlayDirect serves the file bytes untouched.
	AudioPlayDirect AudioPlayPlan = iota
	// AudioPlayTranscode produces an AAC-256 fragmented MP4 on the fly.
	AudioPlayTranscode
)

// CanPlayDirect returns true when the client's caps cover the format on disk.
// Format strings match what the matcher writes into track_files.format
// (extension lowercase, no dot).
func CanPlayDirect(format string, caps AudioCaps) bool {
	switch strings.ToLower(format) {
	case "mp3":
		return caps.MP3
	case "flac":
		return caps.FLAC
	case "m4a", "aac":
		return caps.AAC
	case "ogg", "oga":
		return caps.Vorbis
	case "opus":
		return caps.Opus
	case "wav":
		return caps.WavPCM
	case "alac":
		return caps.ALAC
	case "wma":
		return false // browsers never decode WMA
	}
	return false
}
