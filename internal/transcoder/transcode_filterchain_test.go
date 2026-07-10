package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Filter chain composition tests. These don't shell out to ffmpeg — they
// assert that buildVideoFilterChain wires up the right filters in the right
// order based on PlaybackPlan flags.

func TestBuildVideoFilterChain_Empty(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{Profile: Profile{}})
	assert.Equal(t, "", got)
}

func TestBuildVideoFilterChain_ScaleOnly(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 720},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
	})
	assert.Contains(t, got, "scale=-2:'min(720,ih)'")
}

func TestBuildVideoFilterChain_Deinterlace(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 1080},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
		Plan:    &PlaybackPlan{Deinterlace: true},
	})
	assert.True(t, strings.HasPrefix(got, "yadif,"), "deinterlace runs first; got %q", got)
	assert.Contains(t, got, "scale=-2:'min(1080,ih)'")
}

func TestBuildVideoFilterChain_Rotation(t *testing.T) {
	cases := map[int]string{
		90:  "transpose=1",
		180: "transpose=2,transpose=2",
		270: "transpose=2",
	}
	for deg, want := range cases {
		got := buildVideoFilterChain(TranscodeOpts{
			Profile: Profile{MaxHeight: 1080},
			HWAccel: HwAccelConfig{Type: HwAccelNone},
			Plan:    &PlaybackPlan{Rotate: deg},
		})
		assert.Contains(t, got, want, "rotation %d°", deg)
	}
}

func TestBuildVideoFilterChain_Anamorphic(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 480},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
		Plan:    &PlaybackPlan{FixAnamorphic: true},
	})
	assert.Contains(t, got, "setsar=1:1")
}

func TestBuildVideoFilterChain_DeinterlaceThenRotateThenScale(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 720},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
		Plan: &PlaybackPlan{
			Deinterlace: true,
			Rotate:      90,
		},
	})
	yadifIdx := strings.Index(got, "yadif")
	transposeIdx := strings.Index(got, "transpose")
	scaleIdx := strings.Index(got, "scale=")
	assert.True(t, yadifIdx >= 0 && transposeIdx >= 0 && scaleIdx >= 0, "all filters present: %q", got)
	assert.True(t, yadifIdx < transposeIdx, "deinterlace before transpose")
	assert.True(t, transposeIdx < scaleIdx, "transpose before scale")
}

func TestBuildVideoFilterChain_ToneMapEmbedsScale(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 1080},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
		ToneMap: true,
	})
	// Tone-map carries its own scale step inline — ensure we don't append a
	// second scale on top of it (would double-resize).
	assert.Contains(t, got, "tonemap=hable")
	assert.Equal(t, 1, strings.Count(got, "scale=-2:'min(1080,ih)'"), "exactly one scale step in chain: %q", got)
}

func TestBuildVideoFilterChain_QSV10BitToH264(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 2160},
		HWAccel: HwAccelConfig{Type: HwAccelQSV},
	})
	assert.Equal(t, "scale_qsv=w=-1:h=min(2160\\,ih):format=nv12", got)
}

func TestBuildVideoFilterChain_QSVToneMap(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 2160},
		HWAccel: HwAccelConfig{Type: HwAccelQSV},
		ToneMap: true,
	})
	assert.Equal(t, "vpp_qsv=tonemap=1:format=nv12,scale_qsv=w=-1:h=min(2160\\,ih)", got)
}

func TestBuildVideoFilterChain_DeinterlaceBeforeToneMap(t *testing.T) {
	got := buildVideoFilterChain(TranscodeOpts{
		Profile: Profile{MaxHeight: 1080},
		HWAccel: HwAccelConfig{Type: HwAccelNone},
		ToneMap: true,
		Plan:    &PlaybackPlan{Deinterlace: true},
	})
	yadifIdx := strings.Index(got, "yadif")
	tonemapIdx := strings.Index(got, "tonemap")
	assert.True(t, yadifIdx >= 0 && tonemapIdx >= 0)
	assert.True(t, yadifIdx < tonemapIdx, "deinterlace before tone-map; got %q", got)
}

// appendVideoArgs surgical flags: HEVC retag + DV EL strip on the copy path.

func TestAppendVideoArgs_HEVCRetag(t *testing.T) {
	args := appendVideoArgs(nil, TranscodeOpts{
		Profile: Profile{VideoCodec: "copy"},
		Plan:    &PlaybackPlan{RetagHEVC: true},
	})
	assert.Equal(t, []string{"-c:v", "copy", "-tag:v", "hvc1"}, args)
}

func TestAppendVideoArgs_DVStrip(t *testing.T) {
	args := appendVideoArgs(nil, TranscodeOpts{
		Profile: Profile{VideoCodec: "copy"},
		Plan:    &PlaybackPlan{StripDoViEL: true},
	})
	assert.Contains(t, args, "-bsf:v")
	assert.Contains(t, args, "hevc_metadata=remove_dovi=1")
}

func TestAppendVideoArgs_DoViRetagPreservesConfig(t *testing.T) {
	args := appendVideoArgs(nil, TranscodeOpts{
		Profile: Profile{VideoCodec: "copy"},
		Plan:    &PlaybackPlan{RetagDoVi: "dvh1"},
	})
	assert.Equal(t, []string{"-c:v", "copy", "-tag:v", "dvh1", "-strict", "unofficial"}, args)
}

func TestAppendVideoArgs_BothSurgicalFlags(t *testing.T) {
	args := appendVideoArgs(nil, TranscodeOpts{
		Profile: Profile{VideoCodec: "copy"},
		Plan:    &PlaybackPlan{RetagHEVC: true, StripDoViEL: true},
	})
	assert.Equal(t, "-c:v", args[0])
	assert.Equal(t, "copy", args[1])
	assert.Contains(t, args, "-tag:v")
	assert.Contains(t, args, "hvc1")
	assert.Contains(t, args, "hevc_metadata=remove_dovi=1")
}

// Without a Plan, copy path should be the bare "-c:v copy".
func TestAppendVideoArgs_CopyNoPlan(t *testing.T) {
	args := appendVideoArgs(nil, TranscodeOpts{
		Profile: Profile{VideoCodec: "copy"},
	})
	assert.Equal(t, []string{"-c:v", "copy"}, args)
}
