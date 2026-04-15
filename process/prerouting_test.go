package process

// 本文件覆盖 Core 框架中与 `prerouting` 相关的行为。

import (
	"context"
	"io"
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
)

type prerouteStubServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	sends  []prerouteSendCall
	bus    eventbus.IBus
}

type prerouteSendCall struct {
	connID   string
	targetID uint32
	hopLimit uint8
	major    uint8
}

func newPrerouteStubServer(nodeID uint32, cm core.IConnectionManager) *prerouteStubServer {
	return &prerouteStubServer{nodeID: nodeID, cm: cm}
}

func (s *prerouteStubServer) Start(context.Context) error          { return nil }
func (s *prerouteStubServer) Stop(context.Context) error           { return nil }
func (s *prerouteStubServer) Config() core.IConfig                 { return config.NewMap(nil) }
func (s *prerouteStubServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *prerouteStubServer) Process() core.IProcess               { return nil }
func (s *prerouteStubServer) HeaderCodec() core.IHeaderCodec       { return header.HeaderTcpCodec{} }
func (s *prerouteStubServer) NodeID() uint32                       { return s.nodeID }
func (s *prerouteStubServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *prerouteStubServer) EventBus() eventbus.IBus {
	if s.bus == nil {
		s.bus = eventbus.New(eventbus.Options{})
	}
	return s.bus
}
func (s *prerouteStubServer) Send(_ context.Context, connID string, hdr core.IHeader, _ []byte) error {
	s.sends = append(s.sends, prerouteSendCall{
		connID:   connID,
		targetID: hdr.TargetID(),
		hopLimit: hdr.GetHopLimit(),
		major:    hdr.Major(),
	})
	return nil
}

type prerouteStubConn struct {
	id   string
	meta map[string]any
}

func newPrerouteStubConn(id string) *prerouteStubConn {
	return &prerouteStubConn{id: id, meta: make(map[string]any)}
}

func (c *prerouteStubConn) ID() string                    { return c.id }
func (c *prerouteStubConn) Pipe() core.IPipe              { return prerouteNopPipe{} }
func (c *prerouteStubConn) Close() error                  { return nil }
func (c *prerouteStubConn) OnReceive(core.ReceiveHandler) {}
func (c *prerouteStubConn) SetMeta(key string, val any)   { c.meta[key] = val }
func (c *prerouteStubConn) GetMeta(key string) (any, bool) {
	v, ok := c.meta[key]
	return v, ok
}
func (c *prerouteStubConn) Metadata() map[string]any             { return c.meta }
func (c *prerouteStubConn) LocalAddr() net.Addr                  { return prerouteStubAddr("local") }
func (c *prerouteStubConn) RemoteAddr() net.Addr                 { return prerouteStubAddr("remote") }
func (c *prerouteStubConn) Reader() core.IReader                 { return nil }
func (c *prerouteStubConn) SetReader(core.IReader)               {}
func (c *prerouteStubConn) DispatchReceive(core.IHeader, []byte) {}
func (c *prerouteStubConn) Send([]byte) error                    { return nil }
func (c *prerouteStubConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error {
	return nil
}

type prerouteNopPipe struct{}

func (prerouteNopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (prerouteNopPipe) Write(p []byte) (int, error) { return len(p), nil }
func (prerouteNopPipe) Close() error                { return nil }

type prerouteStubAddr string

func (a prerouteStubAddr) Network() string { return string(a) }
func (a prerouteStubAddr) String() string  { return string(a) }

func TestPreRouteDropsSourceZeroNonAuth(t *testing.T) {
	proc := NewPreRoutingProcess(nil)
	cm := connmgr.New()
	srv := newPrerouteStubServer(7, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	ingress := newPrerouteStubConn("ingress")
	if err := cm.Add(ingress); err != nil {
		t.Fatalf("Add(ingress): %v", err)
	}

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(3).
		WithSourceID(0).
		WithTargetID(7)

	if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); got {
		t.Fatalf("PreRoute()=%v, want false", got)
	}
	if len(srv.sends) != 0 {
		t.Fatalf("unexpected sends: %+v", srv.sends)
	}
}

func TestPreRouteAllowsAuthWithSourceZero(t *testing.T) {
	proc := NewPreRoutingProcess(nil)
	cm := connmgr.New()
	srv := newPrerouteStubServer(7, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	ingress := newPrerouteStubConn("ingress")
	if err := cm.Add(ingress); err != nil {
		t.Fatalf("Add(ingress): %v", err)
	}

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(2).
		WithSourceID(0).
		WithTargetID(7)

	if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); !got {
		t.Fatalf("PreRoute()=%v, want true", got)
	}
	if len(srv.sends) != 0 {
		t.Fatalf("unexpected sends: %+v", srv.sends)
	}
}

func TestPreRouteKeepsMajorCmdHopVisible(t *testing.T) {
	proc := NewPreRoutingProcess(nil)
	cm := connmgr.New()
	srv := newPrerouteStubServer(7, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	ingress := newPrerouteStubConn("ingress")
	if err := cm.Add(ingress); err != nil {
		t.Fatalf("Add(ingress): %v", err)
	}

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(5).
		WithSourceID(11).
		WithTargetID(999)

	if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); !got {
		t.Fatalf("PreRoute()=%v, want true", got)
	}
	if len(srv.sends) != 0 {
		t.Fatalf("unexpected sends: %+v", srv.sends)
	}
}

func TestPreRouteBroadcastChildrenOnly(t *testing.T) {
	proc := NewPreRoutingProcess(nil)
	cm := connmgr.New()
	srv := newPrerouteStubServer(7, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	ingress := newPrerouteStubConn("child-ingress")
	ingress.SetMeta(core.MetaRoleKey, core.RoleChild)
	if err := cm.Add(ingress); err != nil {
		t.Fatalf("Add(ingress): %v", err)
	}

	child := newPrerouteStubConn("child-target")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(9))
	if err := cm.Add(child); err != nil {
		t.Fatalf("Add(child): %v", err)
	}

	parent := newPrerouteStubConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("Add(parent): %v", err)
	}

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(5).
		WithSourceID(11).
		WithTargetID(0).
		WithHopLimit(3)

	if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); got {
		t.Fatalf("PreRoute()=%v, want false", got)
	}
	if len(srv.sends) != 1 {
		t.Fatalf("send count=%d, want 1", len(srv.sends))
	}
	if srv.sends[0].connID != child.ID() {
		t.Fatalf("broadcast sent to %q, want %q", srv.sends[0].connID, child.ID())
	}
	if srv.sends[0].hopLimit != 2 {
		t.Fatalf("broadcast hop_limit=%d, want 2", srv.sends[0].hopLimit)
	}
}

func TestPreRouteFastForwardPrefersDirectChildThenParent(t *testing.T) {
	proc := NewPreRoutingProcess(nil)

	t.Run("direct child route", func(t *testing.T) {
		cm := connmgr.New()
		srv := newPrerouteStubServer(7, cm)
		ctx := core.WithServerContext(context.Background(), srv)

		ingress := newPrerouteStubConn("parent-ingress")
		ingress.SetMeta(core.MetaRoleKey, core.RoleParent)
		if err := cm.Add(ingress); err != nil {
			t.Fatalf("Add(ingress): %v", err)
		}

		targetChild := newPrerouteStubConn("child-8")
		targetChild.SetMeta(core.MetaRoleKey, core.RoleChild)
		targetChild.SetMeta("nodeID", uint32(8))
		if err := cm.Add(targetChild); err != nil {
			t.Fatalf("Add(targetChild): %v", err)
		}

		hdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorOKResp).
			WithSubProto(5).
			WithSourceID(11).
			WithTargetID(8).
			WithHopLimit(4)

		if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); got {
			t.Fatalf("PreRoute()=%v, want false", got)
		}
		if len(srv.sends) != 1 {
			t.Fatalf("send count=%d, want 1", len(srv.sends))
		}
		if srv.sends[0].connID != targetChild.ID() {
			t.Fatalf("sent to %q, want %q", srv.sends[0].connID, targetChild.ID())
		}
		if srv.sends[0].hopLimit != 3 {
			t.Fatalf("forward hop_limit=%d, want 3", srv.sends[0].hopLimit)
		}
	})

	t.Run("fallback parent route", func(t *testing.T) {
		cm := connmgr.New()
		srv := newPrerouteStubServer(7, cm)
		ctx := core.WithServerContext(context.Background(), srv)

		ingress := newPrerouteStubConn("child-ingress")
		ingress.SetMeta(core.MetaRoleKey, core.RoleChild)
		if err := cm.Add(ingress); err != nil {
			t.Fatalf("Add(ingress): %v", err)
		}

		parent := newPrerouteStubConn("parent")
		parent.SetMeta(core.MetaRoleKey, core.RoleParent)
		if err := cm.Add(parent); err != nil {
			t.Fatalf("Add(parent): %v", err)
		}

		hdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorErrResp).
			WithSubProto(5).
			WithSourceID(11).
			WithTargetID(88).
			WithHopLimit(5)

		if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); got {
			t.Fatalf("PreRoute()=%v, want false", got)
		}
		if len(srv.sends) != 1 {
			t.Fatalf("send count=%d, want 1", len(srv.sends))
		}
		if srv.sends[0].connID != parent.ID() {
			t.Fatalf("sent to %q, want %q", srv.sends[0].connID, parent.ID())
		}
		if srv.sends[0].hopLimit != 4 {
			t.Fatalf("forward hop_limit=%d, want 4", srv.sends[0].hopLimit)
		}
	})

	t.Run("do not send back to parent ingress", func(t *testing.T) {
		cm := connmgr.New()
		srv := newPrerouteStubServer(7, cm)
		ctx := core.WithServerContext(context.Background(), srv)

		ingress := newPrerouteStubConn("parent")
		ingress.SetMeta(core.MetaRoleKey, core.RoleParent)
		if err := cm.Add(ingress); err != nil {
			t.Fatalf("Add(ingress): %v", err)
		}

		hdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorMsg).
			WithSubProto(5).
			WithSourceID(11).
			WithTargetID(88).
			WithHopLimit(2)

		if got := proc.PreRoute(ctx, ingress, hdr, []byte("payload")); got {
			t.Fatalf("PreRoute()=%v, want false", got)
		}
		if len(srv.sends) != 0 {
			t.Fatalf("unexpected sends: %+v", srv.sends)
		}
	})
}
