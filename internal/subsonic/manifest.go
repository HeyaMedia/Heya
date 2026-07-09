package subsonic

// The coverage manifest: every endpoint in spec.go, triaged. This is how
// "speak the whole Subsonic API" stays honest — manifest_test.go fails the
// build when a spec endpoint is missing here, when an implemented/stubbed
// claim has no registered route, or when a registered route isn't claimed.
//
// Statuses:
//
//	opImplemented — registered, real behavior backed by Heya services.
//	opStubbed     — registered, answers the correct "feature absent"
//	                shape (empty collection, honest refusal, 70).
//	opUnsupported — not registered; the generic handler answers error 0
//	                ("endpoint not implemented"), which is also what a
//	                real Subsonic answers for views it doesn't know.
type opStatus int

const (
	opUnsupported opStatus = iota
	opImplemented
	opStubbed
)

var manifest = map[string]opStatus{
	// System
	"ping":       opImplemented,
	"getLicense": opImplemented,

	// Browsing
	"getMusicFolders":   opImplemented,
	"getIndexes":        opImplemented, // ID3-shaped (no folder tree), like Navidrome
	"getMusicDirectory": opImplemented, // folder ids are the ID3 hierarchy
	"getGenres":         opImplemented,
	"getArtists":        opImplemented,
	"getArtist":         opImplemented,
	"getAlbum":          opImplemented,
	"getSong":           opImplemented,
	"getVideos":         opStubbed, // music-only surface: empty list
	"getVideoInfo":      opUnsupported,
	"getArtistInfo":     opImplemented,
	"getArtistInfo2":    opImplemented,
	"getAlbumInfo":      opImplemented, // cover URLs + empty notes (no album-notes storage)
	"getAlbumInfo2":     opImplemented,
	"getSimilarSongs":   opImplemented, // sonic-embedding KNN
	"getSimilarSongs2":  opImplemented,
	"getTopSongs":       opImplemented, // Last.fm top tracks joined to local files

	// Lists
	"getAlbumList":    opImplemented,
	"getAlbumList2":   opImplemented,
	"getRandomSongs":  opImplemented,
	"getSongsByGenre": opImplemented,
	"getNowPlaying":   opImplemented, // live session store
	"getStarred":      opImplemented,
	"getStarred2":     opImplemented,

	// Searching
	"search":  opImplemented, // deprecated view, answered with search2 semantics
	"search2": opImplemented,
	"search3": opImplemented,

	// Playlists
	"getPlaylists":   opImplemented,
	"getPlaylist":    opImplemented,
	"createPlaylist": opImplemented,
	"updatePlaylist": opImplemented,
	"deletePlaylist": opImplemented,

	// Media retrieval
	"stream":      opImplemented, // raw bytes; maxBitRate/format accepted + ignored
	"download":    opImplemented,
	"hls":         opUnsupported,
	"getCaptions": opUnsupported,
	"getCoverArt": opImplemented, // in-process dispatch to the native image pipeline
	"getLyrics":   opImplemented,
	"getAvatar":   opStubbed, // no avatar storage: honest 70

	// Annotation
	"star":      opImplemented, // maps to Heya loved state
	"unstar":    opImplemented,
	"setRating": opImplemented, // 1..5 stars → Heya 1..10 ratings
	"scrobble":  opImplemented, // play_events + live session mirror

	// Sharing — no public-link feature to bridge to.
	"getShares":   opStubbed,
	"createShare": opUnsupported,
	"updateShare": opUnsupported,
	"deleteShare": opUnsupported,

	// Podcast — Heya's podcasts are per-user, native-API-only for now.
	// Bridging them here is a real follow-up, not a stub-forever.
	"getPodcasts":            opStubbed,
	"getNewestPodcasts":      opStubbed,
	"refreshPodcasts":        opUnsupported,
	"createPodcastChannel":   opUnsupported,
	"deletePodcastChannel":   opUnsupported,
	"deletePodcastEpisode":   opUnsupported,
	"downloadPodcastEpisode": opUnsupported,

	// Jukebox — no server-side audio hardware.
	"jukeboxControl": opUnsupported,

	// Internet radio — Heya's radio favorites are a candidate bridge;
	// unsupported until then.
	"getInternetRadioStations":   opStubbed,
	"createInternetRadioStation": opUnsupported,
	"updateInternetRadioStation": opUnsupported,
	"deleteInternetRadioStation": opUnsupported,

	// Chat
	"getChatMessages": opStubbed,
	"addChatMessage":  opUnsupported,

	// Users — read real, mutate refused (accounts are managed in Heya).
	"getUser":        opImplemented,
	"getUsers":       opImplemented,
	"createUser":     opStubbed,
	"updateUser":     opStubbed,
	"deleteUser":     opStubbed,
	"changePassword": opStubbed,

	// Bookmarks + play queue
	"getBookmarks":   opStubbed,
	"createBookmark": opUnsupported,
	"deleteBookmark": opUnsupported,
	"getPlayQueue":   opImplemented,
	"savePlayQueue":  opImplemented,

	// Scanning
	"getScanStatus": opImplemented,
	"startScan":     opImplemented,

	// OpenSubsonic
	"getOpenSubsonicExtensions": opImplemented,
	"tokenInfo":                 opImplemented,
	"getLyricsBySongId":         opImplemented,
	"getPodcastEpisode":         opUnsupported,
	"getPlayQueueByIndex":       opUnsupported,
	"savePlayQueueByIndex":      opUnsupported,
}
