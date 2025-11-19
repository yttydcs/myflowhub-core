package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	core "MyFlowHub-Core/internal/core"
)

// UpperHandler 大写转换子协议实现。
type UpperHandler struct {
	log *slog.Logger
}

func NewUpperHandler(log *slog.Logger) *UpperHandler {
	if log == nil {
		log = slog.Default()
	}
	return &UpperHandler{log: log}
}

func (h *UpperHandler) SubProto() uint8 { return SubProtoUpper }

func (h *UpperHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	_ = ctx
	tcp, ok := ToHeaderTcp(hdr)
	if !ok {
		h.log.Error("header 类型错误")
		return
	}
	resp := strings.ToUpper(string(payload))
	msg := fmt.Sprintf("UPPER(%d): %s", tcp.MsgID, resp)
	h.log.Info("UpperHandler", "conn", conn.ID(), "payload", string(payload), "resp", msg)
	SendResponse(h.log, conn, tcp, []byte(msg), h.SubProto())
}
