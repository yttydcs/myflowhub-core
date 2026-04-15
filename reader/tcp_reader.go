package reader

// 本文件承载 Core 框架中与 `tcp_reader` 相关的通用逻辑。

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

// TCPReader 保留历史名称，但底层已改为从任意 pipe 读取传输无关的帧。
type TCPReader struct {
	logger      *slog.Logger
	frameReader core.IFrameReader
}

// NewTCP 创建读取循环，并默认挂上通用的 StreamFrameReader。
func NewTCP(logger *slog.Logger) *TCPReader {
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPReader{
		logger:      logger,
		frameReader: NewStreamFrameReader(),
	}
}

// ReadLoop 持续从连接 pipe 读取帧并回调连接分发，ctx 取消时会主动关闭 pipe 以打断阻塞读取。
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
		frame, err := r.frameReader.ReadFrame(pipe, codec)
		if err != nil {
			return err
		}
		conn.DispatchReceive(frame.Header, frame.Payload)
	}
}
