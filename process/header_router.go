package process

// Context: This file provides shared Core framework logic around header_router.

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

// RouteDecisionKind is the router classification result before any forwarding or local decode happens.
type RouteDecisionKind int

const (
	RouteDecisionDrop RouteDecisionKind = iota
	RouteDecisionHopDispatch
	RouteDecisionLocalDispatch
	RouteDecisionFastForward
	RouteDecisionBroadcastChildren
)

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

// RouteDecision is the header-only routing decision.
type RouteDecision struct {
	Kind   RouteDecisionKind
	Reason string
}

// HeaderRouter decides how the current node should treat a frame by looking only at header metadata.
type HeaderRouter struct{}

func NewHeaderRouter() *HeaderRouter {
	return &HeaderRouter{}
}

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
