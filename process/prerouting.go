package process

// Context: This file provides shared Core framework logic around prerouting.

import (
	"context"
	"log/slog"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

// PreRoutingProcess performs fast header-only routing before sub-protocol dispatch.
type PreRoutingProcess struct {
	log         *slog.Logger
	cfg         core.IConfig
	forwardMode bool
	router      *HeaderRouter
}

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
	p.log.Info("new connection", "id", conn.ID(), "remote", conn.RemoteAddr())
}

func (p *PreRoutingProcess) OnSend(_ context.Context, _ core.IConnection, _ core.IHeader, _ []byte) error {
	return nil
}

func (p *PreRoutingProcess) OnClose(conn core.IConnection) {
	p.log.Info("connection closed", "id", conn.ID())
}

func (p *PreRoutingProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	p.PreRoute(ctx, conn, hdr, payload)
}

// PreRoute returns true when the frame should continue into dispatcher/handler processing.
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

func (p *PreRoutingProcess) forwardOrDrop(sendFn func() error) {
	if !p.forwardMode {
		return
	}
	if err := sendFn(); err != nil {
		p.log.Error("forward failed", "err", err)
	}
}

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
