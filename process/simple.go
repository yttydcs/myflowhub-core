package process

import (
	"context"
	"log/slog"

	core "github.com/yttydcs/myflowhub-core"
)

// SimpleProcess 是一个示例实现：仅记录事件。
type SimpleProcess struct {
	logger *slog.Logger
}

func NewSimple(logger *slog.Logger) *SimpleProcess {
	if logger == nil {
		logger = slog.Default()
	}
	return &SimpleProcess{logger: logger}
}

func (p *SimpleProcess) OnListen(conn core.IConnection) {
	p.logger.Info("connection listen", "id", conn.ID(), "remote", conn.RemoteAddr())
}

func (p *SimpleProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	p.logger.Info("recv", "id", conn.ID(), "bytes", len(payload))
}

func (p *SimpleProcess) OnSend(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) error {
	p.logger.Debug("send", "id", conn.ID(), "bytes", len(payload))
	return nil
}

func (p *SimpleProcess) OnClose(conn core.IConnection) {
	p.logger.Info("connection close", "id", conn.ID())
}
