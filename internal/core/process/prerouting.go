package process

import (
	"context"
	"log/slog"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
)

// PreRoutingProcess 属于核心层：在子协议分发前执行基础路由逻辑。
// 规则：
// 1. target==0 => 广播（仅向子节点下行，来自父节点的广播也只向子节点下行，不回传父节点）
// 2. target!=local => 转发（直接写到目标连接，未命中时上送父节点）
// 3. target==local => 继续 dispatcher 后续子协议处理
// 依赖：连接元数据中存有 nodeID（由登录协议写入），以及 role（parent/child）。
type PreRoutingProcess struct {
	log         *slog.Logger
	cfg         core.IConfig
	forwardMode bool
}

func NewPreRoutingProcess(log *slog.Logger) *PreRoutingProcess {
	if log == nil {
		log = slog.Default()
	}
	return &PreRoutingProcess{log: log, forwardMode: true}
}

func (p *PreRoutingProcess) WithConfig(cfg core.IConfig) *PreRoutingProcess {
	p.cfg = cfg
	if cfg != nil {
		if raw, ok := cfg.Get(coreconfig.KeyRoutingForwardRemote); ok {
			p.forwardMode = core.ParseBool(raw, true)
		}
	}
	return p
}

func (p *PreRoutingProcess) WithForwardMode(enable bool) *PreRoutingProcess {
	p.forwardMode = enable
	return p
}

func (p *PreRoutingProcess) OnListen(conn core.IConnection) {
	p.log.Info("新连接", "id", conn.ID(), "remote", conn.RemoteAddr())
}
func (p *PreRoutingProcess) OnSend(_ context.Context, _ core.IConnection, _ core.IHeader, _ []byte) error {
	return nil
}
func (p *PreRoutingProcess) OnClose(conn core.IConnection) {
	p.log.Info("连接关闭", "id", conn.ID())
}

func (p *PreRoutingProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	// 兼容：直接调用 PreRoute，忽略返回值。
	p.PreRoute(ctx, conn, hdr, payload)
}

// PreRoute 决定是否继续后续子协议处理；返回 false 表示已处理/转发完毕。
func (p *PreRoutingProcess) PreRoute(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) bool {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		p.log.Warn("无法获取 server 上下文，跳过路由")
		return true
	}
	if hdr == nil {
		p.log.Warn("空头部，跳过")
		return true
	}
	target := hdr.TargetID()
	local := srv.NodeID()
	srcIsParent := isParentConn(conn)

	// 广播：只向子节点下行，不向父节点上行。
	if target == 0 {
		p.handleBroadcast(ctx, srv, conn, hdr, payload)
		return false
	}

	// 跨节点转发
	if target != local {
		if !p.forwardMode {
			p.log.Debug("转发功能已禁用，丢弃跨节点消息", "target", target, "local", local)
			return false
		}
		if p.forwardToLocalChild(ctx, srv, hdr, payload, target) {
			return false
		}
		p.forwardToParent(ctx, srv, hdr.Clone(), payload, srcIsParent, target)
		return false
	}
	// 本地：继续 dispatcher 的后续处理
	return true
}

func (p *PreRoutingProcess) forwardOrDrop(sendFn func() error) {
	if !p.forwardMode {
		return
	}
	if err := sendFn(); err != nil {
		p.log.Error("转发失败", "err", err)
	}
}

func (p *PreRoutingProcess) handleBroadcast(ctx context.Context, srv core.IServer, src core.IConnection, hdr core.IHeader, payload []byte) {
	p.log.Info("广播消息", "from", hdr.SourceID(), "subproto", hdr.SubProto())
	baseHdr := hdr.Clone()
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if c.ID() == src.ID() {
			return true
		}
		if isParentConn(c) {
			return true // 不向父节点广播
		}
		if !p.forwardMode {
			return true
		}
		clone := baseHdr.Clone()
		p.forwardOrDrop(func() error {
			return srv.Send(ctx, c.ID(), clone, payload)
		})
		return true
	})
}

func (p *PreRoutingProcess) forwardToLocalChild(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte, target uint32) bool {
	forwardHdr := hdr.Clone()
	if targetConn, ok := srv.ConnManager().GetByNode(target); ok {
		p.forwardOrDrop(func() error {
			return srv.Send(ctx, targetConn.ID(), forwardHdr.Clone(), payload)
		})
		return true
	}
	var forwarded bool
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if nid := extractNodeID(c); nid == target {
			sendHdr := forwardHdr.Clone()
			p.forwardOrDrop(func() error {
				return srv.Send(ctx, c.ID(), sendHdr, payload)
			})
			forwarded = true
			return false
		}
		return true
	})
	return forwarded
}

func (p *PreRoutingProcess) forwardToParent(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte, srcIsParent bool, target uint32) {
	if srcIsParent {
		p.log.Warn("目标节点未找到，来自父节点的消息丢弃", "target", target)
		return
	}
	if !p.forwardMode {
		p.log.Warn("转发已禁用，丢弃未命中路由", "target", target)
		return
	}
	if parent, ok := findParentConn(srv.ConnManager()); ok {
		p.forwardOrDrop(func() error {
			return srv.Send(ctx, parent.ID(), hdr, payload)
		})
		return
	}
	p.log.Warn("目标节点未找到，丢弃", "target", target)
}

func extractServer(ctx context.Context) core.IServer {
	return core.ServerFromContext(ctx)
}

func isParentConn(c core.IConnection) bool {
	if c == nil {
		return false
	}
	if role, ok := c.GetMeta(core.MetaRoleKey); ok {
		if s, ok2 := role.(string); ok2 && s == core.RoleParent {
			return true
		}
	}
	return false
}

func extractNodeID(c core.IConnection) uint32 {
	if c == nil {
		return 0
	}
	if meta, ok := c.GetMeta("nodeID"); ok {
		if nid, ok2 := meta.(uint32); ok2 {
			return nid
		}
	}
	return 0
}

func findParentConn(cm core.IConnectionManager) (core.IConnection, bool) {
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if isParentConn(c) {
			parent = c
			return false
		}
		return true
	})
	return parent, parent != nil
}
