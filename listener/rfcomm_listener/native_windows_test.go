//go:build windows

package rfcomm_listener

import (
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
