package tests

import (
	"bytes"
	"testing"

	hdr "MyFlowHub-Core/internal/core/header"
)

func TestHeaderTcp_EncodeDecode_RoundTrip(t *testing.T) {
	codec := hdr.HeaderTcpCodec{}
	h := hdr.HeaderTcp{}
	h.WithMajor(hdr.MajorMsg).WithSubProto(7)
	h.Flags = hdr.FlagACKRequired
	h.MsgID = 42
	h.Source = 0x0A0B0C0D
	h.Target = 0x01020304
	h.Timestamp = 1700000001
	payload := []byte("ping")

	frame, err := codec.Encode(h, payload)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}

	gotH, gotPayload, err := codec.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	vh, ok := gotH.(hdr.HeaderTcp)
	if !ok {
		t.Fatalf("decoded header type mismatch: %T", gotH)
	}
	if vh.Major() != hdr.MajorMsg || vh.SubProto() != 7 {
		t.Fatalf("typefmt mismatch: major=%d sub=%d", vh.Major(), vh.SubProto())
	}
	if vh.Flags != hdr.FlagACKRequired || vh.MsgID != h.MsgID || vh.Source != h.Source || vh.Target != h.Target || vh.Timestamp != h.Timestamp || vh.PayloadLen != uint32(len(payload)) {
		t.Fatalf("header fields mismatch: %+v vs expected", vh)
	}
	if string(gotPayload) != string(payload) {
		t.Fatalf("payload mismatch: %q vs %q", gotPayload, payload)
	}
}
