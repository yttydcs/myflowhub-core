package quic_listener

// 本文件承载 Core 框架中与 `dial` 相关的通用逻辑。

import (
	"context"
	"errors"
	"net"
	"strings"

	quic "github.com/quic-go/quic-go"
	core "github.com/yttydcs/myflowhub-core"
)

type DialOptions struct {
	Addr           string
	ServerName     string
	ALPN           string
	Insecure       bool
	PinSHA256      string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
}

// setDefaults 补齐 QUIC 拨号所需的 ALPN 与默认 ServerName。
func (o *DialOptions) setDefaults() {
	if strings.TrimSpace(o.ALPN) == "" {
		o.ALPN = DefaultALPN
	}
	addr := strings.TrimSpace(o.Addr)
	host, _, err := net.SplitHostPort(addr)
	if err == nil && strings.TrimSpace(o.ServerName) == "" {
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) == nil {
			o.ServerName = host
		}
	}
}

// Validate 校验 QUIC 拨号参数是否满足 TLS 与 endpoint 约束。
func (o DialOptions) Validate() error {
	if err := validateHostPort(strings.TrimSpace(o.Addr)); err != nil {
		return err
	}
	if strings.TrimSpace(o.ALPN) == "" {
		return ErrEndpointALPNInvalid
	}
	if o.PinSHA256 != "" {
		if _, err := normalizePinSHA256(o.PinSHA256); err != nil {
			return err
		}
	}
	certSet := strings.TrimSpace(o.ClientCertFile) != ""
	keySet := strings.TrimSpace(o.ClientKeyFile) != ""
	if certSet != keySet {
		return errors.New("quic dial requires both cert and key when client cert is configured")
	}
	return nil
}

// DialEndpoint dials a QUIC connection from an endpoint URI.
func DialEndpoint(ctx context.Context, endpoint string) (core.IConnection, error) {
	ep, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	return Dial(ctx, DialOptions{
		Addr:           ep.Addr,
		ServerName:     ep.ServerName,
		ALPN:           ep.ALPN,
		Insecure:       ep.Insecure,
		PinSHA256:      ep.PinSHA256,
		CAFile:         ep.CAFile,
		ClientCertFile: ep.ClientCertFile,
		ClientKeyFile:  ep.ClientKeyFile,
	})
}

// Dial 建立 QUIC 连接并打开单条 stream，再包装成框架连接对象。
func Dial(ctx context.Context, opts DialOptions) (core.IConnection, error) {
	opts.setDefaults()
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tlsConf, err := buildClientTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	conn, err := quic.DialAddr(ctx, strings.TrimSpace(opts.Addr), tlsConf, defaultQUICConfig())
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "open stream failed")
		return nil, err
	}

	pipe := &quicPipe{conn: conn, stream: stream}
	local := &Addr{Address: conn.LocalAddr().String(), ALPN: opts.ALPN, Role: "dial"}
	remote := &Addr{Address: conn.RemoteAddr().String(), ALPN: opts.ALPN, Role: "dial"}
	wrapped, err := NewQUICConnection(pipe, local, remote)
	if err != nil {
		_ = pipe.Close()
		return nil, err
	}
	return wrapped, nil
}
