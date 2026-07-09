package subsonic

// The endpoint universe this surface is triaged against: every view in the
// Subsonic 1.16.1 API reference (subsonic.org/pages/api.jsp) plus the
// OpenSubsonic additions. Subsonic has no machine-readable spec to vendor
// (the Jellyfin layer embeds an OpenAPI document; here the reference table
// IS the spec), so this list is the checked-in equivalent — manifest_test
// fails when an entry here is missing from the manifest or vice versa.

type specEndpoint struct {
	Name     string // canonical camelCase view name
	Category string
}

var specEndpoints = []specEndpoint{
	// System
	{"ping", "System"},
	{"getLicense", "System"},

	// Browsing
	{"getMusicFolders", "Browsing"},
	{"getIndexes", "Browsing"},
	{"getMusicDirectory", "Browsing"},
	{"getGenres", "Browsing"},
	{"getArtists", "Browsing"},
	{"getArtist", "Browsing"},
	{"getAlbum", "Browsing"},
	{"getSong", "Browsing"},
	{"getVideos", "Browsing"},
	{"getVideoInfo", "Browsing"},
	{"getArtistInfo", "Browsing"},
	{"getArtistInfo2", "Browsing"},
	{"getAlbumInfo", "Browsing"},
	{"getAlbumInfo2", "Browsing"},
	{"getSimilarSongs", "Browsing"},
	{"getSimilarSongs2", "Browsing"},
	{"getTopSongs", "Browsing"},

	// Album/song lists
	{"getAlbumList", "Lists"},
	{"getAlbumList2", "Lists"},
	{"getRandomSongs", "Lists"},
	{"getSongsByGenre", "Lists"},
	{"getNowPlaying", "Lists"},
	{"getStarred", "Lists"},
	{"getStarred2", "Lists"},

	// Searching
	{"search", "Searching"},
	{"search2", "Searching"},
	{"search3", "Searching"},

	// Playlists
	{"getPlaylists", "Playlists"},
	{"getPlaylist", "Playlists"},
	{"createPlaylist", "Playlists"},
	{"updatePlaylist", "Playlists"},
	{"deletePlaylist", "Playlists"},

	// Media retrieval
	{"stream", "MediaRetrieval"},
	{"download", "MediaRetrieval"},
	{"hls", "MediaRetrieval"},
	{"getCaptions", "MediaRetrieval"},
	{"getCoverArt", "MediaRetrieval"},
	{"getLyrics", "MediaRetrieval"},
	{"getAvatar", "MediaRetrieval"},

	// Media annotation
	{"star", "Annotation"},
	{"unstar", "Annotation"},
	{"setRating", "Annotation"},
	{"scrobble", "Annotation"},

	// Sharing
	{"getShares", "Sharing"},
	{"createShare", "Sharing"},
	{"updateShare", "Sharing"},
	{"deleteShare", "Sharing"},

	// Podcast
	{"getPodcasts", "Podcast"},
	{"getNewestPodcasts", "Podcast"},
	{"refreshPodcasts", "Podcast"},
	{"createPodcastChannel", "Podcast"},
	{"deletePodcastChannel", "Podcast"},
	{"deletePodcastEpisode", "Podcast"},
	{"downloadPodcastEpisode", "Podcast"},

	// Jukebox
	{"jukeboxControl", "Jukebox"},

	// Internet radio
	{"getInternetRadioStations", "InternetRadio"},
	{"createInternetRadioStation", "InternetRadio"},
	{"updateInternetRadioStation", "InternetRadio"},
	{"deleteInternetRadioStation", "InternetRadio"},

	// Chat
	{"getChatMessages", "Chat"},
	{"addChatMessage", "Chat"},

	// User management
	{"getUser", "Users"},
	{"getUsers", "Users"},
	{"createUser", "Users"},
	{"updateUser", "Users"},
	{"deleteUser", "Users"},
	{"changePassword", "Users"},

	// Bookmarks + play queue
	{"getBookmarks", "Bookmarks"},
	{"createBookmark", "Bookmarks"},
	{"deleteBookmark", "Bookmarks"},
	{"getPlayQueue", "Bookmarks"},
	{"savePlayQueue", "Bookmarks"},

	// Media library scanning
	{"getScanStatus", "Scanning"},
	{"startScan", "Scanning"},

	// OpenSubsonic additions
	{"getOpenSubsonicExtensions", "OpenSubsonic"},
	{"tokenInfo", "OpenSubsonic"},
	{"getLyricsBySongId", "OpenSubsonic"},
	{"getPodcastEpisode", "OpenSubsonic"},
	{"getPlayQueueByIndex", "OpenSubsonic"},
	{"savePlayQueueByIndex", "OpenSubsonic"},
}
