package rfcomm_listener

// 本文件承载 Core 框架中与 `dial` 相关的通用逻辑。

import (
	"context"
	"errors"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

type DialOptions struct {
	BDAddr   string
	UUID     string
	Channel  int
	Adapter  string
	Insecure bool
}

// setDefaults 为 RFCOMM 拨号补齐缺省 UUID 与 adapter。
func (o *DialOptions) setDefaults() {
	if strings.TrimSpace(o.UUID) == "" {
		o.UUID = DefaultRFCOMMUUID
	}
	if strings.TrimSpace(o.Adapter) == "" {
		o.Adapter = "hci0"
	}
}

// Validate 校验拨号参数是否足够交给平台原生实现。
func (o DialOptions) Validate() error {
	if strings.TrimSpace(o.BDAddr) == "" {
		return errors.New("bdaddr required")
	}
	if _, err := normalizeBDAddr(o.BDAddr); err != nil {
		return err
	}
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

// DialEndpoint dials a RFCOMM connection from an endpoint URI.
func DialEndpoint(ctx context.Context, endpoint string) (core.IConnection, error) {
	ep, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	return Dial(ctx, DialOptions{
		BDAddr:   ep.BDAddr,
		UUID:     ep.UUID,
		Channel:  ep.Channel,
		Adapter:  ep.Adapter,
		Insecure: ep.Insecure,
	})
}

// Dial 建立 RFCOMM 连接，并在原生 pipe 之上包装成框架连接对象。
func Dial(ctx context.Context, opts DialOptions) (core.IConnection, error) {
	opts.setDefaults()
	opts.UUID = strings.ToLower(strings.TrimSpace(opts.UUID))
	opts.BDAddr = strings.TrimSpace(opts.BDAddr)
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pipe, local, remote, err := dialNative(ctx, opts)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &Addr{BDAddr: opts.BDAddr, UUID: opts.UUID, Channel: opts.Channel, Role: "dial"}
	}
	if local == nil {
		local = &Addr{UUID: opts.UUID, Channel: opts.Channel, Role: "dial"}
	}
	conn, err := NewRFCOMMConnection(pipe, local, remote)
	if err != nil {
		_ = pipe.Close()
		return nil, err
	}
	return conn, nil
}
