package cast

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Cast v2 frames carry one tiny protobuf message. Keeping this encoder local
// avoids pulling CLI, GUI, and cloud SDK dependency trees into the Heya server
// for a six-field envelope. The wire shape is Google's cast_channel.proto:
// protocol_version, source_id, destination_id, namespace, payload_type,
// payload_utf8.
type castV2Envelope struct {
	SourceID      string
	DestinationID string
	Namespace     string
	PayloadUTF8   string
}

const maxCastV2Frame = 4 << 20

func marshalCastV2Envelope(msg castV2Envelope) []byte {
	out := make([]byte, 0, len(msg.PayloadUTF8)+96)
	out = appendVarintField(out, 1, 0) // CASTV2_1_0
	out = appendBytesField(out, 2, msg.SourceID)
	out = appendBytesField(out, 3, msg.DestinationID)
	out = appendBytesField(out, 4, msg.Namespace)
	out = appendVarintField(out, 5, 0) // STRING
	out = appendBytesField(out, 6, msg.PayloadUTF8)
	return out
}

func unmarshalCastV2Envelope(data []byte) (castV2Envelope, error) {
	var out castV2Envelope
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 {
			return castV2Envelope{}, fmt.Errorf("cast v2: invalid protobuf key")
		}
		data = data[n:]
		field, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			_, n = binary.Uvarint(data)
			if n <= 0 {
				return castV2Envelope{}, fmt.Errorf("cast v2: invalid varint field %d", field)
			}
			data = data[n:]
		case 2:
			length, used := binary.Uvarint(data)
			if used <= 0 || length > uint64(len(data)-used) {
				return castV2Envelope{}, fmt.Errorf("cast v2: invalid bytes field %d", field)
			}
			value := string(data[used : used+int(length)])
			data = data[used+int(length):]
			switch field {
			case 2:
				out.SourceID = value
			case 3:
				out.DestinationID = value
			case 4:
				out.Namespace = value
			case 6:
				out.PayloadUTF8 = value
			}
		case 1:
			if len(data) < 8 {
				return castV2Envelope{}, io.ErrUnexpectedEOF
			}
			data = data[8:]
		case 5:
			if len(data) < 4 {
				return castV2Envelope{}, io.ErrUnexpectedEOF
			}
			data = data[4:]
		default:
			return castV2Envelope{}, fmt.Errorf("cast v2: unsupported protobuf wire type %d", wire)
		}
	}
	if out.SourceID == "" || out.DestinationID == "" || out.Namespace == "" {
		return castV2Envelope{}, fmt.Errorf("cast v2: incomplete envelope")
	}
	return out, nil
}

func appendVarintField(dst []byte, field int, value uint64) []byte {
	dst = binary.AppendUvarint(dst, uint64(field<<3))
	return binary.AppendUvarint(dst, value)
}

func appendBytesField(dst []byte, field int, value string) []byte {
	dst = binary.AppendUvarint(dst, uint64(field<<3|2))
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func readCastV2Frame(r io.Reader) (castV2Envelope, error) {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return castV2Envelope{}, err
	}
	if size == 0 || size > maxCastV2Frame {
		return castV2Envelope{}, fmt.Errorf("cast v2: invalid frame size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return castV2Envelope{}, err
	}
	return unmarshalCastV2Envelope(payload)
}

func writeCastV2Frame(w io.Writer, msg castV2Envelope) error {
	payload := marshalCastV2Envelope(msg)
	if len(payload) == 0 || len(payload) > maxCastV2Frame {
		return fmt.Errorf("cast v2: invalid outbound frame size %d", len(payload))
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(payload))); err != nil { //nolint:gosec // bounded above
		return err
	}
	_, err := w.Write(payload)
	return err
}
