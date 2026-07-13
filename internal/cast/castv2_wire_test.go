package cast

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCastV2EnvelopeRoundTrip(t *testing.T) {
	want := castV2Envelope{
		SourceID:      "sender-0",
		DestinationID: "receiver-0",
		Namespace:     castV2NSReceiver,
		PayloadUTF8:   `{"type":"GET_STATUS","requestId":1}`,
	}
	var framed bytes.Buffer
	if err := writeCastV2Frame(&framed, want); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	got, err := readCastV2Frame(&framed)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestCastV2RejectsOversizedFrame(t *testing.T) {
	var framed bytes.Buffer
	if err := binary.Write(&framed, binary.BigEndian, uint32(maxCastV2Frame+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := readCastV2Frame(&framed); err == nil {
		t.Fatal("oversized frame was accepted")
	}
}
