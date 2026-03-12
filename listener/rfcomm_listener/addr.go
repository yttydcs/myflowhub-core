package rfcomm_listener

import (
	"fmt"
	"net"
	"strings"
)

// Addr is a best-effort net.Addr for RFCOMM connections.
// It is intentionally minimal and string-friendly for logs/diagnostics.
type Addr struct {
	BDAddr  string
	UUID    string
	Channel int
	Role    string // "listen" | "dial" | "" (optional)
}

func (a Addr) Network() string { return "rfcomm" }

func (a Addr) String() string {
	var parts []string
	if s := strings.TrimSpace(a.Role); s != "" {
		parts = append(parts, "role="+s)
	}
	if s := strings.TrimSpace(a.BDAddr); s != "" {
		parts = append(parts, "bdaddr="+s)
	}
	if s := strings.TrimSpace(a.UUID); s != "" {
		parts = append(parts, "uuid="+s)
	}
	if a.Channel > 0 {
		parts = append(parts, fmt.Sprintf("channel=%d", a.Channel))
	}
	if len(parts) == 0 {
		return "rfcomm"
	}
	return "rfcomm(" + strings.Join(parts, ",") + ")"
}

var _ net.Addr = (*Addr)(nil)

