package quic_listener

import (
	"fmt"
	"net"
	"strings"
)

// Addr is a best-effort net.Addr for QUIC connections.
type Addr struct {
	Address string
	ALPN    string
	Role    string // listen | dial
}

func (a Addr) Network() string { return "quic" }

func (a Addr) String() string {
	var parts []string
	if s := strings.TrimSpace(a.Role); s != "" {
		parts = append(parts, "role="+s)
	}
	if s := strings.TrimSpace(a.Address); s != "" {
		parts = append(parts, "addr="+s)
	}
	if s := strings.TrimSpace(a.ALPN); s != "" {
		parts = append(parts, "alpn="+s)
	}
	if len(parts) == 0 {
		return "quic"
	}
	return fmt.Sprintf("quic(%s)", strings.Join(parts, ","))
}

var _ net.Addr = (*Addr)(nil)
