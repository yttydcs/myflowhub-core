package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
)

// LoginHandler (SubProto=2) 负责分配节点 ID；自增且不重复。
// 登录后将分配的 nodeID 写入连接元数据，供路由阶段使用。
var globalNodeID atomic.Uint32

func init() { globalNodeID.Store(2) } // 1 留给默认 server

type LoginHandler struct{ log *slog.Logger }

func NewLoginHandler(log *slog.Logger) *LoginHandler {
	if log == nil {
		log = slog.Default()
	}
	return &LoginHandler{log: log}
}

func (h *LoginHandler) SubProto() uint8 { return 2 }

func (h *LoginHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	id := globalNodeID.Add(1) - 1
	conn.SetMeta("nodeID", id)
	if srv := extractServer(ctx); srv != nil {
		if cm, ok := srv.ConnManager().(interface {
			UpdateNodeIndex(uint32, core.IConnection)
		}); ok {
			cm.UpdateNodeIndex(id, conn)
		}
	}
	respObj := map[string]uint32{"id": id}
	data, _ := json.Marshal(respObj)
	req := CloneRequest(hdr)
	SendResponse(ctx, h.log, conn, req, data, h.SubProto())
}
