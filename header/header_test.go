package header

import (
	"bytes"
	"errors"
	"testing"
)

func TestHeaderTcpCodec_EncodeDecode_RoundTrip(t *testing.T) {
	codec := HeaderTcpCodec{}
	h := &HeaderTcp{}
	h.WithMajor(MajorMsg)
	h.WithSubProto(7)
	h.WithFlags(FlagACKRequired)
	h.WithHopLimit(10)
	h.WithRouteFlags(0xA5)
	h.WithMsgID(42)
	h.WithSourceID(0x0A0B0C0D)
	h.WithTargetID(0x01020304)
	h.WithTraceID(0x11223344)
	h.WithTimestamp(1700000001)
	payload := []byte("ping")

	frame, err := codec.Encode(h, payload)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if got, want := len(frame), headerTcpSize+len(payload); got != want {
		t.Fatalf("encoded length mismatch: got=%d want=%d", got, want)
	}

	gotH, gotPayload, err := codec.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	vh, ok := gotH.(*HeaderTcp)
	if !ok {
		t.Fatalf("decoded header type mismatch: %T", gotH)
	}
	if vh.Magic != HeaderTcpMagicV2 || vh.Ver != HeaderTcpVersionV2 || vh.HdrLen != headerTcpSize {
		t.Fatalf("fixed fields mismatch: magic=0x%X ver=%d hdr_len=%d", vh.Magic, vh.Ver, vh.HdrLen)
	}
	if vh.Major() != MajorMsg || vh.SubProto() != 7 {
		t.Fatalf("typefmt mismatch: major=%d sub=%d", vh.Major(), vh.SubProto())
	}
	if vh.GetFlags() != FlagACKRequired {
		t.Fatalf("flags mismatch: got=%d want=%d", vh.GetFlags(), FlagACKRequired)
	}
	if vh.GetHopLimit() != 10 || vh.GetRouteFlags() != 0xA5 {
		t.Fatalf("routing fields mismatch: hop=%d route=0x%X", vh.GetHopLimit(), vh.GetRouteFlags())
	}
	if vh.GetMsgID() != 42 || vh.SourceID() != 0x0A0B0C0D || vh.TargetID() != 0x01020304 {
		t.Fatalf("id fields mismatch: msg=%d src=0x%08X tgt=0x%08X", vh.GetMsgID(), vh.SourceID(), vh.TargetID())
	}
	if vh.GetTraceID() != 0x11223344 {
		t.Fatalf("trace_id mismatch: got=0x%X", vh.GetTraceID())
	}
	if vh.GetTimestamp() != 1700000001 {
		t.Fatalf("timestamp mismatch: got=%d", vh.GetTimestamp())
	}
	if vh.PayloadLength() != uint32(len(payload)) {
		t.Fatalf("payload len mismatch: got=%d want=%d", vh.PayloadLength(), len(payload))
	}
	if string(gotPayload) != string(payload) {
		t.Fatalf("payload mismatch: %q vs %q", gotPayload, payload)
	}
}

func TestHeaderTcpCodec_Decode_AllowsExtendedHeader(t *testing.T) {
	codec := HeaderTcpCodec{}
	h := &HeaderTcp{}
	h.WithMajor(MajorCmd).WithSubProto(1).WithSourceID(1).WithTargetID(2).WithMsgID(9)
	payload := []byte("hello")

	frame, err := codec.Encode(h, payload)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	extLen := 40
	ext := make([]byte, extLen+len(payload))
	copy(ext[:headerTcpSize], frame[:headerTcpSize])
	ext[3] = byte(extLen) // hdr_len
	for i := headerTcpSize; i < extLen; i++ {
		ext[i] = 0xEE
	}
	copy(ext[extLen:], payload)

	gotH, gotPayload, err := codec.Decode(bytes.NewReader(ext))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	vh := gotH.(*HeaderTcp)
	if vh.HdrLen != byte(extLen) {
		t.Fatalf("hdr_len mismatch: got=%d want=%d", vh.HdrLen, extLen)
	}
	if string(gotPayload) != string(payload) {
		t.Fatalf("payload mismatch: %q vs %q", gotPayload, payload)
	}
}

func TestHeaderTcpCodec_Decode_RejectsBadMagic(t *testing.T) {
	codec := HeaderTcpCodec{}
	h := &HeaderTcp{}
	h.WithMajor(MajorMsg).WithSubProto(1).WithSourceID(1).WithTargetID(2).WithMsgID(9)

	frame, err := codec.Encode(h, []byte("x"))
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	frame[0], frame[1] = 0, 0
	_, _, err = codec.Decode(bytes.NewReader(frame))
	if !errors.Is(err, ErrHeaderMagicMismatch) {
		t.Fatalf("expected ErrHeaderMagicMismatch, got=%v", err)
	}
}

func TestHeaderTcpCodec_Decode_RejectsBadVersion(t *testing.T) {
	codec := HeaderTcpCodec{}
	h := &HeaderTcp{}
	h.WithMajor(MajorMsg).WithSubProto(1).WithSourceID(1).WithTargetID(2).WithMsgID(9)

	frame, err := codec.Encode(h, []byte("x"))
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	frame[2] = 1 // ver
	_, _, err = codec.Decode(bytes.NewReader(frame))
	if !errors.Is(err, ErrHeaderVersionInvalid) {
		t.Fatalf("expected ErrHeaderVersionInvalid, got=%v", err)
	}
}

func TestHeaderTcpCodec_Decode_RejectsShortHdrLen(t *testing.T) {
	codec := HeaderTcpCodec{}
	h := &HeaderTcp{}
	h.WithMajor(MajorMsg).WithSubProto(1).WithSourceID(1).WithTargetID(2).WithMsgID(9)

	frame, err := codec.Encode(h, []byte("x"))
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	frame[3] = headerTcpSize - 1 // hdr_len
	_, _, err = codec.Decode(bytes.NewReader(frame))
	if !errors.Is(err, ErrHeaderLenInvalid) {
		t.Fatalf("expected ErrHeaderLenInvalid, got=%v", err)
	}
}
