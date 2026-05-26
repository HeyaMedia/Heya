package radiobrowser

import (
	"io"
	"regexp"
	"strings"
)

// ICY ("I Can Yell") is the SHOUTcast-style protocol that interleaves audio
// bytes with text metadata blocks. The pattern:
//
//   - Server sends `icy-metaint: N` in the response headers
//   - Audio bytes flow normally for N bytes
//   - Then 1 byte (B) holding the metadata length in 16-byte units
//   - Then B*16 bytes of `StreamTitle='...';` text (zero-padded)
//   - Then back to N audio bytes, and so on
//
// IcyReader wraps the upstream response body, peels the metadata blocks off
// transparently (so the audio stream the browser sees is contiguous), and
// fires a callback whenever a fresh metadata block arrives. The callback
// runs synchronously inside Read() — keep it cheap; we use it to hop the
// (artist, title) into the event hub.

type IcyReader struct {
	src        io.Reader
	metaint    int
	onMetadata func(artist, title string)

	bytesUntilMeta int    // bytes of audio remaining before next metadata block
	inMeta         bool   // currently inside a metadata block?
	metaRemaining  int    // bytes of the current metadata block still to read
	metaSizeRead   bool   // have we read the 1-byte size prefix yet?
	metaBuf        []byte // accumulator for the current metadata block
	lastTitle      string // last emitted StreamTitle (dedupe)
}

// NewIcyReader returns a reader that strips ICY metadata from src. metaint
// is the audio-bytes-per-metadata-block value from the `icy-metaint`
// response header; onMetadata fires on each new (artist, title) pair.
func NewIcyReader(src io.Reader, metaint int, onMetadata func(artist, title string)) *IcyReader {
	return &IcyReader{
		src:            src,
		metaint:        metaint,
		onMetadata:     onMetadata,
		bytesUntilMeta: metaint,
	}
}

// Read is invoked by the HTTP response writer. We read from src into our
// own buffer, then copy audio bytes into p while peeling metadata blocks.
// Audio is copied 1:1; metadata is consumed but not surfaced.
func (r *IcyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Read a raw chunk from upstream first. We may need to skip metadata
	// before we can hand any audio bytes back to the caller.
	buf := make([]byte, len(p))
	n, err := r.src.Read(buf)
	if n == 0 {
		return 0, err
	}

	outIdx := 0
	srcIdx := 0
	for srcIdx < n {
		if r.inMeta {
			// We're consuming a metadata block. Track size+body, never emit
			// bytes to the caller.
			if !r.metaSizeRead {
				r.metaRemaining = int(buf[srcIdx]) * 16
				srcIdx++
				r.metaSizeRead = true
				r.metaBuf = r.metaBuf[:0]
				if r.metaRemaining == 0 {
					// Empty metadata block — reset and resume audio.
					r.inMeta = false
					r.metaSizeRead = false
					r.bytesUntilMeta = r.metaint
				}
				continue
			}
			take := r.metaRemaining
			if take > n-srcIdx {
				take = n - srcIdx
			}
			r.metaBuf = append(r.metaBuf, buf[srcIdx:srcIdx+take]...)
			srcIdx += take
			r.metaRemaining -= take
			if r.metaRemaining == 0 {
				r.parseMeta()
				r.inMeta = false
				r.metaSizeRead = false
				r.bytesUntilMeta = r.metaint
			}
			continue
		}

		// Pass audio bytes through, up to the next metadata block.
		take := r.bytesUntilMeta
		if take > n-srcIdx {
			take = n - srcIdx
		}
		if outIdx+take > len(p) {
			take = len(p) - outIdx
		}
		copy(p[outIdx:outIdx+take], buf[srcIdx:srcIdx+take])
		outIdx += take
		srcIdx += take
		r.bytesUntilMeta -= take
		if r.bytesUntilMeta == 0 {
			r.inMeta = true
		}
		if outIdx == len(p) {
			break
		}
	}

	if outIdx == 0 && err != nil {
		return 0, err
	}
	return outIdx, err
}

// streamTitleRe captures StreamTitle='...' inside the metadata payload.
// The ; on the end is optional because some stations omit it on the final
// pair; the .* is non-greedy via the closing ' anchor.
var streamTitleRe = regexp.MustCompile(`StreamTitle='([^']*)'`)

func (r *IcyReader) parseMeta() {
	// The block is zero-padded out to 16-byte alignment; trim trailing NULs
	// before regex so the captured group doesn't include them.
	text := strings.TrimRight(string(r.metaBuf), "\x00")
	match := streamTitleRe.FindStringSubmatch(text)
	if match == nil {
		return
	}
	title := strings.TrimSpace(match[1])
	if title == "" || title == r.lastTitle {
		return
	}
	r.lastTitle = title

	// ICY convention: "Artist - Title" if a separator is present, otherwise
	// the whole string is the title and there's no artist.
	artist := ""
	display := title
	if idx := strings.Index(title, " - "); idx > 0 {
		artist = strings.TrimSpace(title[:idx])
		display = strings.TrimSpace(title[idx+3:])
	}
	if r.onMetadata != nil {
		r.onMetadata(artist, display)
	}
}
