package process

import (
	"bytes"
	"io"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

type shortFrameWriter struct {
	limit int
	buf   bytes.Buffer
}

func (w *shortFrameWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.limit > 0 && n > w.limit {
		n = w.limit
	}
	if n > 0 {
		_, _ = w.buf.Write(p[:n])
	}
	return n, nil
}

type stubCodec struct {
	encoded []byte
}

func (c stubCodec) Encode(core.IHeader, []byte) ([]byte, error) {
	return append([]byte(nil), c.encoded...), nil
}
func (c stubCodec) Decode(r io.Reader) (core.IHeader, []byte, error) { return nil, nil, nil }

func TestWriteFrameTCPHandlesShortWrites(t *testing.T) {
	dst := &shortFrameWriter{limit: 5}
	payload := []byte("payload-data")
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(1).
		WithTargetID(2).
		WithMsgID(3).
		WithTraceID(4).
		WithPayloadLength(uint32(len(payload)))

	if err := WriteFrame(dst, header.HeaderTcpCodec{}, core.Frame{Header: hdr, Payload: payload}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	gotHdr, gotPayload, err := (header.HeaderTcpCodec{}).Decode(bytes.NewReader(dst.buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotHdr.GetMsgID() != 3 {
		t.Fatalf("msg_id mismatch: got=%d want=3", gotHdr.GetMsgID())
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload mismatch: got=%q want=%q", gotPayload, payload)
	}
}

func TestWriteFrameDefaultCodecHandlesShortWrites(t *testing.T) {
	dst := &shortFrameWriter{limit: 2}
	codec := stubCodec{encoded: []byte("abcdef")}
	if err := WriteFrame(dst, codec, core.Frame{}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if got := dst.buf.String(); got != "abcdef" {
		t.Fatalf("bytes mismatch: got=%q want=%q", got, "abcdef")
	}
}
