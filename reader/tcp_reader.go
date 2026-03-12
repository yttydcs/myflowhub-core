package reader

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

// TCPReader 使用 IHeaderCodec 从连接流中解码。
//
// 历史上该 reader 直接依赖 net.Conn（通过 conn.RawConn）；
// 为支持多承载（例如 RFCOMM/串口等），当前实现改为基于 conn.Pipe() 的字节流抽象。
type TCPReader struct {
	logger *slog.Logger
}

func NewTCP(logger *slog.Logger) *TCPReader {
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPReader{logger: logger}
}

func (r *TCPReader) ReadLoop(ctx context.Context, conn core.IConnection, codec core.IHeaderCodec) error {
	pipe := conn.Pipe()
	if pipe == nil {
		return errors.New("nil pipe")
	}
	var closeOnce sync.Once
	go func() {
		select {
		case <-ctx.Done():
			closeOnce.Do(func() { _ = pipe.Close() })
		}
	}()
	for {
		select {
		case <-ctx.Done():
			closeOnce.Do(func() { _ = pipe.Close() })
			return ctx.Err()
		default:
		}
		h, payload, err := codec.Decode(pipe)
		if err != nil {
			return err
		}
		conn.DispatchReceive(h, payload)
	}
}
