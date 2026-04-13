package quic_listener

// Context: This file provides shared Core framework logic around listener_test.

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestQUICListenerDialRoundTrip(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t)
	const alpn = "myflowhub-test"

	l := New(Options{
		Addr:     "127.0.0.1:0",
		ALPN:     alpn,
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	cm := connmgr.New()
	added := make(chan core.IConnection, 1)
	cm.SetHooks(core.ConnectionHooks{
		OnAdd: func(c core.IConnection) {
			select {
			case added <- c:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(ctx, cm)
	}()

	var addr string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := l.Addr().(*Addr); ok && a.Address != "" {
			addr = a.Address
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("listener addr unavailable")
	}

	clientConn, err := Dial(ctx, DialOptions{
		Addr:     addr,
		ALPN:     alpn,
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	codec := header.HeaderTcpCodec{}
	payload := []byte("ping")
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(1).
		WithTargetID(0).
		WithMsgID(1).
		WithPayloadLength(uint32(len(payload)))
	if err := clientConn.SendWithHeader(hdr, payload, codec); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	var serverConn core.IConnection
	select {
	case serverConn = <-added:
	case <-time.After(3 * time.Second):
		t.Fatalf("server connection not added")
	}
	defer serverConn.Close()

	decodedHdr, decodedPayload, err := codec.Decode(bufio.NewReader(serverConn.Pipe()))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decodedHdr.GetMsgID() != 1 {
		t.Fatalf("msg_id = %d, want 1", decodedHdr.GetMsgID())
	}
	if string(decodedPayload) != "ping" {
		t.Fatalf("payload = %q, want %q", string(decodedPayload), "ping")
	}

	cancel()
	select {
	case err := <-listenErr:
		if err != nil {
			t.Fatalf("listen exit err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("listener did not exit")
	}
}

func writeSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "myflowhub-quic-test",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}
