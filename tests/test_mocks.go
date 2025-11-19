package tests

import (
	"net"

	core "MyFlowHub-Core/internal/core"
)

type mockAddr struct{}

func (mockAddr) Network() string { return "tcp" }
func (mockAddr) String() string  { return "127.0.0.1:0" }

// mockConnection is a lightweight implementation of core.IConnection used in multiple tests.
type mockConnection struct {
	id string
}

var _ core.IConnection = (*mockConnection)(nil)

func (m *mockConnection) ID() string                                                   { return m.id }
func (m *mockConnection) Close() error                                                 { return nil }
func (m *mockConnection) OnReceive(core.ReceiveHandler)                                {}
func (m *mockConnection) SetMeta(string, any)                                          {}
func (m *mockConnection) GetMeta(string) (any, bool)                                   { return nil, false }
func (m *mockConnection) Metadata() map[string]any                                     { return nil }
func (m *mockConnection) LocalAddr() net.Addr                                          { return mockAddr{} }
func (m *mockConnection) RemoteAddr() net.Addr                                         { return mockAddr{} }
func (m *mockConnection) Reader() core.IReader                                         { return nil }
func (m *mockConnection) SetReader(core.IReader)                                       {}
func (m *mockConnection) DispatchReceive(core.IHeader, []byte)                         {}
func (m *mockConnection) RawConn() net.Conn                                            { return nil }
func (m *mockConnection) Send([]byte) error                                            { return nil }
func (m *mockConnection) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error { return nil }
