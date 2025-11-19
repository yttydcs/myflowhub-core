package handler

import (
	"log/slog"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

// Sub-protocol ID 常量定义：统一管理避免分散在 demo 中。
const (
	SubProtoEcho  = 1 // 回显子协议
	SubProtoUpper = 3 // 大写转换子协议（2 预留给登录）
)

// ToHeaderTcp 尝试将通用 core.IHeader 转换为 header.HeaderTcp。
func ToHeaderTcp(h core.IHeader) (header.HeaderTcp, bool) {
	if v, ok := h.(*header.HeaderTcp); ok && v != nil {
		return *v, true
	}
	return header.HeaderTcp{}, false
}

// BuildResponse 根据请求头构建响应头，并指定子协议与载荷长度。
func BuildResponse(req header.HeaderTcp, payloadLen uint32, sub uint8) header.HeaderTcp {
	resp := header.HeaderTcp{
		MsgID:      req.MsgID,
		Source:     req.Target,
		Target:     req.Source,
		Timestamp:  uint32(time.Now().Unix()),
		PayloadLen: payloadLen,
	}
	resp.WithMajor(header.MajorOKResp).WithSubProto(sub)
	return resp
}

// SendResponse 编码并通过连接发送响应。
func SendResponse(log *slog.Logger, conn core.IConnection, req header.HeaderTcp, payload []byte, sub uint8) {
	codec := header.HeaderTcpCodec{}
	resp := BuildResponse(req, uint32(len(payload)), sub)
	if err := conn.SendWithHeader(&resp, payload, codec); err != nil {
		if log != nil {
			log.Error("发送响应失败", "err", err)
		}
	}
}
