package subsonic

import "encoding/xml"

// DTOs for both serializations. Rules that keep XML and JSON honest from
// one struct set:
//
//   - scalar fields are XML attributes (`xml:"name,attr"`), lists and text
//     bodies are child elements — matching subsonic.org's XSD;
//   - only top-level payload structs carry an XMLName field (envelope
//     payload naming); nested entities are named by their field tags, so
//     the same Child struct can appear as <song>, <entry>, or <child>;
//   - optionals are pointer + omitempty (goccy ignores omitzero — see the
//     Jellyfin layer's json.go for the scar tissue);
//   - payload structs that serve several endpoints (song lists, starred)
//     get their XMLName set at construction; encoding/xml prefers the
//     field value over the tag.

// --- entities ---

// Child is Subsonic's universal media row — songs everywhere, and album /
// artist rows in the legacy folder-style endpoints (isDir=true).
type Child struct {
	ID            string      `xml:"id,attr" json:"id"`
	Parent        string      `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	IsDir         bool        `xml:"isDir,attr" json:"isDir"`
	Title         string      `xml:"title,attr" json:"title"`
	Album         string      `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist        string      `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Track         *int32      `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year          *int32      `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre         string      `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	CoverArt      string      `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Size          int64       `xml:"size,attr,omitempty" json:"size,omitempty"`
	ContentType   string      `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	Suffix        string      `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	Duration      int32       `xml:"duration,attr" json:"duration"`
	BitRate       int32       `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	Path          string      `xml:"path,attr,omitempty" json:"path,omitempty"`
	IsVideo       bool        `xml:"isVideo,attr" json:"isVideo"`
	PlayCount     *int64      `xml:"playCount,attr,omitempty" json:"playCount,omitempty"`
	DiscNumber    *int32      `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Created       *subTime    `xml:"created,attr,omitempty" json:"created,omitempty"`
	Starred       *subTime    `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	AlbumID       string      `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
	ArtistID      string      `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	Type          string      `xml:"type,attr,omitempty" json:"type,omitempty"`
	UserRating    *int32      `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	Genres        []ItemGenre `xml:"genres,omitempty" json:"genres,omitempty"`
	MusicBrainzID string      `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
}

// ItemGenre is an OpenSubsonic multi-genre entry.
type ItemGenre struct {
	Name string `xml:"name,attr" json:"name"`
}

// ArtistID3 is the tag-organized artist entity.
type ArtistID3 struct {
	ID             string   `xml:"id,attr" json:"id"`
	Name           string   `xml:"name,attr" json:"name"`
	CoverArt       string   `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	ArtistImageURL string   `xml:"artistImageUrl,attr,omitempty" json:"artistImageUrl,omitempty"`
	AlbumCount     int64    `xml:"albumCount,attr" json:"albumCount"`
	Starred        *subTime `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	UserRating     *int32   `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	MusicBrainzID  string   `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
	SortName       string   `xml:"sortName,attr,omitempty" json:"sortName,omitempty"`
}

// AlbumID3 is the tag-organized album entity.
type AlbumID3 struct {
	ID            string      `xml:"id,attr" json:"id"`
	Name          string      `xml:"name,attr" json:"name"`
	Artist        string      `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	ArtistID      string      `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	CoverArt      string      `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount     int32       `xml:"songCount,attr" json:"songCount"`
	Duration      int32       `xml:"duration,attr" json:"duration"`
	PlayCount     *int64      `xml:"playCount,attr,omitempty" json:"playCount,omitempty"`
	Created       *subTime    `xml:"created,attr,omitempty" json:"created,omitempty"`
	Starred       *subTime    `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Year          *int32      `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre         string      `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	UserRating    *int32      `xml:"userRating,attr,omitempty" json:"userRating,omitempty"`
	Genres        []ItemGenre `xml:"genres,omitempty" json:"genres,omitempty"`
	MusicBrainzID string      `xml:"musicBrainzId,attr,omitempty" json:"musicBrainzId,omitempty"`
	SortName      string      `xml:"sortName,attr,omitempty" json:"sortName,omitempty"`
}

// --- payloads ---

// MusicFolders — getMusicFolders.
type MusicFolders struct {
	XMLName xml.Name      `xml:"musicFolders" json:"-"`
	Folders []MusicFolder `xml:"musicFolder" json:"musicFolder"`
}

type MusicFolder struct {
	ID   int64  `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr,omitempty" json:"name,omitempty"`
}

// ArtistsID3 — getArtists ("artists") and getIndexes ("indexes"). Both
// serve the same ID3 data — Heya has no folder browse tree, so folder-style
// endpoints answer ID3-shaped, the way Navidrome does. The XMLName field is
// deliberately UNTAGGED: encoding/xml prefers a tag over the field value,
// so a tagged name could never be swapped per endpoint at construction.
type ArtistsID3 struct {
	XMLName         xml.Name   `json:"-"`
	IgnoredArticles string     `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	LastModified    *int64     `xml:"lastModified,attr,omitempty" json:"lastModified,omitempty"`
	Index           []IndexID3 `xml:"index" json:"index"`
}

type IndexID3 struct {
	Name    string      `xml:"name,attr" json:"name"`
	Artists []ArtistID3 `xml:"artist" json:"artist"`
}

// ArtistWithAlbumsID3 — getArtist.
type ArtistWithAlbumsID3 struct {
	XMLName xml.Name `xml:"artist" json:"-"`
	ArtistID3
	Albums []AlbumID3 `xml:"album" json:"album"`
}

// AlbumWithSongsID3 — getAlbum.
type AlbumWithSongsID3 struct {
	XMLName xml.Name `xml:"album" json:"-"`
	AlbumID3
	Songs []Child `xml:"song" json:"song"`
}

// SongPayload — getSong ("song").
type SongPayload struct {
	XMLName xml.Name `xml:"song" json:"-"`
	Child
}

// Directory — getMusicDirectory. Children are albums (isDir) or songs.
type Directory struct {
	XMLName  xml.Name `xml:"directory" json:"-"`
	ID       string   `xml:"id,attr" json:"id"`
	Parent   string   `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Name     string   `xml:"name,attr" json:"name"`
	Starred  *subTime `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Children []Child  `xml:"child" json:"child"`
}

// Genres — getGenres. Genre's name is XML text content, JSON "value".
type Genres struct {
	XMLName xml.Name `xml:"genres" json:"-"`
	Genre   []Genre  `xml:"genre" json:"genre"`
}

type Genre struct {
	SongCount  int64  `xml:"songCount,attr" json:"songCount"`
	AlbumCount int64  `xml:"albumCount,attr" json:"albumCount"`
	Value      string `xml:",chardata" json:"value"`
}

// AlbumList2 — getAlbumList2 ("albumList2"); getAlbumList ("albumList")
// reuses it with Child-shaped entries via AlbumList.
type AlbumList2 struct {
	XMLName xml.Name   `xml:"albumList2" json:"-"`
	Albums  []AlbumID3 `xml:"album" json:"album"`
}

// AlbumList — legacy getAlbumList: album rows in Child (directory) shape.
type AlbumList struct {
	XMLName xml.Name `xml:"albumList" json:"-"`
	Albums  []Child  `xml:"album" json:"album"`
}

// SongList serves every "flat list of songs" payload: randomSongs,
// songsByGenre, similarSongs, similarSongs2, topSongs. XMLName set at
// construction.
type SongList struct {
	XMLName xml.Name `json:"-"`
	Songs   []Child  `xml:"song" json:"song"`
}

// Starred2 — getStarred2 ("starred2") and getStarred ("starred").
type Starred2 struct {
	XMLName xml.Name    `json:"-"`
	Artists []ArtistID3 `xml:"artist" json:"artist"`
	Albums  []AlbumID3  `xml:"album" json:"album"`
	Songs   []Child     `xml:"song" json:"song"`
}

// SearchResult3 — search3 ("searchResult3") and search2 ("searchResult2").
type SearchResult3 struct {
	XMLName xml.Name    `json:"-"`
	Artists []ArtistID3 `xml:"artist" json:"artist,omitempty"`
	Albums  []AlbumID3  `xml:"album" json:"album,omitempty"`
	Songs   []Child     `xml:"song" json:"song,omitempty"`
}

// Playlists / Playlist / PlaylistWithSongs — playlist surface.
type Playlists struct {
	XMLName   xml.Name   `xml:"playlists" json:"-"`
	Playlists []Playlist `xml:"playlist" json:"playlist"`
}

type Playlist struct {
	ID        string   `xml:"id,attr" json:"id"`
	Name      string   `xml:"name,attr" json:"name"`
	Comment   string   `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Owner     string   `xml:"owner,attr,omitempty" json:"owner,omitempty"`
	Public    bool     `xml:"public,attr" json:"public"`
	SongCount int32    `xml:"songCount,attr" json:"songCount"`
	Duration  int32    `xml:"duration,attr" json:"duration"`
	Created   *subTime `xml:"created,attr,omitempty" json:"created,omitempty"`
	Changed   *subTime `xml:"changed,attr,omitempty" json:"changed,omitempty"`
	CoverArt  string   `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
}

type PlaylistWithSongs struct {
	XMLName xml.Name `xml:"playlist" json:"-"`
	Playlist
	Entries []Child `xml:"entry" json:"entry"`
}

// License — getLicense. A self-hosted server is always licensed.
type License struct {
	XMLName xml.Name `xml:"license" json:"-"`
	Valid   bool     `xml:"valid,attr" json:"valid"`
}

// User — getUser; Users — getUsers.
type User struct {
	XMLName             xml.Name `xml:"user" json:"-"`
	Username            string   `xml:"username,attr" json:"username"`
	Email               string   `xml:"email,attr,omitempty" json:"email,omitempty"`
	ScrobblingEnabled   bool     `xml:"scrobblingEnabled,attr" json:"scrobblingEnabled"`
	AdminRole           bool     `xml:"adminRole,attr" json:"adminRole"`
	SettingsRole        bool     `xml:"settingsRole,attr" json:"settingsRole"`
	DownloadRole        bool     `xml:"downloadRole,attr" json:"downloadRole"`
	UploadRole          bool     `xml:"uploadRole,attr" json:"uploadRole"`
	PlaylistRole        bool     `xml:"playlistRole,attr" json:"playlistRole"`
	CoverArtRole        bool     `xml:"coverArtRole,attr" json:"coverArtRole"`
	CommentRole         bool     `xml:"commentRole,attr" json:"commentRole"`
	PodcastRole         bool     `xml:"podcastRole,attr" json:"podcastRole"`
	StreamRole          bool     `xml:"streamRole,attr" json:"streamRole"`
	JukeboxRole         bool     `xml:"jukeboxRole,attr" json:"jukeboxRole"`
	ShareRole           bool     `xml:"shareRole,attr" json:"shareRole"`
	VideoConversionRole bool     `xml:"videoConversionRole,attr" json:"videoConversionRole"`
	Folders             []int64  `xml:"folder,omitempty" json:"folder,omitempty"`
}

type Users struct {
	XMLName xml.Name `xml:"users" json:"-"`
	Users   []User   `xml:"user" json:"user"`
}

// ArtistInfo2 — getArtistInfo2 ("artistInfo2") / getArtistInfo
// ("artistInfo"). Sub-fields are ELEMENTS here, not attributes (per XSD).
type ArtistInfo2 struct {
	XMLName        xml.Name    `json:"-"`
	Biography      string      `xml:"biography,omitempty" json:"biography,omitempty"`
	MusicBrainzID  string      `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	LastFmURL      string      `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
	SmallImageURL  string      `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	MediumImageURL string      `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	LargeImageURL  string      `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
	SimilarArtists []ArtistID3 `xml:"similarArtist" json:"similarArtist,omitempty"`
}

// AlbumInfo — getAlbumInfo / getAlbumInfo2 (same shape, both keys).
type AlbumInfo struct {
	XMLName        xml.Name `json:"-"`
	Notes          string   `xml:"notes,omitempty" json:"notes,omitempty"`
	MusicBrainzID  string   `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	LastFmURL      string   `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
	SmallImageURL  string   `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	MediumImageURL string   `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	LargeImageURL  string   `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
}

// NowPlaying — getNowPlaying.
type NowPlaying struct {
	XMLName xml.Name          `xml:"nowPlaying" json:"-"`
	Entries []NowPlayingEntry `xml:"entry" json:"entry"`
}

type NowPlayingEntry struct {
	Child
	Username   string `xml:"username,attr" json:"username"`
	MinutesAgo int32  `xml:"minutesAgo,attr" json:"minutesAgo"`
	PlayerID   int32  `xml:"playerId,attr" json:"playerId"`
	PlayerName string `xml:"playerName,attr,omitempty" json:"playerName,omitempty"`
}

// PlayQueue — getPlayQueue.
type PlayQueue struct {
	XMLName   xml.Name `xml:"playQueue" json:"-"`
	Current   string   `xml:"current,attr,omitempty" json:"current,omitempty"`
	Position  int64    `xml:"position,attr,omitempty" json:"position,omitempty"`
	Username  string   `xml:"username,attr" json:"username"`
	Changed   *subTime `xml:"changed,attr,omitempty" json:"changed,omitempty"`
	ChangedBy string   `xml:"changedBy,attr,omitempty" json:"changedBy,omitempty"`
	Entries   []Child  `xml:"entry" json:"entry"`
}

// Lyrics — legacy getLyrics: plain text body.
type Lyrics struct {
	XMLName xml.Name `xml:"lyrics" json:"-"`
	Artist  string   `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Title   string   `xml:"title,attr,omitempty" json:"title,omitempty"`
	Value   string   `xml:",chardata" json:"value"`
}

// LyricsList — OpenSubsonic getLyricsBySongId.
type LyricsList struct {
	XMLName          xml.Name           `xml:"lyricsList" json:"-"`
	StructuredLyrics []StructuredLyrics `xml:"structuredLyrics" json:"structuredLyrics"`
}

type StructuredLyrics struct {
	DisplayArtist string      `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	DisplayTitle  string      `xml:"displayTitle,attr,omitempty" json:"displayTitle,omitempty"`
	Lang          string      `xml:"lang,attr" json:"lang"`
	Synced        bool        `xml:"synced,attr" json:"synced"`
	Offset        int64       `xml:"offset,attr" json:"offset"`
	Lines         []LyricLine `xml:"line" json:"line"`
}

type LyricLine struct {
	Start *int64 `xml:"start,attr,omitempty" json:"start,omitempty"`
	Value string `xml:",chardata" json:"value"`
}

// OpenSubsonicExtension — getOpenSubsonicExtensions. The payload is a bare
// ARRAY in JSON and repeated elements in XML.
type OpenSubsonicExtension struct {
	XMLName  xml.Name `xml:"openSubsonicExtensions" json:"-"`
	Name     string   `xml:"name,attr" json:"name"`
	Versions []int    `xml:"versions" json:"versions"`
}

// ScanStatus — getScanStatus / startScan.
type ScanStatus struct {
	XMLName  xml.Name `xml:"scanStatus" json:"-"`
	Scanning bool     `xml:"scanning,attr" json:"scanning"`
	Count    int64    `xml:"count,attr" json:"count"`
}

// TokenInfo — OpenSubsonic tokenInfo (apiKeyAuthentication companion).
type TokenInfo struct {
	XMLName  xml.Name `xml:"tokenInfo" json:"-"`
	Username string   `xml:"username,attr" json:"username"`
}

// Empty-collection stubs: a probing client must conclude "none", never
// "broken".
type Videos struct {
	XMLName xml.Name `xml:"videos" json:"-"`
	Videos  []Child  `xml:"video" json:"video"`
}

type Shares struct {
	XMLName xml.Name   `xml:"shares" json:"-"`
	Shares  []struct{} `xml:"share" json:"share"`
}

type Podcasts struct {
	XMLName  xml.Name   `xml:"podcasts" json:"-"`
	Channels []struct{} `xml:"channel" json:"channel"`
}

type NewestPodcasts struct {
	XMLName  xml.Name   `xml:"newestPodcasts" json:"-"`
	Episodes []struct{} `xml:"episode" json:"episode"`
}

type InternetRadioStations struct {
	XMLName  xml.Name   `xml:"internetRadioStations" json:"-"`
	Stations []struct{} `xml:"internetRadioStation" json:"internetRadioStation"`
}

type ChatMessages struct {
	XMLName  xml.Name   `xml:"chatMessages" json:"-"`
	Messages []struct{} `xml:"chatMessage" json:"chatMessage"`
}

type Bookmarks struct {
	XMLName   xml.Name   `xml:"bookmarks" json:"-"`
	Bookmarks []struct{} `xml:"bookmark" json:"bookmark"`
}
