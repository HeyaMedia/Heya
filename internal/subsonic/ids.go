package subsonic

import (
	"errors"
	"strconv"
	"strings"
)

// Subsonic ids are opaque strings to clients, but several endpoints accept
// one `id` parameter that may point at an artist, album, or song
// (getMusicDirectory, getCoverArt, star, setRating, createShare...). So ids
// are typed with a short prefix and route on it — same idea as the Jellyfin
// layer's kind-tagged GUIDs, just human-readable since Subsonic imposes no
// format:
//
//	ar-<artists.id>        artist        (favorites/ratings key on artists.id)
//	al-<albums.id>         album
//	tr-<tracks.id>         song
//	mf-<libraries.id>      music folder  (top-level directory)
//	pl-<user_playlists.id> playlist
//
// Anything that doesn't parse is "not ours" → Subsonic error 70 (data not
// found), which is also what a real server answers for a foreign id.

type Kind byte

const (
	KindInvalid Kind = iota
	KindArtist
	KindAlbum
	KindTrack
	KindFolder
	KindPlaylist
)

var errForeignID = errors.New("subsonic: not a heya id")

var kindPrefix = map[Kind]string{
	KindArtist:   "ar",
	KindAlbum:    "al",
	KindTrack:    "tr",
	KindFolder:   "mf",
	KindPlaylist: "pl",
}

// EncodeID renders (kind, row id) as a typed Subsonic id string.
func EncodeID(kind Kind, id int64) string {
	p, ok := kindPrefix[kind]
	if !ok {
		return ""
	}
	return p + "-" + strconv.FormatInt(id, 10)
}

// DecodeID parses a client-supplied id into (kind, row id).
func DecodeID(s string) (Kind, int64, error) {
	prefix, rest, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok || rest == "" {
		return KindInvalid, 0, errForeignID
	}
	var kind Kind
	switch prefix {
	case "ar":
		kind = KindArtist
	case "al":
		kind = KindAlbum
	case "tr":
		kind = KindTrack
	case "mf":
		kind = KindFolder
	case "pl":
		kind = KindPlaylist
	default:
		return KindInvalid, 0, errForeignID
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 0 {
		return KindInvalid, 0, errForeignID
	}
	return kind, id, nil
}

// DecodeIDKind is DecodeID constrained to one expected kind.
func DecodeIDKind(s string, want Kind) (int64, error) {
	kind, id, err := DecodeID(s)
	if err != nil {
		return 0, err
	}
	if kind != want {
		return 0, errForeignID
	}
	return id, nil
}
