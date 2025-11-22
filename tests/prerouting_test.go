package tests

import (
	"context"
	"net"
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

type stubServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	sends  []sendCall
}

type sendCall struct {
	connID string
	target uint32
}

func (s *stubServer) Start(context.Context) error          { return nil }
func (s *stubServer) Stop(context.Context) error           { return nil }
func (s *stubServer) Config() core.IConfig                 { return config.NewMap(nil) }
func (s *stubServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *stubServer) Process() core.IProcess               { return nil }
func (s *stubServer) HeaderCodec() core.IHeaderCodec       { return nil }
func (s *stubServer) NodeID() uint32                       { return s.nodeID }
func (s *stubServer) Send(_ context.Context, connID string, hdr core.IHeader, _ []byte) error {
	s.sends = append(s.sends, sendCall{connID: connID, target: hdr.TargetID()})
	return nil
}

type stubConn struct {
	id   string
	meta map[string]any
}

func newStubConn(id string) *stubConn {
	return &stubConn{id: id, meta: make(map[string]any)}
}

func (c *stubConn) ID() string { return c.id }
func (c *stubConn) Close() error {
	return nil
}
func (c *stubConn) OnReceive(core.ReceiveHandler) {}
func (c *stubConn) SetMeta(key string, val any)   { c.meta[key] = val }
func (c *stubConn) GetMeta(key string) (any, bool) {
	v, ok := c.meta[key]
	return v, ok
}
func (c *stubConn) Metadata() map[string]any             { return c.meta }
func (c *stubConn) LocalAddr() net.Addr                  { return mockAddr{} }
func (c *stubConn) RemoteAddr() net.Addr                 { return mockAddr{} }
func (c *stubConn) Reader() core.IReader                 { return nil }
func (c *stubConn) SetReader(core.IReader)               {}
func (c *stubConn) DispatchReceive(core.IHeader, []byte) {}
func (c *stubConn) RawConn() net.Conn                    { return nil }
func (c *stubConn) Send([]byte) error                    { return nil }
func (c *stubConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error {
	return nil
}
