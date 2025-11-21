package handler

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
)

// DefaultForwardHandler 丢弃未知子协议，或按配置转发到指定节点。
type DefaultForwardHandler struct {
	log        *slog.Logger
	forward    bool
	subTargets map[uint8]uint32
}

func NewDefaultForwardHandler(cfg core.IConfig, log *slog.Logger) *DefaultForwardHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &DefaultForwardHandler{log: log, subTargets: make(map[uint8]uint32)}
	if cfg != nil {
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardEnable); ok {
			h.forward = core.ParseBool(raw, false)
		}
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardTarget); ok {
			if id, err := parseUint32(raw); err == nil {
				h.subTargets[0] = id
			}
		}
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardMap); ok {
			h.loadMap(raw)
		}
	}
	return h
}

func (h *DefaultForwardHandler) loadMap(raw string) {
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		subID, err1 := strconv.ParseUint(strings.TrimSpace(kv[0]), 10, 8)
		nodeID, err2 := parseUint32(kv[1])
		if err1 != nil || err2 != nil {
			continue
		}
		h.subTargets[uint8(subID)] = nodeID
	}
}

func (h *DefaultForwardHandler) SubProto() uint8 { return 0 }

func (h *DefaultForwardHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	if !h.forward {
		h.log.Debug("unknown subproto dropped", "subproto", hdr.SubProto(), "conn", conn.ID())
		return
	}
	targetNode := h.resolveTarget(hdr.SubProto())
	if targetNode == 0 {
		h.log.Debug("no default route for subproto", "subproto", hdr.SubProto())
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		h.log.Warn("no server context, cannot forward", "conn", conn.ID())
		return
	}
	targetHeader := CloneWithTarget(hdr, targetNode)
	if targetHeader == nil {
		h.log.Warn("cannot clone header for forwarding")
		return
	}
	targetHeader.WithSourceID(srv.NodeID())
	h.forwardToNode(ctx, srv, targetHeader, payload)
}

func (h *DefaultForwardHandler) resolveTarget(sub uint8) uint32 {
	if target, ok := h.subTargets[sub]; ok {
		return target
	}
	return h.subTargets[0]
}

func (h *DefaultForwardHandler) forwardToNode(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte) {
	cm := srv.ConnManager()
	if conn, ok := cm.GetByNode(hdr.TargetID()); ok {
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
			h.log.Error("default forward failed", "err", err, "target", hdr.TargetID())
		}
		return
	}
	// fallback scan
	var forwarded bool
	cm.Range(func(conn core.IConnection) bool {
		if nodeID, ok := conn.GetMeta("nodeID"); ok {
			if nid, ok2 := nodeID.(uint32); ok2 && nid == hdr.TargetID() {
				forwarded = true
				if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
					h.log.Error("default forward failed", "err", err, "target", hdr.TargetID())
				}
				return false
			}
		}
		return true
	})
	if !forwarded {
		h.log.Warn("default target not found", "target", hdr.TargetID())
	}
}

func parseUint32(v string) (uint32, error) {
	val, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
	return uint32(val), err
}
