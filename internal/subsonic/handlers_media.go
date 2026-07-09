package subsonic

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/vfs"
)

// Byte delivery. Subsonic clients construct stream/getCoverArt URLs
// themselves, so these need real handlers; cover art dispatches in-process
// to the native image pipeline (the Jellyfin layer's trick), and stream /
// download serve the track's best file directly — range-capable, SMB-aware.

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

// serveMediaFile range-serves a local or SMB-backed file (same shape as
// the Jellyfin layer's server).
func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request, path, contentType, attachmentName string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	if attachmentName != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(attachmentName, `"`, "")+`"`)
	}
	if vfs.IsSMBPath(path) {
		serveVFS(w, r, path)
		return
	}
	f, err := os.Open(path) //nolint:gosec // G304: path comes from library_files rows, not request input
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), f)
}

func serveVFS(w http.ResponseWriter, r *http.Request, smbPath string) {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	source, err := vfs.Open(smbPath[:lastSlash])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() { _ = source.Close() }()
	f, err := source.FS.Open(smbPath[lastSlash+1:])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, smbPath[lastSlash+1:], stat.ModTime(), rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	_, _ = io.Copy(w, f)
}

// --- Cover art ---

// getCoverArt routes the typed id onto the matching native image endpoint
// and dispatches in-process, so the whole native pipeline (media_assets
// walk, resizer, passive-mode proxy) serves the bytes. Redirects a native
// handler answers with (never-downloaded upstream covers) are resolved
// server-side — Subsonic clients don't reliably follow image redirects.
func (s *Server) handleGetCoverArt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind, id, err := DecodeID(param(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	target := ""
	switch kind {
	case KindArtist:
		ar, err := s.app.SubsonicArtistByID(ctx, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/media/%d/image/poster", ar.MediaItemID)
	case KindAlbum, KindTrack, KindPlaylist:
		slugA, slugB, ok := s.coverSlugs(r, kind, id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", url.PathEscape(slugA), url.PathEscape(slugB))
	default:
		http.NotFound(w, r)
		return
	}

	if size := intParam(r, "size", 0); size > 0 {
		target += fmt.Sprintf("?w=%d&h=%d", size, size)
	}
	s.serveNativeImage(w, r, target)
}

// coverSlugs resolves the (artist_slug, album_slug) pair behind an album /
// track / playlist cover id.
func (s *Server) coverSlugs(r *http.Request, kind Kind, id int64) (string, string, bool) {
	ctx := r.Context()
	switch kind {
	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, jfTracksByIDs(id))
		if err != nil || len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].AlbumSlug == "" {
			return "", "", false
		}
		return rows[0].ArtistSlug, rows[0].AlbumSlug, true
	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, jfAlbumsByIDs(id))
		if err != nil || len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].Slug == "" {
			return "", "", false
		}
		return rows[0].ArtistSlug, rows[0].Slug, true
	case KindPlaylist:
		u, _ := userFrom(ctx)
		detail, err := s.app.GetUserPlaylistDetail(ctx, u.ID, id)
		if err != nil || len(detail.Tracks) == 0 {
			return "", "", false
		}
		// First track's album cover — same synthesized cover the native
		// sidebar uses.
		return s.coverSlugs(r, KindTrack, detail.Tracks[0].TrackID)
	}
	return "", "", false
}

// serveNativeImage dispatches target through the full server mux
// in-process, resolving redirects to bytes (bounded depth). Mirrors the
// Jellyfin layer; the remote-fetch branch reuses that package's SSRF-guarded
// posture by only following redirects the native pipeline itself emitted.
func (s *Server) serveNativeImage(w http.ResponseWriter, r *http.Request, target string) {
	if s.native == nil {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	for range 3 {
		u, err := url.Parse(target)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if u.IsAbs() {
			s.proxyRemoteImage(w, r, target)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = u.Path
		r2.URL.RawPath = ""
		r2.URL.RawQuery = u.RawQuery
		r2.RequestURI = ""
		dw := &imageDispatchWriter{ResponseWriter: w}
		s.native.ServeHTTP(dw, r2)
		if !dw.intercepted {
			return
		}
		if dw.redirect == "" {
			http.NotFound(w, r)
			return
		}
		target = dw.redirect
	}
	http.NotFound(w, r)
}

type imageDispatchWriter struct {
	http.ResponseWriter
	redirect    string
	intercepted bool
}

func (dw *imageDispatchWriter) WriteHeader(code int) {
	if code >= 300 && code < 400 {
		dw.redirect = dw.Header().Get("Location")
		dw.Header().Del("Location")
		dw.intercepted = true
		return
	}
	dw.ResponseWriter.WriteHeader(code)
}

func (dw *imageDispatchWriter) Write(b []byte) (int, error) {
	if dw.intercepted {
		return len(b), nil
	}
	return dw.ResponseWriter.Write(b)
}

// proxyRemoteImage fetches a native-pipeline-emitted remote URL (heya.media
// CDN covers that were never downloaded) and streams the bytes through.
func (s *Server) proxyRemoteImage(w http.ResponseWriter, r *http.Request, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer func() { _ = res.Body.Close() }()
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(res.Body, 32<<20))
	}
}

// getAvatar — Heya has no user avatars; the spec answer for "no image" is
// a data-not-found error.
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	respondError(w, r, errNotFound, "no avatar")
}

// --- Lyrics ---

var lrcTimestamp = regexp.MustCompile(`^\[(\d+):(\d{2})(?:[.:](\d{1,3}))?\](.*)$`)

// lyricsFor loads and parses the track's sidecar lyrics file into lines.
// synced=true when LRC timestamps were found (start in milliseconds).
func (s *Server) lyricsFor(r *http.Request, trackID int64) ([]LyricLine, bool, bool) {
	files, err := s.app.ListTrackFiles(r.Context(), trackID)
	if err != nil {
		return nil, false, false
	}
	path := ""
	for _, tf := range files {
		if tf.LyricsPath != "" {
			path = tf.LyricsPath
			break
		}
	}
	if path == "" {
		return nil, false, false
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path comes from track_files rows, not request input
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
