package subsonic

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/publichttp"
	"github.com/karbowiak/heya/internal/vfs"
)

// Byte delivery. Subsonic clients construct stream/getCoverArt URLs
// themselves, so these need real handlers; cover art dispatches in-process
// to the native image pipeline (the Jellyfin layer's trick), and stream /
// download range-serve the track's best file directly.

// resolveTrackFile maps a song id onto its best playable file.
func (s *Server) resolveTrackFile(r *http.Request) (string, string, bool) {
	trackID, err := DecodeIDKind(param(r, "id"), KindTrack)
	if err != nil {
		return "", "", false
	}
	files, err := s.app.SubsonicTrackBestFiles(r.Context(), []int64{trackID})
	if err != nil {
		return "", "", false
	}
	f, ok := files[trackID]
	if !ok || f.Path == "" {
		return "", "", false
	}
	return f.Path, contentTypeForSuffix(suffixOf(f)), true
}

// stream — direct bytes. maxBitRate / format / timeOffset are accepted and
// deliberately ignored (raw stream): every current client copes with the
// original file when the server doesn't transcode, and Heya's audio
// transcoder integration is a follow-up, not a blocker.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	path, contentType, ok := s.resolveTrackFile(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.serveMediaFile(w, r, path, contentType, "")
}

// download — original bytes with an attachment disposition.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	path, contentType, ok := s.resolveTrackFile(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	base := path[strings.LastIndex(path, "/")+1:]
	s.serveMediaFile(w, r, path, contentType, base)
}

// serveMediaFile range-serves a library file (same shape as the Jellyfin
// layer's server). Host/container-mounted shares are ordinary local paths.
func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request, path, contentType, attachmentName string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	if attachmentName != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(attachmentName, `"`, "")+`"`)
	}
	serveFile(w, r, path)
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	file, err := vfs.OpenFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

// --- Cover art ---

// getCoverArt routes the typed id onto the matching native image endpoint
// and dispatches in-process, so the whole native pipeline (media_assets
// walk, resizer, passive-mode proxy) serves the bytes. Redirects a native
// handler answers with (never-downloaded upstream covers) are resolved
// server-side — Subsonic clients don't reliably follow image redirects.
func (s *Server) handleGetCoverArt(w http.ResponseWriter, r *http.Request) {
	kind, id, err := DecodeID(param(r, "id"))
	if err != nil {
		respondError(w, r, errNotFound, "unknown cover art id")
		return
	}

	size := intParam(r, "size", 0)
	for _, target := range s.coverTargets(r, kind, id) {
		if size > 0 {
			sep := "?"
			if strings.Contains(target, "?") {
				sep = "&"
			}
			target += fmt.Sprintf("%sw=%d&h=%d", sep, size, size)
		}
		if s.serveNativeImage(w, r, target) {
			return
		}
	}
	// Binary endpoints signal errors through the envelope (spec: a failed
	// binary request answers with the regular error response), not a bare
	// HTTP 404 that strict clients surface as a transport failure.
	respondError(w, r, errNotFound, "no cover art")
}

// coverTargets resolves a typed cover id into an ordered list of native
// image endpoints to try. Album-shaped ids fall back to the artist poster
// when the album itself has no art anywhere, so clients get a sensible
// image instead of a placeholder.
func (s *Server) coverTargets(r *http.Request, kind Kind, id int64) []string {
	ctx := r.Context()
	albumCover := func(artistSlug, albumSlug string) string {
		return fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", url.PathEscape(artistSlug), url.PathEscape(albumSlug))
	}
	artistPoster := func(mediaItemID int64) string {
		return fmt.Sprintf("/api/media/%d/image/poster", mediaItemID)
	}
	switch kind {
	case KindArtist:
		ar, err := s.app.SubsonicArtistByID(ctx, id)
		if err != nil {
			return nil
		}
		return []string{artistPoster(ar.MediaItemID)}
	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, jfTracksByIDs(id))
		if err != nil || len(rows) == 0 {
			return nil
		}
		var out []string
		if rows[0].ArtistSlug != "" && rows[0].AlbumSlug != "" {
			out = append(out, albumCover(rows[0].ArtistSlug, rows[0].AlbumSlug))
		}
		if rows[0].ArtistMediaItemID > 0 {
			out = append(out, artistPoster(rows[0].ArtistMediaItemID))
		}
		return out
	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, jfAlbumsByIDs(id))
		if err != nil || len(rows) == 0 {
			return nil
		}
		var out []string
		if rows[0].ArtistSlug != "" && rows[0].Slug != "" {
			out = append(out, albumCover(rows[0].ArtistSlug, rows[0].Slug))
		}
		if rows[0].ArtistMediaItemID > 0 {
			out = append(out, artistPoster(rows[0].ArtistMediaItemID))
		}
		return out
	case KindPlaylist:
		u, _ := userFrom(ctx)
		detail, err := s.app.GetUserPlaylistDetail(ctx, u.ID, id)
		if err != nil || len(detail.Tracks) == 0 {
			return nil
		}
		// First track's album cover — same synthesized cover the native
		// sidebar uses.
		return s.coverTargets(r, KindTrack, detail.Tracks[0].TrackID)
	}
	return nil
}

// serveNativeImage dispatches target through the full server mux
// in-process, resolving redirects to bytes (bounded depth). Mirrors the
// Jellyfin layer; the remote-fetch branch reuses that package's SSRF-guarded
// posture by only following redirects the native pipeline itself emitted.
// Returns true once a successful response has been written; failures are
// swallowed (nothing committed to w) so the caller can try a fallback.
func (s *Server) serveNativeImage(w http.ResponseWriter, r *http.Request, target string) bool {
	if s.native == nil {
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	for range 3 {
		u, err := url.Parse(target)
		if err != nil {
			return false
		}
		if u.IsAbs() {
			return s.proxyRemoteImage(w, r, target)
		}
		r2 := r.Clone(r.Context())
		// Clients may call the Subsonic endpoint via POST (formPost); the
		// native image routes are GET-registered, so normalize the
		// dispatched method and drop the consumed form body.
		if r.Method != http.MethodHead {
			r2.Method = http.MethodGet
		}
		r2.Body = http.NoBody
		r2.ContentLength = 0
		r2.Header.Del("Content-Length")
		r2.Header.Del("Content-Type")
		r2.URL.Path = u.Path
		r2.URL.RawPath = ""
		r2.URL.RawQuery = u.RawQuery
		r2.RequestURI = ""
		dw := &imageDispatchWriter{ResponseWriter: w}
		s.native.ServeHTTP(dw, r2)
		if dw.failed {
			// The native handler may have stamped headers before its
			// error status; scrub them so a fallback (or the error
			// envelope) starts clean.
			h := w.Header()
			h.Del("Content-Type")
			h.Del("Content-Length")
			h.Del("Cache-Control")
			return false
		}
		if !dw.intercepted {
			return true
		}
		if dw.redirect == "" {
			return false
		}
		target = dw.redirect
	}
	return false
}

type imageDispatchWriter struct {
	http.ResponseWriter
	redirect    string
	intercepted bool
	failed      bool
}

func (dw *imageDispatchWriter) WriteHeader(code int) {
	switch {
	case code >= 300 && code < 400:
		dw.redirect = dw.Header().Get("Location")
		dw.Header().Del("Location")
		dw.intercepted = true
	case code >= 400:
		dw.intercepted = true
		dw.failed = true
	default:
		dw.ResponseWriter.WriteHeader(code)
	}
}

func (dw *imageDispatchWriter) Write(b []byte) (int, error) {
	if dw.intercepted {
		return len(b), nil
	}
	return dw.ResponseWriter.Write(b)
}

// proxyRemoteImage fetches a native-pipeline-emitted remote URL (heya.media
// CDN covers that were never downloaded) and streams the bytes through.
// Returns true once bytes were committed; all failures leave w untouched.
func (s *Server) proxyRemoteImage(w http.ResponseWriter, r *http.Request, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	image, err := publichttp.FetchImage(r.Context(), rawURL)
	if err != nil {
		return false
	}
	publichttp.ServeImage(w, r, image, "public, max-age=86400")
	return true
}

// getAvatar — Heya has no user avatars; the spec answer for "no image" is
// a data-not-found error.
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	respondError(w, r, errNotFound, "no avatar")
}

// --- Lyrics ---

var lrcTimestamp = regexp.MustCompile(`^\[(\d+):(\d{2})(?:[.:](\d{1,3}))?\](.*)$`)

// lyricsFor loads and parses local or canonical recording lyrics into lines.
// synced=true when LRC timestamps were found (start in milliseconds).
func (s *Server) lyricsFor(r *http.Request, trackID int64) ([]LyricLine, bool, bool) {
	data, err := s.app.TrackLyrics(r.Context(), trackID)
	if err != nil {
		return nil, false, false
	}

	var lines []LyricLine
	synced := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if m := lrcTimestamp.FindStringSubmatch(line); m != nil {
			mins, _ := strconv.Atoi(m[1])
			secs, _ := strconv.Atoi(m[2])
			frac := 0
			if m[3] != "" {
				padded := m[3] + strings.Repeat("0", 3-len(m[3]))
				frac, _ = strconv.Atoi(padded)
			}
			text := strings.TrimSpace(m[4])
			if text == "" {
				continue
			}
			start := (int64(mins)*60+int64(secs))*1000 + int64(frac)
			synced = true
			lines = append(lines, LyricLine{Start: &start, Value: text})
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue // LRC metadata tags ([ar:], [ti:], ...)
		}
		lines = append(lines, LyricLine{Value: line})
	}
	return lines, synced, len(lines) > 0
}

// getLyricsBySongId — OpenSubsonic structured lyrics.
func (s *Server) handleGetLyricsBySongID(w http.ResponseWriter, r *http.Request) {
	trackID, err := DecodeIDKind(param(r, "id"), KindTrack)
	if err != nil {
		respondError(w, r, errNotFound, "song not found")
		return
	}
	out := LyricsList{StructuredLyrics: []StructuredLyrics{}}
	if lines, synced, ok := s.lyricsFor(r, trackID); ok {
		u, _ := userFrom(r.Context())
		var artist, title string
		if children := s.tracksByIDs(r.Context(), u.ID, []int64{trackID}); len(children) > 0 {
			artist, title = children[0].Artist, children[0].Title
		}
		out.StructuredLyrics = append(out.StructuredLyrics, StructuredLyrics{
			DisplayArtist: artist,
			DisplayTitle:  title,
			Lang:          "und",
			Synced:        synced,
			Lines:         lines,
		})
	}
	respond(w, r, "lyricsList", &out)
}

// getLyrics — legacy artist+title lookup, answered as plain text. Resolves
// the song by title search filtered on artist name.
func (s *Server) handleGetLyrics(w http.ResponseWriter, r *http.Request) {
	artist, title := param(r, "artist"), param(r, "title")
	out := Lyrics{Artist: artist, Title: title}
	if title != "" {
		rows, _, err := s.app.JFListTracks(r.Context(), jfTracksBySearch(title, 50))
		if err == nil {
			for _, tr := range rows {
				if artist != "" && !containsFold(tr.ArtistName, artist) {
					continue
				}
				if lines, _, ok := s.lyricsFor(r, tr.ID); ok {
					var b strings.Builder
					for i, l := range lines {
						if i > 0 {
							b.WriteByte('\n')
						}
						b.WriteString(l.Value)
					}
					out.Value = b.String()
					break
				}
			}
		}
	}
	respond(w, r, "lyrics", &out)
}
