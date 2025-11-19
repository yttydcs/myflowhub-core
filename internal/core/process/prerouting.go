package process

import (
	"context"
	"log/slog"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
)

// PreRoutingProcess 属于核心层：在子协议分发前执行基础路由逻辑。
// 规则：
// 1. target==0 => 广播（当前实现不回发给源连接，可调整）。
// 2. target!=local => 转发（直接写到目标连接，不进入子协议 handler）。
// 3. target==local => 继续 dispatcher 后续子协议处理。
// 依赖：连接元数据中存在 nodeID（由登录协议写入）。

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
	srv := extractServer(ctx)
	if srv == nil {
		p.log.Warn("无法获取 server 上下文，跳过路由")
		return
	}
	if hdr == nil {
		p.log.Warn("空头部，跳过")
		return
	}
	target := hdr.TargetID()
	local := srv.NodeID()

	// 广播
	if target == 0 {
		p.log.Info("广播消息", "from", hdr.SourceID(), "subproto", hdr.SubProto())
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if c.ID() != conn.ID() {
				if !p.forwardMode {
					return true
				}
				p.forwardOrDrop(func() error {
					return c.SendWithHeader(hdr, payload, srv.HeaderCodec())
				})
			}
			return true
		})
		return
	}

	if target != local {
		if !p.forwardMode {
			p.log.Debug("转发功能已禁用，丢弃跨节点消息", "target", target, "local", local)
			return
		}
		p.log.Info("转发消息", "from", hdr.SourceID(), "to", target, "subproto", hdr.SubProto())
		var forwarded bool
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if meta, ok := c.GetMeta("nodeID"); ok {
				if nid, ok2 := meta.(uint32); ok2 && nid == target {
					p.forwardOrDrop(func() error {
						return c.SendWithHeader(hdr, payload, srv.HeaderCodec())
					})
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

func (p *PreRoutingProcess) forwardOrDrop(sendFn func() error) {
	if !p.forwardMode {
		return
	}
	if err := sendFn(); err != nil {
		p.log.Error("转发失败", "err", err)
	}
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
