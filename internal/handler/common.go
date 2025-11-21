package handler

import (
	"context"
	"log/slog"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

// Sub-protocol ID 常量定义：统一管理避免分散，demo 中用
const (
	SubProtoEcho = 1 // 回显子协议
)

// CloneRequest 封装请求头部的克隆操作。
func CloneRequest(h core.IHeader) *header.HeaderTcp {
	return header.CloneToTCP(h)
}

// CloneWithTarget 克隆头部并重写目标节点。
func CloneWithTarget(h core.IHeader, target uint32) *header.HeaderTcp {
	clone := header.CloneToTCP(h)
	if clone != nil {
		clone.WithTargetID(target)
	}
	return clone
}

func extractServer(ctx context.Context) core.IServer {
	if ctx == nil {
		return nil
	}
	if srv, ok := ctx.Value(struct{ S string }{"server"}).(core.IServer); ok {
		return srv
	}
	return nil
}

// BuildResponse 根据请求头构建响应头，并指定子协议与载荷长度。
func BuildResponse(req core.IHeader, payloadLen uint32, sub uint8) core.IHeader {
	return header.BuildTCPResponse(req, payloadLen, sub)
}

// SendResponse 编码并通过发送管线发送响应；若无法取得 server，则回退直接写连接。
func SendResponse(ctx context.Context, log *slog.Logger, conn core.IConnection, req core.IHeader, payload []byte, sub uint8) {
	codec := header.HeaderTcpCodec{}
	resp := BuildResponse(req, uint32(len(payload)), sub)
	if srv := extractServer(ctx); srv != nil {
		if err := srv.Send(ctx, conn.ID(), resp, payload); err != nil && log != nil {
			log.Error("发送响应失败", "err", err)
		}
		return
	}
	if err := conn.SendWithHeader(resp, payload, codec); err != nil {
		if log != nil {
			log.Error("发送响应失败", "err", err)
		}
	}
}
