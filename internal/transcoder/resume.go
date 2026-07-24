package transcoder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// resumeManifestName is the sidecar written into every HLS session output
// directory. It records exactly which encode plan produced the segments
// sitting next to it, so a later process can decide whether those segments
// are still valid for a freshly computed plan.
const resumeManifestName = "session.json"

// resumeManifestVersion invalidates every on-disk manifest when the sidecar
// format or the fingerprint recipe changes. Bump it rather than trying to
// migrate: the cost of a mismatch is one re-encode.
const resumeManifestVersion = 1

// resumeManifest is the sidecar's contents. Fingerprint is the load-bearing
// field; the rest is there so a human (or `heya doctor`) can tell what a
// cache directory holds without decoding the hash.
type resumeManifest struct {
	Version     int     `json:"version"`
	Key         string  `json:"key"`
	FilePath    string  `json:"file_path"`
	SegExt      string  `json:"seg_ext"`
	TotalSegs   int     `json:"total_segs"`
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

// resumeFingerprintInput is everything that has to match for previously
// encoded segments to be safe to hand a player alongside newly encoded ones.
//
// Two classes of field live here. Boundary inputs (SegmentEnds, SegExt,
// TotalSegs) decide what segment index N *means* — get those wrong and the
// player gets the wrong wall-clock content. Bitstream inputs (profile,
// hwaccel, tone mapping, the surgical plan flags) decide what the decoder
// sees — mixing an x264 segment with an NVENC one inside a single stream
// hands the decoder incompatible parameter sets mid-playback.
type resumeFingerprintInput struct {
	FilePath    string        `json:"file_path"`
	SegExt      string        `json:"seg_ext"`
	SegmentEnds []float64     `json:"segment_ends"`
	Profile     Profile       `json:"profile"`
	HWAccel     HwAccelConfig `json:"hwaccel"`
	AudioTrack  int           `json:"audio_track"`
	ToneMap     bool          `json:"tone_map"`
	UseFMP4     bool          `json:"use_fmp4"`

	// Surgical plan flags, flattened. The plan struct as a whole is not
	// hashable-stable: Reason is prose and Reasons is a bitmask that can gain
	// members without changing a single ffmpeg argument.
	StripDoViEL     bool   `json:"strip_dovi_el"`
	RetagHEVC       bool   `json:"retag_hevc"`
	RetagDoVi       string `json:"retag_dovi"`
	Deinterlace     bool   `json:"deinterlace"`
	Rotate          int    `json:"rotate"`
	FixAnamorphic   bool   `json:"fix_anamorphic"`
	DownmixToStereo bool   `json:"downmix_to_stereo"`
}

// resumeFingerprint hashes the encode plan for a session. Any difference
// that would make old and new segments disagree — about timing or about the
// bitstream — has to change this value.
func resumeFingerprint(filePath, segExt string, ends []float64, opts TranscodeOpts) string {
	in := resumeFingerprintInput{
		FilePath:    filePath,
		SegExt:      segExt,
		SegmentEnds: ends,
		Profile:     opts.Profile,
		HWAccel:     opts.HWAccel,
		AudioTrack:  opts.AudioTrack,
		ToneMap:     opts.ToneMap,
		UseFMP4:     opts.UseFMP4,
	}
	if p := opts.Plan; p != nil {
		in.StripDoViEL = p.StripDoViEL
		in.RetagHEVC = p.RetagHEVC
		in.RetagDoVi = p.RetagDoVi
		in.Deinterlace = p.Deinterlace
		in.Rotate = p.Rotate
		in.FixAnamorphic = p.FixAnamorphic
		in.DownmixToStereo = p.DownmixToStereo
	}
	// json.Marshal over a struct emits fields in declaration order, so the
	// encoding is stable across processes and Go versions.
	blob, err := json.Marshal(in)
	if err != nil {
		// Unreachable for this struct (no channels/funcs/NaN), but a silent
		// empty fingerprint would make every directory look adoptable.
		return ""
	}
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

// readResumeManifest loads the sidecar from dir. A missing or unreadable
// manifest is not an error worth logging — it just means "nothing adoptable
// here", which is the normal state for a brand-new session.
func readResumeManifest(dir string) (resumeManifest, bool) {
	blob, err := os.ReadFile(filepath.Join(dir, resumeManifestName)) //nolint:gosec // server-created cache path
	if err != nil {
		return resumeManifest{}, false
	}
	var m resumeManifest
	if err := json.Unmarshal(blob, &m); err != nil {
		return resumeManifest{}, false
	}
	return m, true
}

func writeResumeManifest(dir string, m resumeManifest) {
	blob, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := produceAtomicOutput(filepath.Join(dir, resumeManifestName), func(tmp string) error {
		return os.WriteFile(tmp, blob, 0o640) //nolint:gosec // server-created cache path
	}); err != nil {
		// Losing the sidecar costs a re-encode after the next restart, not
		// correctness: without it the directory is purged rather than adopted.
		log.Debug().Err(err).Str("key", m.Key).Msg("write transcode resume manifest")
	}
}

// purgeSessionOutput empties dir without removing it. The directory itself is
// already leased by the SessionManager, and dropping it would hand the cache
// an unpinned path to evict between here and the first head's MkdirAll.
func purgeSessionOutput(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			log.Debug().Err(err).Str("dir", dir).Str("entry", e.Name()).Msg("purge stale transcode output")
		}
	}
}

// adoptCachedSegments makes a freshly constructed session take ownership of
// whatever a previous process left in its output directory.
//
// This is what lets playback survive a server restart. The in-memory session
// map is gone after a restart, but the segment URLs a player holds carry
// enough to rebuild the session — and the bytes it already paid to encode are
// still on disk. Without adoption the rebuilt session believes it has nothing,
// re-encodes ground the player already walked, and turns every backward seek
// into a fresh ffmpeg run.
//
// Safety comes from the fingerprint: segments are adopted only when the
// directory's manifest proves they were produced by exactly this plan. Any
// mismatch (different quality, different audio track, a changed hwaccel, a
// re-analysed keyframe map) purges the directory instead — serving segments
// from two different plans inside one stream is worse than re-encoding.
//
// Must be called before the session becomes reachable: it both mutates
// segment latches and can delete files a running head would be writing.
func (s *TranscodeSession) adoptCachedSegments(fingerprint string) int {
	manifest := resumeManifest{
		Version:     resumeManifestVersion,
		Key:         s.Key,
		FilePath:    s.FilePath,
		SegExt:      s.SegExt,
		TotalSegs:   s.TotalSegs,
		Duration:    s.Duration,
		Fingerprint: fingerprint,
	}
	defer writeResumeManifest(s.OutputDir, manifest)

	prior, ok := readResumeManifest(s.OutputDir)
	if !ok || prior.Version != resumeManifestVersion || prior.Fingerprint != fingerprint || fingerprint == "" {
		// Either a virgin directory (purge is a no-op) or leftovers from a plan
		// this session must not mix with.
		purgeSessionOutput(s.OutputDir)
		return 0
	}

	entries, err := os.ReadDir(s.OutputDir)
	if err != nil {
		return 0
	}
	adopted := 0
	for _, e := range entries {
		name := e.Name()
		// ffmpeg's temp_file flag means a final-named segment is fully flushed;
		// anything still being written carries a .tmp suffix and fails this test.
		if !strings.HasSuffix(name, s.SegExt) {
			continue
		}
		idx := parseSegIdx(name)
		if idx < 0 || idx >= s.TotalSegs {
			continue
		}
		s.markSegmentReady(idx)
		adopted++
	}
	return adopted
}
