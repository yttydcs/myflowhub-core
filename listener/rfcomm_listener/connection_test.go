package rfcomm_listener

import (
	"io"
	"net"
	"testing"
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
