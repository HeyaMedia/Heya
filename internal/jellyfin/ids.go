package jellyfin

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

// Jellyfin item ids are GUIDs serialized as 32 lowercase hex chars (the SDKs
// send them undashed; some clients round-trip the dashed form). Heya ids are
// int64 identity columns per table, so a Jellyfin id must carry which table
// it points at. The encoding is deterministic and reversible — no mapping
// table, no schema change:
//
//	bytes [0:3]  magic "hya"
//	byte  [3]    codec version (1)
//	byte  [4]    Kind (entity table)
//	bytes [5:8]  reserved (zero)
//	bytes [8:16] uint64 big-endian row id (or FNV-64a hash for name-keyed
//	             entities like genres, which have no numeric id)
//
// Anything that doesn't decode (wrong magic/version/length) is simply "not
// ours" — handlers treat it as a 404, which is also what a real Jellyfin
// does for foreign GUIDs.

type Kind byte

const (
	KindInvalid    Kind = 0x00
	KindLibrary    Kind = 0x01 // libraries row → Jellyfin "view" / CollectionFolder
	KindItem       Kind = 0x02 // media_items row (movie, series, artist, book)
	KindSeason     Kind = 0x03 // tv_seasons row
	KindEpisode    Kind = 0x04 // tv_episodes row
	KindAlbum      Kind = 0x05 // albums row
	KindTrack      Kind = 0x06 // tracks row
	KindPerson     Kind = 0x07 // people row
	KindStudio     Kind = 0x08 // production_companies row
	KindGenre      Kind = 0x09 // FNV-64a(lowercased name)
	KindMusicGenre Kind = 0x0a // FNV-64a(lowercased name)
	KindCollection Kind = 0x0b // collections row
	KindPlaylist   Kind = 0x0c // user playlists row
	KindFile       Kind = 0x0d // library_files row (video MediaSource)
	KindTrackFile  Kind = 0x0e // track_files row (audio MediaSource)
	KindYear       Kind = 0x0f // literal year number
	KindUser       Kind = 0x10 // users row
	KindList       Kind = 0x11 // user_lists row
)

var (
	idMagic      = [3]byte{'h', 'y', 'a'}
	idVersion    = byte(1)
	ErrForeignID = errors.New("jellyfin: not a heya id")
)

// EncodeID renders (kind, id) as a Jellyfin-compatible GUID string.
func EncodeID(kind Kind, id int64) string {
	var buf [16]byte
	copy(buf[0:3], idMagic[:])
	buf[3] = idVersion
	buf[4] = byte(kind)
	binary.BigEndian.PutUint64(buf[8:16], uint64(id))
	return hex.EncodeToString(buf[:])
}

// DecodeID parses a client-supplied id. Dashed GUID forms are accepted
// because a handful of clients reformat ids through platform GUID types.
func DecodeID(s string) (Kind, int64, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	if len(s) != 32 {
		return KindInvalid, 0, ErrForeignID
	}
	var buf [16]byte
	if _, err := hex.Decode(buf[:], []byte(s)); err != nil {
		return KindInvalid, 0, ErrForeignID
	}
	if buf[0] != idMagic[0] || buf[1] != idMagic[1] || buf[2] != idMagic[2] || buf[3] != idVersion {
		return KindInvalid, 0, ErrForeignID
	}
	return Kind(buf[4]), int64(binary.BigEndian.Uint64(buf[8:16])), nil
}

// DecodeIDKind is DecodeID constrained to one expected kind.
func DecodeIDKind(s string, want Kind) (int64, error) {
	kind, id, err := DecodeID(s)
	if err != nil {
		return 0, err
	}
	if kind != want {
		return 0, ErrForeignID
	}
	return id, nil
}

// hashName produces the stable pseudo-id for name-keyed entities (genres).
// FNV-64a over the lowercased name; collisions across a personal library's
// genre list are astronomically unlikely and merely merge two genre pages.
func hashName(name string) int64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range []byte(strings.ToLower(strings.TrimSpace(name))) {
		h ^= uint64(b)
		h *= prime64
	}
	return int64(h)
}
