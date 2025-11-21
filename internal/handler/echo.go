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
	req := CloneRequest(hdr)
	respPayload := []byte(fmt.Sprintf("ECHO: %s", string(payload)))
	h.log.Info("EchoHandler", "conn", conn.ID(), "payload", string(payload))
	SendResponse(ctx, h.log, conn, req, respPayload, h.SubProto())
}
