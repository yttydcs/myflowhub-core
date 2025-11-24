package tests

import (
	"context"
	"net"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
)

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
func (s *stubServer) UpdateNodeID(id uint32)               { s.nodeID = id }
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
