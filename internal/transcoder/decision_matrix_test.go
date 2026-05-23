package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Jellyfin-style decision matrix: each row is (profile, media) → expected
// (Action, Reasons bitmask, CopyVideo, CopyAudio, NeedsFMP4, NeedsToneMap).
//
// Add new scenarios by creating one JSON fixture in testdata/{profiles,mediainfo}/
// and one row in the matrix.

type matrixCase struct {
	profile   string
	media     string
	action    PlaybackAction
	reasons   TranscodeReason
	copyVideo bool
	copyAudio bool
	fmp4      bool
	toneMap   bool
}

func TestDecideMatrix(t *testing.T) {
	cases := []matrixCase{
		// --- Direct play (client supports everything as-is) ----------------
		{"all-codecs", "mp4-h264-aac-2600k", ActionDirectPlay, 0, true, true, false, false},
		{"all-codecs", "mp4-hevc-aac-15200k", ActionDirectPlay, 0, true, true, false, false},
		{"all-codecs", "mkv-av1-opus", ActionDirectPlay, 0, true, true, false, false},
		{"all-codecs", "webm-vp9-opus", ActionDirectPlay, 0, true, true, false, false},

		// --- H.264 + AAC: universally compatible in MP4, remux from MKV ----
		{"default", "mp4-h264-aac-2600k", ActionDirectPlay, 0, true, true, false, false},
		{"chrome", "mp4-h264-aac-2600k", ActionDirectPlay, 0, true, true, false, false},
		{"firefox", "mp4-h264-aac-2600k", ActionDirectPlay, 0, true, true, false, false},
		{"safari", "mp4-h264-aac-2600k", ActionDirectPlay, 0, true, true, false, false},

		{"default", "mkv-h264-aac-2600k", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"chrome", "mkv-h264-aac-2600k", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"firefox", "mkv-h264-aac-2600k", ActionRemux, ReasonContainerNotSupported, true, true, false, false},

		// --- HEVC: needs client support ------------------------------------
		// MP4 HEVC: direct play on Safari, transcode on Chrome/Firefox.
		{"safari", "mp4-hevc-aac-15200k", ActionDirectPlay, 0, true, true, false, false},
		{"firefox-mac", "mp4-hevc-aac-15200k", ActionDirectPlay, 0, true, true, false, false},
		{"chrome", "mp4-hevc-aac-15200k", ActionTranscode, ReasonVideoCodecNotSupported, false, true, false, false},
		{"firefox", "mp4-hevc-aac-15200k", ActionTranscode, ReasonVideoCodecNotSupported, false, true, false, false},

		// MKV HEVC: container + maybe codec issue
		{"safari", "mkv-hevc-aac-2600k", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"firefox-mac", "mkv-hevc-aac-2600k", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"chrome", "mkv-hevc-aac-2600k", ActionTranscode, ReasonContainerNotSupported | ReasonVideoCodecNotSupported, false, true, false, false},
		{"firefox", "mkv-hevc-aac-2600k", ActionTranscode, ReasonContainerNotSupported | ReasonVideoCodecNotSupported, false, true, false, false},

		// --- AV1: requires fMP4 segments when remuxing --------------------
		{"chrome", "mkv-av1-aac", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"firefox", "mkv-av1-aac", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"safari", "mkv-av1-aac", ActionTranscode, ReasonContainerNotSupported | ReasonVideoCodecNotSupported, false, true, false, false},

		// AV1 + Opus (Oshi no Ko S01): remux video + audio via fMP4
		{"chrome", "mkv-av1-opus", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"firefox", "mkv-av1-opus", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"firefox-mac", "mkv-av1-opus", ActionRemux, ReasonContainerNotSupported, true, true, true, false},

		// AV1 + EAC3 (Nobody 2 / Oshi S02E13): video copy, audio transcode on
		// Firefox. Because the video stream copies, this stays under the
		// Remux label — audio re-encode alone doesn't justify the Transcode
		// badge (DecideForHLS uses the same convention).
		{"chrome", "mkv-av1-eac3-5_1", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"firefox", "mkv-av1-eac3-5_1", ActionRemux, ReasonContainerNotSupported | ReasonAudioCodecNotSupported, true, false, true, false},

		// --- VP9 + Opus in WebM: native direct play in WebM-capable browsers
		{"chrome", "webm-vp9-opus", ActionDirectPlay, 0, true, true, false, false},
		{"firefox", "webm-vp9-opus", ActionDirectPlay, 0, true, true, false, false},
		// Safari has no VP9/WebM support
		{"safari", "webm-vp9-opus", ActionTranscode, ReasonContainerNotSupported | ReasonVideoCodecNotSupported | ReasonAudioCodecNotSupported, false, false, false, false},

		// --- HDR: tone-map for SDR clients ---------------------------------
		// 4K HDR HEVC on Safari (HDR-capable): direct play
		{"safari", "mp4-hevc-hdr10", ActionDirectPlay, 0, true, true, false, false},
		// Same content on Chrome/Firefox: HDR tone-map forces video transcode
		{"chrome", "mp4-hevc-hdr10", ActionTranscode, ReasonHDRNotSupported, false, true, false, true},
		{"firefox-mac", "mp4-hevc-hdr10", ActionTranscode, ReasonHDRNotSupported, false, true, false, true},

		// HDR HEVC + EAC3 in MKV: container + HDR + (maybe) audio
		{"safari", "mkv-hevc-eac3-15200k-hdr", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"firefox", "mkv-hevc-eac3-15200k-hdr", ActionTranscode, ReasonContainerNotSupported | ReasonHDRNotSupported | ReasonAudioCodecNotSupported, false, false, false, true},

		// --- FLAC audio: Firefox/Chrome OK, Safari has it too --------------
		{"chrome", "mkv-h264-flac", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"firefox", "mkv-h264-flac", ActionRemux, ReasonContainerNotSupported, true, true, false, false},
		{"default", "mkv-h264-flac", ActionRemux, ReasonContainerNotSupported | ReasonAudioCodecNotSupported, true, false, false, false},

		// --- Empty / no streams: safe transcode fallback -------------------
		{"chrome", "empty", ActionTranscode, 0, false, false, false, false}, // reasons=0 because empty short-circuits

		// --- Chromecast: surround AAC/EAC3 passthrough -------------------
		{"chromecast", "mkv-av1-eac3-5_1", ActionRemux, ReasonContainerNotSupported, true, true, true, false},
		{"chromecast", "mp4-hevc-hdr10", ActionDirectPlay, 0, true, true, false, false},

		// --- Dolby Vision profile 8 with HDR10 base layer ----------------
		// DoVi-capable clients direct-play the source. Non-DoVi clients with
		// HEVC support can remux + strip the RPU/EL to deliver plain HDR10.
		{"safari-tv", "mp4-hevc-dvh1.08-hdr10bl", ActionDirectPlay, 0, true, true, false, false},
		{"nvidia-shield", "mp4-hevc-dvh1.08-hdr10bl", ActionDirectPlay, 0, true, true, false, false},
		{"chromecast-ultra", "mp4-hevc-dvh1.08-hdr10bl", ActionRemux, ReasonDolbyVisionNotSupported, true, true, false, false},

		// --- Dolby Vision profile 5 (DV-only, no HDR10 fallback) ----------
		// DoVi-capable clients direct-play. Anyone else must transcode +
		// tone-map.
		{"safari-tv", "mp4-hevc-dvh1.05-15200k", ActionDirectPlay, 0, true, true, false, false},
		{"chromecast-ultra", "mp4-hevc-dvh1.05-15200k", ActionTranscode, ReasonDolbyVisionNotSupported, false, true, false, true},
		{"chrome", "mp4-hevc-dvh1.05-15200k", ActionTranscode, ReasonDolbyVisionNotSupported | ReasonHDRNotSupported, false, true, false, true},

		// --- Dolby Vision profile 7 + TrueHD ------------------------------
		// Lossless audio always transcodes. DV7 has a separate EL track that
		// we can't strip via bitstream filter alone, so non-DoVi clients
		// transcode the video.
		{"nvidia-shield", "mp4-hevc-dvh1.07-truehd", ActionRemux, ReasonAudioLosslessNotSupported, true, false, false, false},
		{"chromecast-ultra", "mp4-hevc-dvh1.07-truehd", ActionTranscode, ReasonDolbyVisionNotSupported | ReasonAudioLosslessNotSupported, false, false, false, true},
		{"safari-tv", "mp4-hevc-dvh1.07-truehd", ActionRemux, ReasonAudioLosslessNotSupported, true, false, false, false},

		// --- HEVC `hev1` codec tag -----------------------------------------
		// Safari needs `hvc1`; we remux with -tag:v hvc1. Clients that accept
		// either tag (chromecast-ultra, nvidia-shield) direct-play.
		{"safari", "mp4-hevc-hev1-aac", ActionRemux, ReasonVideoCodecTagNotSupported, true, true, false, false},
		{"safari-tv", "mp4-hevc-hev1-aac", ActionRemux, ReasonVideoCodecTagNotSupported, true, true, false, false},
		{"chromecast-ultra", "mp4-hevc-hev1-aac", ActionDirectPlay, 0, true, true, false, false},
		{"nvidia-shield", "mp4-hevc-hev1-aac", ActionDirectPlay, 0, true, true, false, false},

		// --- Lossless audio ------------------------------------------------
		// TrueHD / DTS / DTS-HD MA / PCM never play in MSE — always transcode
		// to AAC. Video can still copy if otherwise compatible.
		{"chrome", "mkv-h264-truehd-7_1", ActionRemux, ReasonContainerNotSupported | ReasonAudioLosslessNotSupported, true, false, false, false},
		{"safari", "mkv-h264-truehd-7_1", ActionRemux, ReasonContainerNotSupported | ReasonAudioLosslessNotSupported, true, false, false, false},
		{"chrome", "mkv-h264-dts-5_1", ActionRemux, ReasonContainerNotSupported | ReasonAudioLosslessNotSupported, true, false, false, false},
		{"chrome", "mkv-h264-dtshd-7_1-atmos", ActionRemux, ReasonContainerNotSupported | ReasonAudioLosslessNotSupported, true, false, false, false},
		// nvidia-shield supports MKV natively — only audio reason remains.
		{"nvidia-shield", "mkv-h264-truehd-7_1", ActionRemux, ReasonAudioLosslessNotSupported, true, false, false, false},
		{"nvidia-shield", "mkv-h264-dtshd-7_1-atmos", ActionRemux, ReasonAudioLosslessNotSupported, true, false, false, false},

		// --- Video rotation ------------------------------------------------
		// Phone-recorded portrait clips and upside-down videos: browsers ignore
		// Display Matrix in MSE, so we transcode + transpose.
		{"chrome", "mp4-h264-rotated-portrait", ActionTranscode, ReasonVideoRotationNotSupported, false, true, false, false},
		{"safari", "mp4-h264-rotated-portrait", ActionTranscode, ReasonVideoRotationNotSupported, false, true, false, false},
		{"chrome", "mp4-h264-aac-rotated-180", ActionTranscode, ReasonVideoRotationNotSupported, false, true, false, false},

		// --- Anamorphic ---------------------------------------------------
		// DVD rips with SAR != 1:1. Browsers ignore PAR in MSE.
		{"chrome", "mp4-h264-anamorphic", ActionTranscode, ReasonAnamorphicNotSupported, false, true, false, false},
		{"safari", "mp4-h264-anamorphic", ActionTranscode, ReasonAnamorphicNotSupported, false, true, false, false},

		// --- Interlaced ---------------------------------------------------
		// 1080i broadcast. Browsers can't deinterlace in MSE.
		{"chrome", "mp4-h264-interlaced-1080i", ActionTranscode, ReasonInterlacedNotSupported, false, true, false, false},
		{"firefox", "mp4-h264-interlaced-1080i", ActionTranscode, ReasonInterlacedNotSupported, false, true, false, false},

		// --- Hi10P (10-bit H.264) -----------------------------------------
		// Plays in browsers via MSE in MP4 fine — only the MPEG-TS HLS path
		// can't carry it. Decide() returns direct play; DecideForHLS()
		// transcodes (see TestDecideForHLSMatrix).
		{"chrome", "mp4-h264-hi10p-aac", ActionDirectPlay, 0, true, true, false, false},
		{"safari", "mp4-h264-hi10p-aac", ActionDirectPlay, 0, true, true, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.media, func(t *testing.T) {
			caps := loadProfile(t, tc.profile)
			info := loadMediaInfo(t, tc.media)

			got := Decide(&info, caps)

			assert.Equal(t, tc.action, got.Action, "action")
			assert.Equal(t, tc.reasons, got.Reasons, "reasons bitmask (got: %s)", got.Reasons)
			assert.Equal(t, tc.copyVideo, got.CopyVideo, "copy_video")
			assert.Equal(t, tc.copyAudio, got.CopyAudio, "copy_audio")
			assert.Equal(t, tc.fmp4, got.NeedsFMP4, "needs_fmp4")
			assert.Equal(t, tc.toneMap, got.NeedsToneMap, "needs_tonemap")
		})
	}
}

// TestDecideForHLSMatrix exercises the HLS-specific decision path (when
// direct play has been ruled out and the player is asking for HLS).
func TestDecideForHLSMatrix(t *testing.T) {
	type hlsCase struct {
		profile    string
		media      string
		audioTrack int
		action     PlaybackAction
		copyVideo  bool
		copyAudio  bool
		fmp4       bool
	}
	cases := []hlsCase{
		// H.264 + AAC: copy both, MPEG-TS
		{"chrome", "mkv-h264-aac-2600k", 0, ActionRemux, true, true, false},
		{"firefox", "mkv-h264-aac-2600k", 0, ActionRemux, true, true, false},

		// H.264 Hi10P: cannot copy to MPEG-TS — transcode video
		{"chrome", "mp4-h264-hi10p-aac", 0, ActionTranscode, false, true, false},

		// HEVC: copy if client supports, MPEG-TS
		{"safari", "mkv-hevc-aac-2600k", 0, ActionRemux, true, true, false},
		{"firefox", "mkv-hevc-aac-2600k", 0, ActionTranscode, false, true, false},

		// AV1: needs fMP4
		{"chrome", "mkv-av1-opus", 0, ActionRemux, true, true, true},
		{"firefox", "mkv-av1-opus", 0, ActionRemux, true, true, true},

		// Multi-audio AV1: track 0 (EAC3) transcoded on Firefox; track 1 (Opus) copied
		{"firefox", "mkv-av1-opus-eac3-multi", 0, ActionRemux, true, false, true}, // EAC3 → AAC, copy video
		{"firefox", "mkv-av1-opus-eac3-multi", 1, ActionRemux, true, true, true},  // Opus copy

		// AV1 + EAC3 5.1 (Nobody 2): video copy, audio transcode on Firefox
		{"firefox", "mkv-av1-eac3-5_1", 0, ActionRemux, true, false, true},
		// Same on Chrome (EAC3 supported): full copy
		{"chrome", "mkv-av1-eac3-5_1", 0, ActionRemux, true, true, true},

		// HDR HEVC: forced transcode (tone-map) regardless of codec support
		{"safari", "mkv-hevc-eac3-15200k-hdr", 0, ActionRemux, true, true, false},
		{"firefox", "mkv-hevc-eac3-15200k-hdr", 0, ActionTranscode, false, false, false},

		// Lossless audio: video can still copy, audio re-encodes
		{"chrome", "mkv-h264-truehd-7_1", 0, ActionRemux, true, false, false},
		{"chrome", "mkv-h264-dts-5_1", 0, ActionRemux, true, false, false},

		// Rotation / interlace / anamorphic: force video transcode in HLS
		{"chrome", "mp4-h264-rotated-portrait", 0, ActionTranscode, false, true, false},
		{"chrome", "mp4-h264-interlaced-1080i", 0, ActionTranscode, false, true, false},
		{"chrome", "mp4-h264-anamorphic", 0, ActionTranscode, false, true, false},

		// HEVC hev1 codec tag: safari can copy the HEVC stream into fMP4 and
		// retag to hvc1. (HEVC stays in MPEG-TS — no fmp4 forced.)
		{"safari", "mp4-hevc-hev1-aac", 0, ActionRemux, true, true, false},

		// Dolby Vision profile 8 + HDR10 BL: HEVC-capable non-DoVi client
		// can copy (strip EL via bsf). DoVi-capable client direct-plays
		// (not visible in this matrix — that's the Decide() path).
		{"chromecast-ultra", "mp4-hevc-dvh1.08-hdr10bl", 0, ActionRemux, true, true, false},
		// DV5 forces transcode (no HDR10 base layer to strip).
		{"chromecast-ultra", "mp4-hevc-dvh1.05-15200k", 0, ActionTranscode, false, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.media, func(t *testing.T) {
			caps := loadProfile(t, tc.profile)
			info := loadMediaInfo(t, tc.media)

			got := DecideForHLS(&info, tc.audioTrack, caps)

			assert.Equal(t, tc.action, got.Action, "action")
			assert.Equal(t, tc.copyVideo, got.CopyVideo, "copy_video")
			assert.Equal(t, tc.copyAudio, got.CopyAudio, "copy_audio")
			assert.Equal(t, tc.fmp4, got.NeedsFMP4, "needs_fmp4")
		})
	}
}

func TestTranscodeReason_String(t *testing.T) {
	assert.Equal(t, "", TranscodeReason(0).String())
	assert.Equal(t, "container", ReasonContainerNotSupported.String())
	assert.Equal(t, "container, audio codec", (ReasonContainerNotSupported | ReasonAudioCodecNotSupported).String())
	assert.True(t, (ReasonContainerNotSupported | ReasonHDRNotSupported).Has(ReasonHDRNotSupported))
	assert.False(t, ReasonContainerNotSupported.Has(ReasonHDRNotSupported))

	// New reason bits added in the DV/rotation/anamorphic/lossless expansion.
	assert.Equal(t, "codec tag", ReasonVideoCodecTagNotSupported.String())
	assert.Equal(t, "rotation", ReasonVideoRotationNotSupported.String())
	assert.Equal(t, "interlaced", ReasonInterlacedNotSupported.String())
	assert.Equal(t, "anamorphic", ReasonAnamorphicNotSupported.String())
	assert.Equal(t, "lossless audio", ReasonAudioLosslessNotSupported.String())
	assert.Equal(t, "dolby vision", ReasonDolbyVisionNotSupported.String())
	// Bit order in reasonNames determines display order: lossless < dolby vision.
	assert.Equal(t, "lossless audio, dolby vision", (ReasonDolbyVisionNotSupported | ReasonAudioLosslessNotSupported).String())
}

// TestDetectionHelpers covers the small predicate helpers in decision.go.
func TestDetectionHelpers(t *testing.T) {
	t.Run("IsDolbyVision", func(t *testing.T) {
		assert.False(t, IsDolbyVision(nil))
		assert.False(t, IsDolbyVision(&StreamInfo{}))
		assert.True(t, IsDolbyVision(&StreamInfo{DvProfile: 5}))
		assert.True(t, IsDolbyVision(&StreamInfo{DvProfile: 8}))
	})
	t.Run("IsDoViHDR10Compatible", func(t *testing.T) {
		assert.False(t, IsDoViHDR10Compatible(nil))
		assert.False(t, IsDoViHDR10Compatible(&StreamInfo{DvProfile: 5}))                  // DV-only
		assert.False(t, IsDoViHDR10Compatible(&StreamInfo{DvProfile: 7}))                  // EL track
		assert.False(t, IsDoViHDR10Compatible(&StreamInfo{DvProfile: 8, DvBlCompatID: 0})) // DV-only BL
		assert.True(t, IsDoViHDR10Compatible(&StreamInfo{DvProfile: 8, DvBlCompatID: 1}))  // HDR10 BL
		assert.False(t, IsDoViHDR10Compatible(&StreamInfo{DvProfile: 8, DvBlCompatID: 4})) // HLG BL (not HDR10)
	})
	t.Run("IsLosslessAudio", func(t *testing.T) {
		for _, codec := range []string{"truehd", "TrueHD", "MLP", "dts", "dts-hd", "dtshd", "pcm_s16le", "pcm_s24be", "pcm_bluray"} {
			assert.True(t, IsLosslessAudio(codec), codec)
		}
		for _, codec := range []string{"aac", "ac3", "eac3", "opus", "flac", "mp3", ""} {
			assert.False(t, IsLosslessAudio(codec), codec)
		}
	})
	t.Run("IsInterlaced", func(t *testing.T) {
		assert.False(t, IsInterlaced(nil))
		assert.False(t, IsInterlaced(&StreamInfo{FieldOrder: "progressive"}))
		assert.False(t, IsInterlaced(&StreamInfo{FieldOrder: ""}))
		assert.False(t, IsInterlaced(&StreamInfo{FieldOrder: "unknown"}))
		assert.True(t, IsInterlaced(&StreamInfo{FieldOrder: "tt"}))
		assert.True(t, IsInterlaced(&StreamInfo{FieldOrder: "bb"}))
		assert.True(t, IsInterlaced(&StreamInfo{FieldOrder: "tb"}))
	})
	t.Run("IsAnamorphic", func(t *testing.T) {
		assert.False(t, IsAnamorphic(""))
		assert.False(t, IsAnamorphic("1:1"))
		assert.False(t, IsAnamorphic("0:0"))
		assert.False(t, IsAnamorphic("0:1"))
		assert.True(t, IsAnamorphic("8:9"))
		assert.True(t, IsAnamorphic("32:27"))
	})
	t.Run("IsRotated", func(t *testing.T) {
		assert.False(t, IsRotated(nil))
		assert.False(t, IsRotated(&StreamInfo{}))
		assert.True(t, IsRotated(&StreamInfo{Rotation: 90}))
		assert.True(t, IsRotated(&StreamInfo{Rotation: 270}))
	})
	t.Run("IsHEVCHev1Tag", func(t *testing.T) {
		assert.False(t, IsHEVCHev1Tag(nil))
		assert.False(t, IsHEVCHev1Tag(&StreamInfo{CodecName: "h264", CodecTag: "hev1"}))
		assert.False(t, IsHEVCHev1Tag(&StreamInfo{CodecName: "hevc", CodecTag: "hvc1"}))
		assert.False(t, IsHEVCHev1Tag(&StreamInfo{CodecName: "hevc", CodecTag: ""}))
		assert.True(t, IsHEVCHev1Tag(&StreamInfo{CodecName: "hevc", CodecTag: "hev1"}))
		assert.True(t, IsHEVCHev1Tag(&StreamInfo{CodecName: "h265", CodecTag: "hev1"}))
	})
	t.Run("SubtitleDeliveryFor", func(t *testing.T) {
		external := []string{"srt", "SubRip", "subrip", "webvtt", "vtt", "ass", "ssa", "mov_text", "text"}
		burnIn := []string{"pgs", "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "dvbsub"}
		unsupported := []string{"unknown", "", "weird_codec"}
		for _, c := range external {
			assert.Equal(t, SubDeliveryExternal, SubtitleDeliveryFor(c), c)
		}
		for _, c := range burnIn {
			assert.Equal(t, SubDeliveryBurnIn, SubtitleDeliveryFor(c), c)
		}
		for _, c := range unsupported {
			assert.Equal(t, SubDeliveryUnsupported, SubtitleDeliveryFor(c), c)
		}
	})
}
