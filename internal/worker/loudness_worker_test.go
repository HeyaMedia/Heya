package worker

import "testing"

func TestAlbumEBUR128Args(t *testing.T) {
	// Paths ride in argv untouched — no manifest, no quoting layer. The old
	// concat-demuxer manifest rune-truncated non-ASCII paths (♡ → 'a', exit
	// 254) and choked on mixed-codec albums (exit 69); both classes are
	// covered by passing paths verbatim and normalizing per input.
	paths := []string{
		"/music/ano/LoliRockyunRobo♡.flac",
		"/music/ano/ちゅ、多様性。/01. o'malley.mp3",
	}
	args := albumEBUR128Args(paths)

	for i, p := range paths {
		found := false
		for j, a := range args {
			if a == "-i" && j+1 < len(args) && args[j+1] == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("input %d: path %q missing from argv %q", i, p, args)
		}
	}

	fc := ""
	for j, a := range args {
		if a == "-filter_complex" && j+1 < len(args) {
			fc = args[j+1]
			break
		}
	}
	want := "[0:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a0];" +
		"[1:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a1];" +
		"[a0][a1]concat=n=2:v=0:a=1,ebur128=peak=true"
	if fc != want {
		t.Errorf("filter_complex:\n got %q\nwant %q", fc, want)
	}
}
