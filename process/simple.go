package process

// 本文件承载 Core 框架中与 `simple` 相关的通用逻辑。

import (
	"context"
	"log/slog"

	core "github.com/yttydcs/myflowhub-core"
)

// SimpleProcess 是一个示例实现：仅记录事件。
type SimpleProcess struct {
	logger *slog.Logger
}

// NewSimple 创建一个只做日志观察的最小流程实现，便于 demo 或排障时快速挂载。
func NewSimple(logger *slog.Logger) *SimpleProcess {
	if logger == nil {
		logger = slog.Default()
	}
	return &SimpleProcess{logger: logger}
}

// OnListen 在连接建立时记录基础元信息。
func (p *SimpleProcess) OnListen(conn core.IConnection) {
	p.logger.Info("connection listen", "id", conn.ID(), "remote", conn.RemoteAddr())
}

// OnReceive 仅记录接收事件，不参与任何业务分发。
func (p *SimpleProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	p.logger.Info("recv", "id", conn.ID(), "bytes", len(payload))
}

// OnSend 在发送前输出调试日志，帮助观察发送流量。
func (p *SimpleProcess) OnSend(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) error {
	p.logger.Debug("send", "id", conn.ID(), "bytes", len(payload))
	return nil
}

// OnClose 记录连接关闭事件。
func (p *SimpleProcess) OnClose(conn core.IConnection) {
	p.logger.Info("connection close", "id", conn.ID())
}
