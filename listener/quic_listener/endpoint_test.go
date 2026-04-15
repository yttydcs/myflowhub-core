package quic_listener

// 本文件覆盖 Core 框架中与 `endpoint` 相关的行为。

import (
	"testing"
)

func TestParseEndpoint_Defaults(t *testing.T) {
	ep, err := ParseEndpoint("quic://127.0.0.1:9000")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if ep.Addr != "127.0.0.1:9000" {
		t.Fatalf("addr = %q, want %q", ep.Addr, "127.0.0.1:9000")
	}
	if ep.ALPN != DefaultALPN {
		t.Fatalf("alpn = %q, want %q", ep.ALPN, DefaultALPN)
	}
	if ep.Insecure {
		t.Fatalf("insecure = true, want false")
	}
}

func TestParseEndpoint_WithPinAndOptions(t *testing.T) {
	raw := "quic://example.com:443?server_name=test.local&alpn=mh&insecure=true&pin_sha256=" +
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	ep, err := ParseEndpoint(raw)
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if ep.ServerName != "test.local" {
		t.Fatalf("server_name = %q, want %q", ep.ServerName, "test.local")
	}
	if ep.ALPN != "mh" {
		t.Fatalf("alpn = %q, want %q", ep.ALPN, "mh")
	}
	if !ep.Insecure {
		t.Fatalf("insecure = false, want true")
	}
	if ep.PinSHA256 == "" {
		t.Fatalf("pin should not be empty")
	}
}

func TestParseEndpoint_Invalid(t *testing.T) {
	_, err := ParseEndpoint("tcp://127.0.0.1:9000")
	if err == nil {
		t.Fatalf("expected scheme error")
	}
	_, err = ParseEndpoint("quic://127.0.0.1")
	if err == nil {
		t.Fatalf("expected addr invalid")
	}
	_, err = ParseEndpoint("quic://127.0.0.1:9000?pin_sha256=1234")
	if err == nil {
		t.Fatalf("expected pin invalid")
	}
}
