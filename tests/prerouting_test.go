package tests

import (
	"context"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/process"
)

func TestPreRouteForwardToParentOnMiss(t *testing.T) {
	cm := connmgr.New()
	parent := newStubConn("parent-1")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	child := newStubConn("child-1")
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child: %v", err)
	}

	srv := &stubServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)
	p := process.NewPreRoutingProcess(nil).WithConfig(config.NewMap(nil))

	hdr := (&header.HeaderTcp{}).WithTargetID(99).WithSourceID(10)
	next := p.PreRoute(ctx, child, hdr, []byte("data"))
	if next {
		t.Fatalf("expected short-circuit after forwarding to parent")
	}
	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 forwarded send, got %d", len(srv.sends))
	}
	if srv.sends[0].connID != parent.ID() {
		t.Fatalf("expected send to parent, got %s", srv.sends[0].connID)
	}
}

func TestPreRouteBroadcastDownstreamOnly(t *testing.T) {
	cm := connmgr.New()
	parent := newStubConn("parent-1")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	child1 := newStubConn("child-1")
	child2 := newStubConn("child-2")
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if err := cm.Add(child1); err != nil {
		t.Fatalf("add child1: %v", err)
	}
	if err := cm.Add(child2); err != nil {
		t.Fatalf("add child2: %v", err)
	}

	srv := &stubServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)
	p := process.NewPreRoutingProcess(nil)

	hdr := (&header.HeaderTcp{}).WithTargetID(0).WithSourceID(1)
	next := p.PreRoute(ctx, parent, hdr, []byte("broadcast"))
	if next {
		t.Fatalf("expected broadcast short-circuit")
	}
	if len(srv.sends) != 2 {
		t.Fatalf("expected broadcast to 2 children, got %d", len(srv.sends))
	}
	for _, call := range srv.sends {
		if call.connID == parent.ID() {
			t.Fatalf("broadcast should not go back to parent")
		}
	}
}
