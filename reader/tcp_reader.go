package reader

// Context: This file provides shared Core framework logic around tcp_reader.

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

// TCPReader keeps the historical name, but now reads transport-neutral frames from conn.Pipe().
type TCPReader struct {
	logger      *slog.Logger
	frameReader core.IFrameReader
}

func NewTCP(logger *slog.Logger) *TCPReader {
	if logger == nil {
		logger = slog.Default()
	}
	return &TCPReader{
		logger:      logger,
		frameReader: NewStreamFrameReader(),
	}
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
		frame, err := r.frameReader.ReadFrame(pipe, codec)
		if err != nil {
			return err
		}
		conn.DispatchReceive(frame.Header, frame.Payload)
	}
}
