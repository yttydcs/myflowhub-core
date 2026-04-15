//go:build !windows && !linux && !android

package rfcomm_listener

// 本文件承载 Core 框架中与 `native_stub` 相关的通用逻辑。

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
