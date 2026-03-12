//go:build android

package rfcomm_listener

import (
	"context"
	"errors"
	"net"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

func dialNative(ctx context.Context, opts DialOptions) (core.IPipe, net.Addr, net.Addr, error) {
	p, err := getAndroidRFCOMMProvider()
	if err != nil {
		return nil, nil, nil, err
	}
	pipe, err := p.Dial(opts.BDAddr, opts.UUID, opts.Channel, !opts.Insecure)
	if err != nil {
		return nil, nil, nil, err
	}
	remote := &Addr{BDAddr: opts.BDAddr, UUID: opts.UUID, Channel: opts.Channel, Role: "dial"}
	local := &Addr{UUID: opts.UUID, Channel: opts.Channel, Role: "dial"}
	return pipe, local, remote, nil
}

func listenNative(opts Options) (nativeListener, error) {
	p, err := getAndroidRFCOMMProvider()
	if err != nil {
		return nil, err
	}
	l, err := p.Listen(opts.UUID, !opts.Insecure)
	if err != nil {
		return nil, err
	}
	return &androidListener{uuid: opts.UUID, channel: opts.Channel, l: l}, nil
}

type androidListener struct {
	uuid    string
	channel int
	l       AndroidRFCOMMListener
}

func (a *androidListener) Accept() (core.IPipe, net.Addr, net.Addr, error) {
	pipe, err := a.l.Accept()
	if err != nil {
		return nil, nil, nil, err
	}
	if pipe == nil {
		return nil, nil, nil, errors.New("android rfcomm listener returned nil pipe")
	}
	remoteBDAddr := strings.TrimSpace(pipe.RemoteBDAddr())
	if remoteBDAddr != "" {
		if bd, err2 := normalizeBDAddr(remoteBDAddr); err2 == nil {
			remoteBDAddr = bd
		}
	}
	local := &Addr{UUID: a.uuid, Channel: a.channel, Role: "listen"}
	remote := &Addr{BDAddr: remoteBDAddr, UUID: a.uuid, Channel: a.channel, Role: "listen"}
	return pipe, local, remote, nil
}

func (a *androidListener) Close() error { return a.l.Close() }

func (a *androidListener) Addr() net.Addr {
	return &Addr{UUID: a.uuid, Channel: a.channel, Role: "listen"}
}
