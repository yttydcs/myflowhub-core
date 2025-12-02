package reader

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

// TCPReader 使用 IHeaderCodec 从连接流中解码。
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
	raw := conn.RawConn()
	if raw == nil {
		return errors.New("nil raw conn")
	}
	var closeOnce sync.Once
	go func() {
		select {
		case <-ctx.Done():
			closeOnce.Do(func() { _ = raw.Close() })
		}
	}()
	for {
		select {
		case <-ctx.Done():
			closeOnce.Do(func() { _ = raw.Close() })
			return ctx.Err()
		default:
		}
		h, payload, err := codec.Decode(raw)
		if err != nil {
			return err
		}
		conn.DispatchReceive(h, payload)
	}
}
