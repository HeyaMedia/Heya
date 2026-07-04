package worker

import (
	"encoding/base64"
	"testing"
)

// normalizeChromaprint must map every base64 dialect the two extractors emit
// onto the AcoustID convention (URL-safe, no padding) so DB values compare
// equal regardless of which tool produced them.
func TestNormalizeChromaprint(t *testing.T) {
	// Bytes that force both dialects to differ: 0xfb 0xef encodes to "++8="
	// (std) vs "--8" (url-safe), so mixing them up cannot pass by accident.
	raw := []byte{0xfb, 0xef, 0xbe, 0x01, 0x02, 0x03, 0xff}
	want := base64.RawURLEncoding.EncodeToString(raw)

	cases := map[string]string{
		"url-safe no padding (fpcalc)": base64.RawURLEncoding.EncodeToString(raw),
		"standard padded (ffmpeg)":     base64.StdEncoding.EncodeToString(raw),
		"standard unpadded":            base64.RawStdEncoding.EncodeToString(raw),
	}
	for name, in := range cases {
		got, err := normalizeChromaprint(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}

	if _, err := normalizeChromaprint(""); err == nil {
		t.Error("empty fingerprint should error")
	}
	if _, err := normalizeChromaprint("not!!valid@@base64"); err == nil {
		t.Error("invalid base64 should error")
	}
}
