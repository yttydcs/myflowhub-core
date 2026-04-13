//go:build !windows && !linux && !android

package rfcomm_listener

// Context: This file provides shared Core framework logic around native_stub.

import (
	"context"
	"errors"
	"net"

	core "github.com/yttydcs/myflowhub-core"
)

func dialNative(ctx context.Context, opts DialOptions) (core.IPipe, net.Addr, net.Addr, error) {
	return nil, nil, nil, errors.New("rfcomm not supported on this OS")
}

func listenNative(opts Options) (nativeListener, error) {
	return nil, errors.New("rfcomm not supported on this OS")
}
