//go:build linux && !android

package rfcomm_listener

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"golang.org/x/sys/unix"
)

func dialNative(ctx context.Context, opts DialOptions) (core.IPipe, net.Addr, net.Addr, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Channel > 0 {
		pipe, err := dialRFCOMMByChannel(ctx, opts)
		return pipe, nil, nil, err
	}
	pipe, err := dialRFCOMMByUUIDBlueZ(ctx, opts)
	return pipe, nil, nil, err
}

func listenNative(opts Options) (nativeListener, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return listenRFCOMMBlueZ(opts)
}

func dialRFCOMMByChannel(ctx context.Context, opts DialOptions) (core.IPipe, error) {
	bdaddr, err := normalizeBDAddr(opts.BDAddr)
	if err != nil {
		return nil, err
	}
	if opts.Channel < 1 || opts.Channel > 30 {
		return nil, ErrEndpointChannelInvalid
	}
	// unix.SockaddrRFCOMM uses little-endian byte order (see x/sys/unix docs).
	addrLE, err := bdAddrToRFCOMMAddrLE(bdaddr)
	if err != nil {
		return nil, err
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = unix.Close(fd)
		}
	}()

	sa := &unix.SockaddrRFCOMM{
		Addr:    addrLE,
		Channel: uint8(opts.Channel),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- unix.Connect(fd, sa) }()

	select {
	case <-ctx.Done():
		_ = unix.Close(fd)
		closed = true
		return nil, ctx.Err()
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	}

	f := os.NewFile(uintptr(fd), "rfcomm")
	if f == nil {
		return nil, errors.New("os.NewFile returned nil")
	}
	closed = true // ownership transferred to *os.File
	return f, nil
}

func bdAddrToRFCOMMAddrLE(bdaddr string) ([6]uint8, error) {
	bdaddr = strings.TrimSpace(bdaddr)
	bdaddr, err := normalizeBDAddr(bdaddr)
	if err != nil {
		return [6]uint8{}, err
	}
	parts := strings.Split(bdaddr, ":")
	if len(parts) != 6 {
		return [6]uint8{}, errors.New("invalid bdaddr")
	}
	var out [6]uint8
	for i := 0; i < 6; i++ {
		// Little-endian: reverse order (see x/sys/unix.SockaddrRFCOMM docs).
		b, err := strconvHexByte(parts[5-i])
		if err != nil {
			return [6]uint8{}, err
		}
		out[i] = b
	}
	return out, nil
}
