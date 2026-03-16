package quic_listener

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	quic "github.com/quic-go/quic-go"
	core "github.com/yttydcs/myflowhub-core"
)

const defaultStreamAcceptTimeout = 10 * time.Second

type Options struct {
	Addr   string
	ALPN   string
	Logger *slog.Logger

	CertFile string
	KeyFile  string

	ClientCAFile      string
	RequireClientCert bool
}

func (o *Options) setDefaults() {
	if strings.TrimSpace(o.ALPN) == "" {
		o.ALPN = DefaultALPN
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

func (o Options) Validate() error {
	if err := validateHostPort(strings.TrimSpace(o.Addr)); err != nil {
		return err
	}
	if strings.TrimSpace(o.ALPN) == "" {
		return ErrEndpointALPNInvalid
	}
	if strings.TrimSpace(o.CertFile) == "" || strings.TrimSpace(o.KeyFile) == "" {
		return errors.New("quic listener cert_file and key_file are required")
	}
	return nil
}

type QUICListener struct {
	opts Options
	ln   *quic.Listener

	closed atomic.Bool
}

func New(opts Options) *QUICListener {
	opts.setDefaults()
	return &QUICListener{opts: opts}
}

var _ core.IListener = (*QUICListener)(nil)

func (l *QUICListener) Protocol() string { return "quic" }

func (l *QUICListener) Addr() net.Addr {
	if l.ln == nil {
		return nil
	}
	return &Addr{Address: l.ln.Addr().String(), ALPN: l.opts.ALPN, Role: "listen"}
}

func (l *QUICListener) Listen(ctx context.Context, cm core.IConnectionManager) error {
	if l.closed.Load() {
		return errors.New("quic listener already closed")
	}
	if cm == nil {
		return errors.New("connection manager required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.opts.setDefaults()
	if err := l.opts.Validate(); err != nil {
		return err
	}

	tlsConf, err := buildServerTLSConfig(l.opts)
	if err != nil {
		return err
	}
	ln, err := quic.ListenAddr(strings.TrimSpace(l.opts.Addr), tlsConf, defaultQUICConfig())
	if err != nil {
		return err
	}
	l.ln = ln
	log := l.opts.Logger
	log.Info("quic listener started", "addr", ln.Addr().String(), "alpn", l.opts.ALPN)

	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-ctxDone:
		}
	}()
	defer func() {
		close(ctxDone)
		_ = l.Close()
		log.Info("quic listener stopped")
	}()

	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			if l.closed.Load() || ctx.Err() != nil || errors.Is(err, quic.ErrServerClosed) {
				return nil
			}
			return err
		}

		streamCtx, cancel := context.WithTimeout(ctx, defaultStreamAcceptTimeout)
		stream, err := conn.AcceptStream(streamCtx)
		cancel()
		if err != nil {
			log.Warn("quic accept stream failed", "remote", conn.RemoteAddr().String(), "err", err)
			_ = conn.CloseWithError(0, "stream accept failed")
			continue
		}

		pipe := &quicPipe{conn: conn, stream: stream}
		local := &Addr{Address: conn.LocalAddr().String(), ALPN: l.opts.ALPN, Role: "listen"}
		remote := &Addr{Address: conn.RemoteAddr().String(), ALPN: l.opts.ALPN, Role: "listen"}
		wrapped, err := NewQUICConnection(pipe, local, remote)
		if err != nil {
			_ = pipe.Close()
			log.Warn("quic new connection wrapper failed", "err", err)
			continue
		}
		if err := cm.Add(wrapped); err != nil {
			log.Warn("failed to add quic connection to manager", "remote", remote.String(), "err", err)
			_ = wrapped.Close()
			continue
		}
	}
}

func (l *QUICListener) Close() error {
	l.closed.Store(true)
	if l.ln != nil {
		return l.ln.Close()
	}
	return nil
}

func defaultQUICConfig() *quic.Config {
	return &quic.Config{
		KeepAlivePeriod: 15 * time.Second,
		MaxIdleTimeout:  60 * time.Second,
	}
}
