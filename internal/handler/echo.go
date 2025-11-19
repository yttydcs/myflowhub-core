package handler

import (
	"context"
	"fmt"
	"log/slog"

	core "MyFlowHub-Core/internal/core"
)

// EchoHandler 回显子协议实现。
type EchoHandler struct {
	log *slog.Logger
}

func NewEchoHandler(log *slog.Logger) *EchoHandler {
	if log == nil {
		log = slog.Default()
	}
	return &EchoHandler{log: log}
}

func (h *EchoHandler) SubProto() uint8 { return SubProtoEcho }

func (h *EchoHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	_ = ctx
	tcp, ok := ToHeaderTcp(hdr)
	if !ok {
		h.log.Error("header 类型错误")
		return
	}
	respPayload := []byte(fmt.Sprintf("ECHO: %s", string(payload)))
	h.log.Info("EchoHandler", "conn", conn.ID(), "payload", string(payload))
	SendResponse(h.log, conn, tcp, respPayload, h.SubProto())
}
