package tcp_listener

import (
	"fmt"
	"io"
	"net"
	"sync"

	core "MyFlowHub-Core/internal/core"
)

// tcpConnection 是针对 TCP 的 IConnection 实现。
type tcpConnection struct {
	conn   net.Conn
	id     string
	mu     sync.RWMutex
	meta   map[string]any
	recvH  core.ReceiveHandler
	reader core.IReader
}

func newTCPConnection(c net.Conn) *tcpConnection {
	return &tcpConnection{
		conn: c,
		id:   fmt.Sprintf("%s->%s", c.LocalAddr().String(), c.RemoteAddr().String()),
		meta: make(map[string]any),
	}
}

// 编译期断言实现接口
var _ core.IConnection = (*tcpConnection)(nil)
var _ core.ISender = (*tcpConnection)(nil)

func (c *tcpConnection) ID() string { return c.id }

func (c *tcpConnection) Close() error { return c.conn.Close() }

func (c *tcpConnection) OnReceive(h core.ReceiveHandler) { c.mu.Lock(); c.recvH = h; c.mu.Unlock() }

func (c *tcpConnection) SetMeta(key string, val any) { c.mu.Lock(); c.meta[key] = val; c.mu.Unlock() }

func (c *tcpConnection) GetMeta(key string) (any, bool) {
	c.mu.RLock()
	v, ok := c.meta[key]
	c.mu.RUnlock()
	return v, ok
}

func (c *tcpConnection) Metadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]any, len(c.meta))
	for k, v := range c.meta {
		cp[k] = v
	}
	return cp
}

func (c *tcpConnection) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *tcpConnection) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (c *tcpConnection) Reader() core.IReader     { c.mu.RLock(); defer c.mu.RUnlock(); return c.reader }
func (c *tcpConnection) SetReader(r core.IReader) { c.mu.Lock(); c.reader = r; c.mu.Unlock() }

func (c *tcpConnection) DispatchReceive(h core.IHeader, payload []byte) {
	c.mu.RLock()
	recv := c.recvH
	c.mu.RUnlock()
	if recv != nil {
		recv(c, h, payload)
	}
}

func (c *tcpConnection) RawConn() net.Conn { return c.conn }

func (c *tcpConnection) Send(data []byte) error {
	_, err := c.conn.Write(data)
	return err
}

func (c *tcpConnection) SendWithHeader(hdr core.IHeader, payload []byte, codec core.IHeaderCodec) error {
	if codec == nil {
		return io.ErrNoProgress // 表示未提供 codec
	}
	frame, err := codec.Encode(hdr, payload)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(frame)
	return err
}
