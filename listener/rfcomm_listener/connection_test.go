package rfcomm_listener

import (
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/yttydcs/myflowhub-core/header"
)

type noopPipe struct{}

func (noopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (noopPipe) Write(b []byte) (int, error) { return len(b), nil }
func (noopPipe) Close() error                { return nil }

func TestNewRFCOMMConnectionUsesUniqueID(t *testing.T) {
	local := &Addr{UUID: DefaultRFCOMMUUID, Channel: 12, Role: "listen"}
	remote := &Addr{BDAddr: "AA:BB:CC:DD:EE:FF", UUID: DefaultRFCOMMUUID, Channel: 12, Role: "dial"}

	c1, err := NewRFCOMMConnection(noopPipe{}, local, remote)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	c2, err := NewRFCOMMConnection(noopPipe{}, local, remote)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	if c1.ID() == c2.ID() {
		t.Fatalf("connection id duplicated: %s", c1.ID())
	}
}

func TestNewRFCOMMConnectionRejectsNilPipe(t *testing.T) {
	_, err := NewRFCOMMConnection(nil, net.Addr(nil), net.Addr(nil))
	if err == nil {
		t.Fatal("expected nil pipe error")
	}
}

type shortPipe struct {
	buf   bytes.Buffer
	limit int
}

func (p *shortPipe) Read([]byte) (int, error) { return 0, io.EOF }
func (p *shortPipe) Write(b []byte) (int, error) {
	n := len(b)
	if p.limit > 0 && n > p.limit {
		n = p.limit
	}
	if n > 0 {
		_, _ = p.buf.Write(b[:n])
	}
	return n, nil
}
func (p *shortPipe) Close() error { return nil }

func TestRFCOMMConnectionSendWritesAllBytes(t *testing.T) {
	pipe := &shortPipe{limit: 3}
	conn, err := NewRFCOMMConnection(pipe, nil, nil)
	if err != nil {
		t.Fatalf("NewRFCOMMConnection: %v", err)
	}
	data := []byte("abcdef")
	if err := conn.Send(data); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := pipe.buf.String(); got != "abcdef" {
		t.Fatalf("bytes mismatch: got=%q want=%q", got, "abcdef")
	}
}

func TestRFCOMMConnectionSendWithHeaderWritesFullFrame(t *testing.T) {
	pipe := &shortPipe{limit: 4}
	conn, err := NewRFCOMMConnection(pipe, nil, nil)
	if err != nil {
		t.Fatalf("NewRFCOMMConnection: %v", err)
	}
	payload := []byte("hello-rfcomm")
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(1).
		WithTargetID(2).
		WithMsgID(3).
		WithTraceID(4)
	if err := conn.SendWithHeader(hdr, payload, header.HeaderTcpCodec{}); err != nil {
		t.Fatalf("SendWithHeader: %v", err)
	}
	gotHdr, gotPayload, err := (header.HeaderTcpCodec{}).Decode(bytes.NewReader(pipe.buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotHdr.GetMsgID() != 3 {
		t.Fatalf("msg_id mismatch: got=%d want=3", gotHdr.GetMsgID())
	}
	if string(gotPayload) != string(payload) {
		t.Fatalf("payload mismatch: got=%q want=%q", gotPayload, payload)
	}
}
