package process

import (
	"context"
	"log/slog"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

// PreRoutingProcess 属于核心层：在子协议分发前执行基础路由逻辑。
// 规则：
// 1. target==0 => 广播（当前实现不回发给源连接，可调整）。
// 2. target!=local => 转发（直接写到目标连接，不进入子协议 handler）。
// 3. target==local => 继续 dispatcher 后续子协议处理。
// 依赖：连接元数据中存在 nodeID（由登录协议写入）。

type PreRoutingProcess struct{ log *slog.Logger }

func NewPreRoutingProcess(log *slog.Logger) *PreRoutingProcess {
	if log == nil {
		log = slog.Default()
	}
	return &PreRoutingProcess{log: log}
}

func (p *PreRoutingProcess) OnListen(conn core.IConnection) {
	p.log.Info("新连接", "id", conn.ID(), "remote", conn.RemoteAddr())
}
func (p *PreRoutingProcess) OnSend(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) error {
	return nil
}
func (p *PreRoutingProcess) OnClose(conn core.IConnection) {
	p.log.Info("连接关闭", "id", conn.ID())
}

func (p *PreRoutingProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) {
	val := ctx.Value(struct{ S string }{"server"})
	srv, _ := val.(core.IServer)
	if srv == nil {
		p.log.Warn("无法获取 server 上下文，跳过路由")
		return
	}
	hreq, ok := toHeaderTcp(hdr)
	if !ok {
		p.log.Warn("无法转换 HeaderTcp，跳过路由")
		return
	}
	target := hreq.Target
	local := srv.NodeID()

	// 广播
	if target == 0 {
		p.log.Info("广播消息", "from", hreq.Source, "subproto", hreq.SubProto())
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if c.ID() != conn.ID() {
				_ = c.SendWithHeader(hreq, payload, srv.HeaderCodec())
			}
			return true
		})
		return
	}

	// 转发
	if target != local {
		p.log.Info("转发消息", "from", hreq.Source, "to", target, "subproto", hreq.SubProto())
		var forwarded bool
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if meta, ok := c.GetMeta("nodeID"); ok {
				if nid, ok2 := meta.(uint32); ok2 && nid == target {
					_ = c.SendWithHeader(hreq, payload, srv.HeaderCodec())
					forwarded = true
					return false
				}
			}
			return true
		})
		if !forwarded {
			p.log.Warn("目标节点未找到，丢弃", "target", target)
		}
		return
	}
	// 本地：继续 dispatcher 的后续处理。
}

func toHeaderTcp(h header.IHeader) (header.HeaderTcp, bool) {
	switch v := h.(type) {
	case header.HeaderTcp:
		return v, true
	case *header.HeaderTcp:
		if v != nil {
			return *v, true
		}
	}
	return header.HeaderTcp{}, false
}
