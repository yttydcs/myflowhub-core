package quic_listener

// Context: This file provides shared Core framework logic around endpoint.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// EndpointSchemeQUIC is the URI scheme for QUIC transport.
	EndpointSchemeQUIC = "quic"

	// DefaultALPN is the default ALPN token used by MyFlowHub QUIC transport.
	DefaultALPN = "myflowhub"
)

var (
	ErrEndpointEmpty         = errors.New("quic endpoint is empty")
	ErrEndpointSchemeInvalid = errors.New("quic endpoint scheme invalid")
	ErrEndpointAddrMissing   = errors.New("quic endpoint addr missing")
	ErrEndpointAddrInvalid   = errors.New("quic endpoint addr invalid")
	ErrEndpointALPNInvalid   = errors.New("quic endpoint alpn invalid")
	ErrEndpointPinInvalid    = errors.New("quic endpoint pin_sha256 invalid")
)

// Endpoint describes a QUIC dial target.
type Endpoint struct {
	Addr           string
	ServerName     string
	ALPN           string
	Insecure       bool
	PinSHA256      string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
}

func (e Endpoint) Validate() error {
	if err := validateHostPort(e.Addr); err != nil {
		return err
	}
	if strings.TrimSpace(e.ALPN) == "" {
		return ErrEndpointALPNInvalid
	}
	if e.PinSHA256 != "" {
		if _, err := normalizePinSHA256(e.PinSHA256); err != nil {
			return err
		}
	}
	certSet := strings.TrimSpace(e.ClientCertFile) != ""
	keySet := strings.TrimSpace(e.ClientKeyFile) != ""
	if certSet != keySet {
		return errors.New("quic endpoint requires both cert and key when mTLS client cert is configured")
	}
	return nil
}

// ParseEndpoint parses a QUIC endpoint URI.
//
// Format:
//
//	quic://host:port?server_name=example.com&alpn=myflowhub&insecure=false&pin_sha256=<hex>
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, ErrEndpointEmpty
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, err
	}
	if strings.ToLower(strings.TrimSpace(u.Scheme)) != EndpointSchemeQUIC {
		return Endpoint{}, fmt.Errorf("%w: %s", ErrEndpointSchemeInvalid, u.Scheme)
	}

	addr := strings.TrimSpace(u.Host)
	if addr == "" {
		return Endpoint{}, ErrEndpointAddrMissing
	}
	if err := validateHostPort(addr); err != nil {
		return Endpoint{}, err
	}
	host, _, _ := net.SplitHostPort(addr)
	host = strings.Trim(host, "[]")

	q := u.Query()
	alpn := strings.TrimSpace(q.Get("alpn"))
	if alpn == "" {
		alpn = DefaultALPN
	}
	insecure, err := parseBoolDefault(q.Get("insecure"), false)
	if err != nil {
		return Endpoint{}, fmt.Errorf("quic endpoint insecure invalid: %w", err)
	}
	pinRaw := strings.TrimSpace(q.Get("pin_sha256"))
	pinBytes, err := normalizePinSHA256(pinRaw)
	if err != nil {
		return Endpoint{}, err
	}
	pin := ""
	if len(pinBytes) > 0 {
		pin = hex.EncodeToString(pinBytes)
	}

	serverName := strings.TrimSpace(q.Get("server_name"))
	if serverName == "" && net.ParseIP(host) == nil {
		serverName = host
	}

	ep := Endpoint{
		Addr:           addr,
		ServerName:     serverName,
		ALPN:           alpn,
		Insecure:       insecure,
		PinSHA256:      pin,
		CAFile:         strings.TrimSpace(q.Get("ca")),
		ClientCertFile: strings.TrimSpace(q.Get("cert")),
		ClientKeyFile:  strings.TrimSpace(q.Get("key")),
	}
	return ep, ep.Validate()
}

func parseBoolDefault(raw string, def bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %q", raw)
	}
}

func validateHostPort(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ErrEndpointAddrMissing
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEndpointAddrInvalid, err)
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("%w: missing port", ErrEndpointAddrInvalid)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("%w: invalid port", ErrEndpointAddrInvalid)
	}
	// host may be empty (e.g. :9000 for listen)
	_ = host
	return nil
}
