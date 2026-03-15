//go:build windows

package rfcomm_listener

import (
	"errors"
	"io"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRawSockaddrBthRoundTrip(t *testing.T) {
	guid := windows.GUID{
		Data1: 0x0eef65b8,
		Data2: 0x9374,
		Data3: 0x42ea,
		Data4: [8]byte{0xb9, 0x92, 0x6e, 0xe2, 0xd0, 0x69, 0x9f, 0x5c},
	}
	raw := newRawSockaddrBth(0x580205B566F3, guid, 12)
	got := sockaddrBthFromRaw(&raw)

	if got.AddressFamily != afBth {
		t.Fatalf("address family = %d, want %d", got.AddressFamily, afBth)
	}
	if got.BtAddr != 0x580205B566F3 {
		t.Fatalf("bt addr = %#x, want %#x", got.BtAddr, uint64(0x580205B566F3))
	}
	if got.Port != 12 {
		t.Fatalf("port = %d, want 12", got.Port)
	}
	if got.ServiceClass != guid {
		t.Fatalf("service class = %+v, want %+v", got.ServiceClass, guid)
	}
}

func TestNewDialLocalSockaddrBth(t *testing.T) {
	sa := newDialLocalSockaddrBth()
	if sa == nil {
		t.Fatal("sockaddr is nil")
	}
	if sa.BtAddr != 0 {
		t.Fatalf("bt addr = %#x, want 0", sa.BtAddr)
	}
	if sa.Port != 0 {
		t.Fatalf("port = %d, want 0", sa.Port)
	}
	if sa.ServiceClassId != (windows.GUID{}) {
		t.Fatalf("service class = %+v, want zero GUID", sa.ServiceClassId)
	}
}

func TestNewDialRemoteSockaddrBthChannelFirst(t *testing.T) {
	guid := windows.GUID{
		Data1: 0x0eef65b8,
		Data2: 0x9374,
		Data3: 0x42ea,
		Data4: [8]byte{0xb9, 0x92, 0x6e, 0xe2, 0xd0, 0x69, 0x9f, 0x5c},
	}
	sa := newDialRemoteSockaddrBth(0x580205B566F3, guid, 12)
	if sa == nil {
		t.Fatal("sockaddr is nil")
	}
	if sa.BtAddr != 0x580205B566F3 {
		t.Fatalf("bt addr = %#x, want %#x", sa.BtAddr, uint64(0x580205B566F3))
	}
	if sa.Port != 12 {
		t.Fatalf("port = %d, want 12", sa.Port)
	}
	if sa.ServiceClassId != (windows.GUID{}) {
		t.Fatalf("service class = %+v, want zero GUID for channel-first dial", sa.ServiceClassId)
	}
}

func TestNewDialRemoteSockaddrBthUUIDFirst(t *testing.T) {
	guid := windows.GUID{
		Data1: 0x0eef65b8,
		Data2: 0x9374,
		Data3: 0x42ea,
		Data4: [8]byte{0xb9, 0x92, 0x6e, 0xe2, 0xd0, 0x69, 0x9f, 0x5c},
	}
	sa := newDialRemoteSockaddrBth(0x580205B566F3, guid, 0)
	if sa == nil {
		t.Fatal("sockaddr is nil")
	}
	if sa.BtAddr != 0x580205B566F3 {
		t.Fatalf("bt addr = %#x, want %#x", sa.BtAddr, uint64(0x580205B566F3))
	}
	if sa.Port != 0 {
		t.Fatalf("port = %d, want 0", sa.Port)
	}
	if sa.ServiceClassId != guid {
		t.Fatalf("service class = %+v, want %+v", sa.ServiceClassId, guid)
	}
}

func TestWinSockPipeReadZeroReturnsEOF(t *testing.T) {
	orig := wsaRecvFn
	defer func() { wsaRecvFn = orig }()

	wsaRecvFn = func(_ windows.Handle, _ *windows.WSABuf, _ uint32, recvd *uint32, _ *uint32, _ *windows.Overlapped, _ *byte) error {
		*recvd = 0
		return nil
	}

	p := &winSockPipe{}
	buf := make([]byte, 8)
	n, err := p.Read(buf)
	if n != 0 {
		t.Fatalf("read n = %d, want 0", n)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("read err = %v, want EOF", err)
	}
}

func TestWinSockPipeWriteFallbackOnMsgSize(t *testing.T) {
	orig := wsaSendFn
	defer func() { wsaSendFn = orig }()

	calls := 0
	wsaSendFn = func(_ windows.Handle, buf *windows.WSABuf, _ uint32, sent *uint32, _ uint32, _ *windows.Overlapped, _ *byte) error {
		calls++
		if int(buf.Len) > 8 {
			*sent = 0
			return windows.WSAEMSGSIZE
		}
		*sent = buf.Len
		return nil
	}

	p := &winSockPipe{}
	payload := make([]byte, 32)
	n, err := p.Write(payload)
	if err != nil {
		t.Fatalf("write err = %v, want nil", err)
	}
	if n != len(payload) {
		t.Fatalf("write n = %d, want %d", n, len(payload))
	}
	if calls < 2 {
		t.Fatalf("send calls = %d, want >= 2 (fallback expected)", calls)
	}
}
