package rfcomm_listener

// 本文件承载 Core 框架中与 `endpoint` 相关的通用逻辑。

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// EndpointSchemeRFCOMM is the URI scheme for Bluetooth Classic RFCOMM byte-stream transport.
	//
	// Example:
	//   bt+rfcomm://01:23:45:67:89:AB?uuid=0eef65b8-9374-42ea-b992-6ee2d0699f5c&secure=true
	EndpointSchemeRFCOMM = "bt+rfcomm"

	// DefaultRFCOMMUUID is the default service UUID used by MyFlowHub.
	//
	// NOTE: Keep this consistent with Server's default (hubruntime.Options RFCOMMUUID).
	DefaultRFCOMMUUID = "0eef65b8-9374-42ea-b992-6ee2d0699f5c"
)

var (
	ErrEndpointEmpty          = errors.New("rfcomm endpoint is empty")
	ErrEndpointSchemeInvalid  = errors.New("rfcomm endpoint scheme invalid")
	ErrEndpointAddrMissing    = errors.New("rfcomm endpoint addr missing")
	ErrEndpointAddrInvalid    = errors.New("rfcomm endpoint addr invalid")
	ErrEndpointUUIDInvalid    = errors.New("rfcomm endpoint uuid invalid")
	ErrEndpointChannelInvalid = errors.New("rfcomm endpoint channel invalid")
	ErrEndpointNameReserved   = errors.New("rfcomm endpoint name is reserved (scan-by-name not implemented)")
)

// Endpoint describes a RFCOMM dial target.
// It is a value object (safe to copy).
type Endpoint struct {
	// BDAddr is the remote bluetooth device address in canonical form "AA:BB:CC:DD:EE:FF".
	BDAddr string

	// UUID is the RFCOMM service UUID in canonical lower-case form.
	UUID string

	// Channel is the RFCOMM channel number (1..30). 0 means "resolve by UUID via SDP" (UUID-first).
	Channel int

	// Adapter is the bluetooth adapter name (Linux). Default "hci0" when omitted.
	Adapter string

	// Insecure indicates whether to use insecure RFCOMM (Android).
	//
	// Default false (secure). This shape avoids a tri-state bool problem.
	Insecure bool

	// Name is reserved for future "scan/resolve by device name". v1 must not accept it.
	Name string
}

// Validate 校验 RFCOMM endpoint 的 BDAddr、UUID、channel 与 adapter 组合是否合法。
func (e Endpoint) Validate() error {
	if e.BDAddr == "" {
		return ErrEndpointAddrMissing
	}
	if _, err := normalizeBDAddr(e.BDAddr); err != nil {
		return ErrEndpointAddrInvalid
	}
	if e.UUID == "" {
		return ErrEndpointUUIDInvalid
	}
	if !isUUIDLike(e.UUID) {
		return ErrEndpointUUIDInvalid
	}
	if e.Channel != 0 && (e.Channel < 1 || e.Channel > 30) {
		return ErrEndpointChannelInvalid
	}
	if strings.TrimSpace(e.Adapter) == "" {
		// Adapter is optional, but if provided it must be non-empty after trimming.
		return errors.New("rfcomm endpoint adapter empty")
	}
	if strings.TrimSpace(e.Name) != "" {
		return ErrEndpointNameReserved
	}
	return nil
}

// ParseEndpoint 解析 RFCOMM endpoint URI，并补齐 UUID/channel/adapter 的默认值。
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, ErrEndpointEmpty
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, err
	}
	if strings.ToLower(strings.TrimSpace(u.Scheme)) != EndpointSchemeRFCOMM {
		return Endpoint{}, fmt.Errorf("%w: %s", ErrEndpointSchemeInvalid, u.Scheme)
	}

	// IMPORTANT: do NOT use u.Hostname()/u.Port() because BDADDR contains ':' and might be mis-split.
	addrRaw := strings.TrimSpace(u.Host)
	if addrRaw == "" {
		addrRaw = strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
	}
	if addrRaw == "" {
		return Endpoint{}, ErrEndpointAddrMissing
	}
	bdaddr, err := normalizeBDAddr(addrRaw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: %v", ErrEndpointAddrInvalid, err)
	}

	q := u.Query()
	uuid := strings.TrimSpace(q.Get("uuid"))
	if uuid == "" {
		uuid = DefaultRFCOMMUUID
	}
	uuid = strings.ToLower(uuid)
	if !isUUIDLike(uuid) {
		return Endpoint{}, ErrEndpointUUIDInvalid
	}

	ch := 0
	if rawCh := strings.TrimSpace(q.Get("channel")); rawCh != "" {
		n, err := strconv.Atoi(rawCh)
		if err != nil {
			return Endpoint{}, ErrEndpointChannelInvalid
		}
		if n < 1 || n > 30 {
			return Endpoint{}, ErrEndpointChannelInvalid
		}
		ch = n
	}

	adapter := strings.TrimSpace(q.Get("adapter"))
	if adapter == "" {
		adapter = "hci0"
	}

	secure := true
	if rawSecure := strings.TrimSpace(q.Get("secure")); rawSecure != "" {
		v, err := parseBool(rawSecure)
		if err != nil {
			return Endpoint{}, fmt.Errorf("rfcomm endpoint secure invalid: %w", err)
		}
		secure = v
	}

	name := strings.TrimSpace(q.Get("name"))
	if name != "" {
		return Endpoint{}, ErrEndpointNameReserved
	}

	ep := Endpoint{
		BDAddr:   bdaddr,
		UUID:     uuid,
		Channel:  ch,
		Adapter:  adapter,
		Insecure: !secure,
		Name:     name,
	}
	return ep, ep.Validate()
}

// parseBool 解析 endpoint query 中的布尔开关。
func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %q", raw)
	}
}

// normalizeBDAddr 把多种蓝牙地址输入格式统一成 `AA:BB:CC:DD:EE:FF`。
func normalizeBDAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty")
	}
	// Common forms:
	// - "aa:bb:cc:dd:ee:ff"
	// - "aa-bb-cc-dd-ee-ff"
	// - "aabbccddeeff"
	s := strings.ReplaceAll(raw, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ToUpper(s)
	if len(s) != 12 {
		return "", fmt.Errorf("length %d, want 12 hex chars", len(s))
	}
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') {
			continue
		}
		return "", fmt.Errorf("invalid hex char: %q", c)
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", s[0:2], s[2:4], s[4:6], s[6:8], s[8:10], s[10:12]), nil
}

// isUUIDLike 做一层最小 UUID 形态校验，避免把明显错误的值传给平台层。
func isUUIDLike(s string) bool {
	// Minimal validation: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	// We keep it strict to avoid surprising cross-platform behavior.
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
				continue
			}
			return false
		}
	}
	return true
}
