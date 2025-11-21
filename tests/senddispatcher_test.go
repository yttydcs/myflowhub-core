package tests

import (
	"context"
	"net"
	"testing"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/process"
)

// pipeConn adapts net.Conn to core.IConnection for send dispatcher tests.
type pipeConn struct {
	id   string
	conn net.Conn
}

func (p *pipeConn) ID() string                                                   { return p.id }
func (p *pipeConn) Close() error                                                 { return p.conn.Close() }
func (p *pipeConn) OnReceive(core.ReceiveHandler)                                {}
func (p *pipeConn) SetMeta(string, any)                                          {}
func (p *pipeConn) GetMeta(string) (any, bool)                                   { return nil, false }
func (p *pipeConn) Metadata() map[string]any                                     { return nil }
func (p *pipeConn) LocalAddr() net.Addr                                          { return p.conn.LocalAddr() }
func (p *pipeConn) RemoteAddr() net.Addr                                         { return p.conn.RemoteAddr() }
func (p *pipeConn) Reader() core.IReader                                         { return nil }
func (p *pipeConn) SetReader(core.IReader)                                       {}
func (p *pipeConn) DispatchReceive(core.IHeader, []byte)                         {}
func (p *pipeConn) RawConn() net.Conn                                            { return p.conn }
func (p *pipeConn) Send([]byte) error                                            { return nil }
func (p *pipeConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error { return nil }

var _ core.IConnection = (*pipeConn)(nil)

func TestSendDispatcherSequentialPerConnection(t *testing.T) {
	sd, err := process.NewSendDispatcher(process.SendOptions{
		ChannelCount:   2,
		WorkersPerChan: 2,
		ChannelBuffer:  4,
		ConnBuffer:     8,
		EnqueueTimeout: 200 * time.Millisecond,
		EncodeInWriter: true,
	})
	if err != nil {
		t.Fatalf("new send dispatcher: %v", err)
	}
	defer sd.Shutdown()

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := &pipeConn{id: "c1", conn: client}
	codec := header.HeaderTcpCodec{}
	payloads := []string{"one", "two", "three"}

	ctx := context.Background()
	for i, p := range payloads {
		hdr := &header.HeaderTcp{
			MsgID:      uint32(i + 1),
			Source:     1,
			Target:     2,
			Timestamp:  uint32(time.Now().Unix()),
			PayloadLen: uint32(len(p)),
		}
		hdr.WithMajor(header.MajorMsg).WithSubProto(1)
		if err := sd.Dispatch(ctx, conn, hdr, []byte(p), codec, nil); err != nil {
			t.Fatalf("dispatch #%d failed: %v", i, err)
		}
	}

	dec := header.HeaderTcpCodec{}
	for i, want := range payloads {
		h, data, err := dec.Decode(server)
		if err != nil {
			t.Fatalf("decode #%d failed: %v", i, err)
		}
		gotHdr, ok := h.(*header.HeaderTcp)
		if !ok {
			t.Fatalf("unexpected header type: %T", h)
		}
		if gotHdr.MsgID != uint32(i+1) {
			t.Fatalf("msg id order wrong: got %d want %d", gotHdr.MsgID, i+1)
		}
		if string(data) != want {
			t.Fatalf("payload mismatch: got %q want %q", string(data), want)
		}
	}
}
