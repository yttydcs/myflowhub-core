package rfcomm_listener

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	core "github.com/yttydcs/myflowhub-core"
)

type Options struct {
	UUID    string
	Channel int
	Adapter string
	Insecure bool

	Logger *slog.Logger
}

func (o *Options) setDefaults() {
	if strings.TrimSpace(o.UUID) == "" {
		o.UUID = DefaultRFCOMMUUID
	}
	if strings.TrimSpace(o.Adapter) == "" {
		o.Adapter = "hci0"
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

func (o Options) Validate() error {
	if strings.TrimSpace(o.UUID) == "" || !isUUIDLike(o.UUID) {
		return ErrEndpointUUIDInvalid
	}
	if o.Channel != 0 && (o.Channel < 1 || o.Channel > 30) {
		return ErrEndpointChannelInvalid
	}
	if strings.TrimSpace(o.Adapter) == "" {
		return errors.New("adapter empty")
	}
	return nil
}

type nativeListener interface {
	Accept() (core.IPipe, net.Addr, net.Addr, error)
	Close() error
	Addr() net.Addr
}

// RFCOMMListener implements core.IListener for Bluetooth Classic RFCOMM (byte stream).
type RFCOMMListener struct {
	opts Options
	nl   nativeListener

	closed atomic.Bool
}

func New(opts Options) *RFCOMMListener {
	opts.setDefaults()
	opts.UUID = strings.ToLower(strings.TrimSpace(opts.UUID))
	return &RFCOMMListener{opts: opts}
}

var _ core.IListener = (*RFCOMMListener)(nil)

func (l *RFCOMMListener) Protocol() string { return "rfcomm" }

func (l *RFCOMMListener) Addr() net.Addr {
	if l.nl != nil {
		return l.nl.Addr()
	}
	return nil
}

func (l *RFCOMMListener) Listen(ctx context.Context, cm core.IConnectionManager) error {
	if l.closed.Load() {
		return errors.New("rfcomm listener already closed")
	}
	if cm == nil {
		return errors.New("connection manager required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := l.opts.Validate(); err != nil {
		return err
	}

	nl, err := listenNative(l.opts)
	if err != nil {
		return err
	}
	l.nl = nl
	log := l.opts.Logger
	if a := nl.Addr(); a != nil {
		log.Info("rfcomm listener started", "addr", a.String(), "uuid", l.opts.UUID, "channel", l.opts.Channel, "secure", !l.opts.Insecure)
	} else {
		log.Info("rfcomm listener started", "uuid", l.opts.UUID, "channel", l.opts.Channel, "secure", !l.opts.Insecure)
	}

	// ctx cancellation path: close the underlying listener to wake Accept.
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
		log.Info("rfcomm listener stopped")
	}()

	for {
		pipe, local, remote, err := nl.Accept()
		if err != nil {
			if l.closed.Load() || ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Temporary() {
				log.Warn("rfcomm accept temporary error", "err", ne)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		if remote == nil {
			remote = &Addr{UUID: l.opts.UUID, Channel: l.opts.Channel, Role: "listen"}
		}
		if local == nil {
			local = &Addr{UUID: l.opts.UUID, Channel: l.opts.Channel, Role: "listen"}
		}
		conn, err := NewRFCOMMConnection(pipe, local, remote)
		if err != nil {
			_ = pipe.Close()
			log.Warn("rfcomm new connection wrapper failed", "err", err)
			continue
		}
		if err := cm.Add(conn); err != nil {
			log.Warn("failed to add rfcomm connection to manager", "remote", remote.String(), "err", err)
			_ = conn.Close()
			continue
		}
		log.Debug("new rfcomm connection accepted", "remote", remote.String())
	}
}

func (l *RFCOMMListener) Close() error {
	l.closed.Store(true)
	if l.nl != nil {
		return l.nl.Close()
	}
	return nil
}
