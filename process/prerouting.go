package process

// 本文件承载 Core 框架中与 `prerouting` 相关的通用逻辑。

import (
	"context"
	"log/slog"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

// PreRoutingProcess 在进入子协议 handler 前做一次仅基于 header 的快速路由。
type PreRoutingProcess struct {
	log         *slog.Logger
	cfg         core.IConfig
	forwardMode bool
	router      *HeaderRouter
}

// NewPreRoutingProcess 创建预路由流程，并默认开启远端转发能力。
func NewPreRoutingProcess(log *slog.Logger) *PreRoutingProcess {
	if log == nil {
		log = slog.Default()
	}
	return &PreRoutingProcess{
		log:         log,
		forwardMode: true,
		router:      NewHeaderRouter(),
	}
}

// WithConfig 绑定运行时配置，并同步读取是否允许转发远端目标帧。
func (p *PreRoutingProcess) WithConfig(cfg core.IConfig) *PreRoutingProcess {
	p.cfg = cfg
	if cfg != nil {
		if raw, ok := cfg.Get(coreconfig.KeyRoutingForwardRemote); ok {
			p.forwardMode = core.ParseBool(raw, true)
		}
	}
	return p
}

// WithForwardMode 允许调用方显式覆盖默认转发开关，便于测试或极简节点裁剪。
func (p *PreRoutingProcess) WithForwardMode(enable bool) *PreRoutingProcess {
	p.forwardMode = enable
	return p
}

// OnListen 记录新连接进入预路由层，便于定位后续转发链路。
func (p *PreRoutingProcess) OnListen(conn core.IConnection) {
	p.log.Info("new connection", "id", conn.ID(), "remote", conn.RemoteAddr())
}

// OnSend 预路由层不改写发送逻辑，只占位满足 IProcess 接口。
func (p *PreRoutingProcess) OnSend(_ context.Context, _ core.IConnection, _ core.IHeader, _ []byte) error {
	return nil
}

// OnClose 记录连接离开，方便把转发异常与生命周期事件关联起来。
func (p *PreRoutingProcess) OnClose(conn core.IConnection) {
	p.log.Info("connection closed", "id", conn.ID())
}

// OnReceive 兼容 IProcess 入口，内部直接复用 PreRoute 的判定逻辑。
func (p *PreRoutingProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	p.PreRoute(ctx, conn, hdr, payload)
}

// PreRoute 返回 true 表示帧仍需进入本地 dispatcher；返回 false 表示已在此层丢弃或转发。
func (p *PreRoutingProcess) PreRoute(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) bool {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		p.log.Warn("server context missing, skip preroute")
		return true
	}
	if hdr == nil {
		p.log.Warn("nil header, skip preroute")
		return true
	}

	decision := p.router.Decide(srv.NodeID(), conn, hdr)
	switch decision.Kind {
	case RouteDecisionDrop:
		p.log.Debug("drop frame in preroute", "reason", decision.Reason, "subproto", hdr.SubProto(), "source", hdr.SourceID())
		return false
	case RouteDecisionHopDispatch, RouteDecisionLocalDispatch:
		return true
	case RouteDecisionBroadcastChildren:
		fwdHdr, ok := p.cloneForForward(hdr)
		if !ok {
			p.log.Warn("drop broadcast frame: hop_limit exhausted", "subproto", hdr.SubProto(), "source", hdr.SourceID())
			return false
		}
		p.handleBroadcast(ctx, srv, conn, fwdHdr, payload)
		return false
	case RouteDecisionFastForward:
		target := hdr.TargetID()
		local := srv.NodeID()
		if !p.forwardMode {
			p.log.Debug("forwarding disabled, drop remote-target frame", "target", target, "local", local)
			return false
		}
		fwdHdr, ok := p.cloneForForward(hdr)
		if !ok {
			p.log.Warn("drop forwarded frame: hop_limit exhausted", "target", target, "local", local, "subproto", hdr.SubProto(), "source", hdr.SourceID())
			return false
		}
		srcIsParent := isParentConn(conn)
		if p.forwardToLocalChild(ctx, srv, fwdHdr, payload, target) {
			return false
		}
		p.forwardToParent(ctx, srv, fwdHdr, payload, srcIsParent, target)
		return false
	default:
		return true
	}
}

// forwardOrDrop 统一包裹实际发送动作，把“关闭转发”与“发送失败记日志”收敛到一处。
func (p *PreRoutingProcess) forwardOrDrop(sendFn func() error) {
	if !p.forwardMode {
		return
	}
	if err := sendFn(); err != nil {
		p.log.Error("forward failed", "err", err)
	}
}

// handleBroadcast 把广播帧复制给本地子连接，但显式跳过来源连接和父连接，避免回环。
func (p *PreRoutingProcess) handleBroadcast(ctx context.Context, srv core.IServer, src core.IConnection, hdr core.IHeader, payload []byte) {
	p.log.Info("broadcast frame", "from", hdr.SourceID(), "subproto", hdr.SubProto())
	baseHdr := hdr
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if c.ID() == src.ID() {
			return true
		}
		if isParentConn(c) {
			return true
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

// forwardToLocalChild 优先命中本地直连或已索引的后代节点，把远端目标就地消化在当前节点。
func (p *PreRoutingProcess) forwardToLocalChild(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte, target uint32) bool {
	if targetConn, ok := srv.ConnManager().GetByNode(target); ok {
		p.forwardOrDrop(func() error {
			return srv.Send(ctx, targetConn.ID(), hdr.Clone(), payload)
		})
		return true
	}
	var forwarded bool
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if nid := extractNodeID(c); nid == target {
			sendHdr := hdr.Clone()
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

// forwardToParent 在本地找不到目标时把帧继续上送父节点，但会阻止“父节点来的包再回父节点”。
func (p *PreRoutingProcess) forwardToParent(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte, srcIsParent bool, target uint32) {
	if srcIsParent {
		p.log.Warn("drop frame from parent: target not found", "target", target)
		return
	}
	if !p.forwardMode {
		p.log.Warn("forwarding disabled, drop unroutable frame", "target", target)
		return
	}
	if parent, ok := findParentConn(srv.ConnManager()); ok {
		p.forwardOrDrop(func() error {
			return srv.Send(ctx, parent.ID(), hdr, payload)
		})
		return
	}
	p.log.Warn("drop frame: target not found", "target", target)
}

// cloneForForward 克隆并递减 hop_limit，确保每次跨节点转发都会消耗一跳。
func (p *PreRoutingProcess) cloneForForward(hdr core.IHeader) (core.IHeader, bool) {
	if hdr == nil {
		return nil, false
	}
	clone := hdr.Clone()
	hop := clone.GetHopLimit()
	if hop == 0 {
		hop = header.DefaultHopLimit
	}
	if hop <= 1 {
		return nil, false
	}
	clone.WithHopLimit(hop - 1)
	return clone, true
}

// isParentConn 通过连接元数据识别父链路，供转发与来源校验共用。
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

// extractNodeID 从连接元数据提取已登录节点号，未绑定时返回 0。
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

// findParentConn 在线程安全的连接管理器里查找唯一父连接。
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
