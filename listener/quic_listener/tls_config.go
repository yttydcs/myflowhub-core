package quic_listener

// 本文件承载 Core 框架中与 `tls_config` 相关的通用逻辑。

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// buildServerTLSConfig 组装 QUIC 服务端 TLS 1.3 配置，并按需开启客户端证书校验。
func buildServerTLSConfig(opts Options) (*tls.Config, error) {
	certFile := strings.TrimSpace(opts.CertFile)
	keyFile := strings.TrimSpace(opts.KeyFile)
	if certFile == "" || keyFile == "" {
		return nil, errors.New("quic listener requires cert_file and key_file")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load quic cert/key failed: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{opts.ALPN},
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
	}

	clientCAFile := strings.TrimSpace(opts.ClientCAFile)
	if clientCAFile != "" || opts.RequireClientCert {
		pool, err := loadCertPool(clientCAFile, false)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
		if opts.RequireClientCert {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else if pool != nil {
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}
	return cfg, nil
}

// buildClientTLSConfig 组装 QUIC 客户端 TLS 配置，并可选启用 pin / mTLS。
func buildClientTLSConfig(opts DialOptions) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{opts.ALPN},
		InsecureSkipVerify: opts.Insecure,
		ServerName:         strings.TrimSpace(opts.ServerName),
	}

	if ca := strings.TrimSpace(opts.CAFile); ca != "" {
		pool, err := loadCertPool(ca, true)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}

	certFile := strings.TrimSpace(opts.ClientCertFile)
	keyFile := strings.TrimSpace(opts.ClientKeyFile)
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("quic dial requires both cert and key when client cert is configured")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load quic client cert/key failed: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if pin := strings.TrimSpace(opts.PinSHA256); pin != "" {
		pinBytes, err := normalizePinSHA256(pin)
		if err != nil {
			return nil, err
		}
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || state.PeerCertificates[0] == nil {
				return errors.New("quic peer certificate missing")
			}
			got := sha256.Sum256(state.PeerCertificates[0].Raw)
			if !bytes.Equal(got[:], pinBytes) {
				return fmt.Errorf("quic pin mismatch: want=%s got=%s", pin, hex.EncodeToString(got[:]))
			}
			return nil
		}
	}

	return cfg, nil
}

// loadCertPool 按需加载系统根证书与额外 PEM 文件，供 server/client 复用。
func loadCertPool(file string, useSystem bool) (*x509.CertPool, error) {
	var pool *x509.CertPool
	if useSystem {
		systemPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system cert pool failed: %w", err)
		}
		if systemPool != nil {
			pool = systemPool
		}
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}
	if strings.TrimSpace(file) == "" {
		return pool, nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read cert file failed: %w", err)
	}
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return nil, errors.New("append certs failed")
	}
	return pool, nil
}

// normalizePinSHA256 归一化十六进制 pin 文本，便于直接参与证书摘要比对。
func normalizePinSHA256(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	raw = strings.ReplaceAll(raw, ":", "")
	raw = strings.ToLower(raw)
	if len(raw) != 64 {
		return nil, ErrEndpointPinInvalid
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, ErrEndpointPinInvalid
	}
	return b, nil
}
