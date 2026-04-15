package process

// 本文件承载 Core 框架中与 `header_router` 相关的通用逻辑。

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

// RouteDecisionKind 表示只看 header 时得到的路由分类结果。
type RouteDecisionKind int

const (
	RouteDecisionDrop RouteDecisionKind = iota
	RouteDecisionHopDispatch
	RouteDecisionLocalDispatch
	RouteDecisionFastForward
	RouteDecisionBroadcastChildren
)

// String 返回决策类型的稳定字符串，便于日志和调试输出。
func (k RouteDecisionKind) String() string {
	switch k {
	case RouteDecisionDrop:
		return "drop"
	case RouteDecisionHopDispatch:
		return "hop_dispatch"
	case RouteDecisionLocalDispatch:
		return "local_dispatch"
	case RouteDecisionFastForward:
		return "fast_forward"
	case RouteDecisionBroadcastChildren:
		return "broadcast_children"
	default:
		return "unknown"
	}
}

// RouteDecision 表示仅依据 header 做出的路由判定结果。
type RouteDecision struct {
	Kind   RouteDecisionKind
	Reason string
}

// HeaderRouter 只依赖 header 元数据决定当前节点该如何处置一帧数据。
type HeaderRouter struct{}

// NewHeaderRouter 创建一个仅依赖 header 元数据做快速判定的路由器。
func NewHeaderRouter() *HeaderRouter {
	return &HeaderRouter{}
}

// Decide 在不解析 payload 的前提下给出本节点应丢弃、本地处理还是继续转发的决策。
func (r *HeaderRouter) Decide(localNodeID uint32, ingress core.ILink, hdr core.IHeader) RouteDecision {
	_ = ingress
	if hdr == nil {
		return RouteDecision{Kind: RouteDecisionDrop, Reason: "nil_header"}
	}
	if hdr.SourceID() == 0 && hdr.SubProto() != 2 {
		return RouteDecision{Kind: RouteDecisionDrop, Reason: "source_zero_non_auth"}
	}
	if hdr.SubProto() == 2 {
		return RouteDecision{Kind: RouteDecisionHopDispatch, Reason: "auth_subproto"}
	}
	if hdr.Major() == header.MajorCmd {
		return RouteDecision{Kind: RouteDecisionHopDispatch, Reason: "major_cmd"}
	}
	if hdr.TargetID() == 0 {
		return RouteDecision{Kind: RouteDecisionBroadcastChildren, Reason: "broadcast_children"}
	}
	if hdr.TargetID() != localNodeID {
		return RouteDecision{Kind: RouteDecisionFastForward, Reason: "remote_target"}
	}
	return RouteDecision{Kind: RouteDecisionLocalDispatch, Reason: "local_target"}
}
