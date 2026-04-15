package connmgr

// 本文件覆盖 Core 框架中与 `manager` 相关的行为。

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
)

type nopPipe struct{}

func (nopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopPipe) Write(p []byte) (int, error) { return len(p), nil }
func (nopPipe) Close() error                { return nil }

type stubConn struct {
	id     string
	pipe   core.IPipe
	closed atomic.Bool

	mu     sync.RWMutex
	meta   map[string]any
	reader core.IReader
}

func newStubConn(id string) *stubConn {
	return &stubConn{
		id:   id,
		pipe: nopPipe{},
		meta: make(map[string]any),
	}
}

func (c *stubConn) ID() string                    { return c.id }
func (c *stubConn) Pipe() core.IPipe              { return c.pipe }
func (c *stubConn) Close() error                  { c.closed.Store(true); return nil }
func (c *stubConn) OnReceive(core.ReceiveHandler) {}
func (c *stubConn) Send([]byte) error             { return nil }
func (c *stubConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error {
	return nil
}

func (c *stubConn) SetMeta(key string, val any) {
	c.mu.Lock()
	c.meta[key] = val
	c.mu.Unlock()
}

func (c *stubConn) GetMeta(key string) (any, bool) {
	c.mu.RLock()
	v, ok := c.meta[key]
	c.mu.RUnlock()
	return v, ok
}

func (c *stubConn) Metadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]any, len(c.meta))
	for k, v := range c.meta {
		cp[k] = v
	}
	return cp
}

func (c *stubConn) LocalAddr() net.Addr  { return nil }
func (c *stubConn) RemoteAddr() net.Addr { return nil }

func (c *stubConn) Reader() core.IReader {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reader
}

func (c *stubConn) SetReader(r core.IReader) {
	c.mu.Lock()
	c.reader = r
	c.mu.Unlock()
}

func (c *stubConn) DispatchReceive(core.IHeader, []byte) {}

type stubLink struct {
	id     string
	pipe   core.IPipe
	closed atomic.Bool
	mu     sync.RWMutex
	meta   map[string]any
}

func newStubLink(id string) *stubLink {
	return &stubLink{id: id, pipe: nopPipe{}, meta: make(map[string]any)}
}

func (l *stubLink) ID() string       { return l.id }
func (l *stubLink) Pipe() core.IPipe { return l.pipe }
func (l *stubLink) Close() error     { l.closed.Store(true); return nil }
func (l *stubLink) LocalAddr() net.Addr {
	return nil
}
func (l *stubLink) RemoteAddr() net.Addr {
	return nil
}
func (l *stubLink) SetMeta(key string, val any) {
	l.mu.Lock()
	l.meta[key] = val
	l.mu.Unlock()
}
func (l *stubLink) GetMeta(key string) (any, bool) {
	l.mu.RLock()
	v, ok := l.meta[key]
	l.mu.RUnlock()
	return v, ok
}
func (l *stubLink) Metadata() map[string]any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	cp := make(map[string]any, len(l.meta))
	for k, v := range l.meta {
		cp[k] = v
	}
	return cp
}

func TestManager_AddLink_CompatibilityPath(t *testing.T) {
	m := New()
	c1 := newStubConn("c1")
	if err := m.AddLink(c1); err != nil {
		t.Fatalf("AddLink(c1): %v", err)
	}
	got, ok := m.GetLink("c1")
	if !ok || got == nil || got.ID() != "c1" {
		t.Fatalf("GetLink(c1) = ok=%v link=%v", ok, got)
	}
}

func TestManager_AddLink_RejectsNonConnection(t *testing.T) {
	m := New()
	l1 := newStubLink("l1")
	if err := m.AddLink(l1); err == nil {
		t.Fatalf("expected AddLink to reject non-IConnection link")
	}
}

func TestManager_LinkHooks(t *testing.T) {
	m := New()
	c1 := newStubConn("c1")
	var addCount atomic.Int32
	var removeCount atomic.Int32
	m.SetLinkHooks(core.LinkHooks{
		OnAdd: func(link core.ILink) {
			if link == nil || link.ID() != "c1" {
				t.Fatalf("unexpected add link: %#v", link)
			}
			addCount.Add(1)
		},
		OnRemove: func(link core.ILink) {
			if link == nil || link.ID() != "c1" {
				t.Fatalf("unexpected remove link: %#v", link)
			}
			removeCount.Add(1)
		},
	})
	if err := m.Add(c1); err != nil {
		t.Fatalf("Add c1: %v", err)
	}
	if err := m.Remove("c1"); err != nil {
		t.Fatalf("Remove c1: %v", err)
	}
	if addCount.Load() != 1 {
		t.Fatalf("add hook count=%d, want 1", addCount.Load())
	}
	if removeCount.Load() != 1 {
		t.Fatalf("remove hook count=%d, want 1", removeCount.Load())
	}
}

func TestManager_UpdateNodeIndex_DirectConflictClosesOld(t *testing.T) {
	m := New()

	c1 := newStubConn("c1")
	c2 := newStubConn("c2")
	if err := m.Add(c1); err != nil {
		t.Fatalf("Add c1: %v", err)
	}
	if err := m.Add(c2); err != nil {
		t.Fatalf("Add c2: %v", err)
	}

	c1.SetMeta("nodeID", uint32(10))
	c2.SetMeta("nodeID", uint32(10))

	m.UpdateNodeIndex(10, c1)
	m.UpdateNodeIndex(10, c2)

	if !c1.closed.Load() {
		t.Fatalf("expected c1 to be closed")
	}
	if _, ok := m.Get("c1"); ok {
		t.Fatalf("expected c1 removed from manager")
	}
	if _, ok := m.Get("c2"); !ok {
		t.Fatalf("expected c2 still in manager")
	}
	if got, ok := m.GetByNode(10); !ok || got == nil || got.ID() != "c2" {
		t.Fatalf("expected node 10 maps to c2, got ok=%v conn=%v", ok, got)
	}
}

func TestManager_UpdateNodeIndex_DescendantOverwriteDoesNotCloseOld(t *testing.T) {
	m := New()

	old := newStubConn("old")
	newc := newStubConn("new")
	if err := m.Add(old); err != nil {
		t.Fatalf("Add old: %v", err)
	}
	if err := m.Add(newc); err != nil {
		t.Fatalf("Add new: %v", err)
	}

	old.SetMeta("nodeID", uint32(100))
	newc.SetMeta("nodeID", uint32(200))

	m.UpdateNodeIndex(30, old)
	m.UpdateNodeIndex(30, newc)

	if old.closed.Load() {
		t.Fatalf("expected old not to be closed")
	}
	if _, ok := m.Get("old"); !ok {
		t.Fatalf("expected old still in manager")
	}
	if got, ok := m.GetByNode(30); !ok || got == nil || got.ID() != "new" {
		t.Fatalf("expected node 30 maps to new, got ok=%v conn=%v", ok, got)
	}
}

func TestStubTypes_Compile(t *testing.T) {
	var _ core.ILink = (*stubConn)(nil)
	var _ core.IConnection = (*stubConn)(nil)
	var _ core.ILink = (*stubLink)(nil)
	var _ core.ILinkManager = (*Manager)(nil)
	_ = context.Background()
}
