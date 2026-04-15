package quic_listener

// 本文件承载 Core 框架中与 `connection` 相关的通用逻辑。

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	quic "github.com/quic-go/quic-go"
	core "github.com/yttydcs/myflowhub-core"
)

type quicPipe struct {
	conn   *quic.Conn
	stream *quic.Stream

	closeOnce sync.Once
}

func (p *quicPipe) Read(b []byte) (int, error) {
	return p.stream.Read(b)
}

func (p *quicPipe) Write(b []byte) (int, error) {
	return p.stream.Write(b)
}

// Close 同时关闭 stream 与底层 quic 连接，避免只关单边留下会话泄漏。
func (p *quicPipe) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if p.stream != nil {
			_ = p.stream.Close()
			p.stream.CancelRead(0)
			p.stream.CancelWrite(0)
		}
		if p.conn != nil {
			closeErr = p.conn.CloseWithError(0, "closed")
		}
	})
	return closeErr
}

type quicConnection struct {
	pipe   core.IPipe
	id     string
	local  net.Addr
	remote net.Addr

	mu     sync.RWMutex
	meta   map[string]any
	recvH  core.ReceiveHandler
	reader core.IReader
}

var quicConnSeq atomic.Uint64

// NewQUICConnection 为 QUIC 承载分配稳定 ID，并挂上地址与元数据存储。
func NewQUICConnection(pipe core.IPipe, local, remote net.Addr) (*quicConnection, error) {
	if pipe == nil {
		return nil, errors.New("nil pipe")
	}
	id := fmt.Sprintf("quic#%d", quicConnSeq.Add(1))
	return &quicConnection{
		pipe:   pipe,
		id:     id,
		local:  local,
		remote: remote,
		meta:   make(map[string]any),
	}, nil
}

var _ core.IConnection = (*quicConnection)(nil)
var _ core.ISender = (*quicConnection)(nil)

func (c *quicConnection) ID() string { return c.id }

func (c *quicConnection) Pipe() core.IPipe { return c.pipe }

func (c *quicConnection) Close() error { return c.pipe.Close() }

func (c *quicConnection) OnReceive(h core.ReceiveHandler) {
	c.mu.Lock()
	c.recvH = h
	c.mu.Unlock()
}

func (c *quicConnection) SetMeta(key string, val any) {
	c.mu.Lock()
	c.meta[key] = val
	c.mu.Unlock()
}

func (c *quicConnection) GetMeta(key string) (any, bool) {
	c.mu.RLock()
	v, ok := c.meta[key]
	c.mu.RUnlock()
	return v, ok
}

func (c *quicConnection) Metadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]any, len(c.meta))
	for k, v := range c.meta {
		cp[k] = v
	}
	return cp
}

func (c *quicConnection) LocalAddr() net.Addr  { return c.local }
func (c *quicConnection) RemoteAddr() net.Addr { return c.remote }

func (c *quicConnection) Reader() core.IReader {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reader
}

func (c *quicConnection) SetReader(r core.IReader) {
	c.mu.Lock()
	c.reader = r
	c.mu.Unlock()
}

// DispatchReceive 把解码后的帧交给当前连接绑定的 receive handler。
func (c *quicConnection) DispatchReceive(h core.IHeader, payload []byte) {
	c.mu.RLock()
	recv := c.recvH
	c.mu.RUnlock()
	if recv != nil {
		recv(c, h, payload)
	}
}

// Send 直接把原始字节完整写入 QUIC stream。
func (c *quicConnection) Send(data []byte) error {
	return core.WriteAll(c.pipe, data)
}

// SendWithHeader 先编码 header/payload，再通过 QUIC stream 输出整帧。
func (c *quicConnection) SendWithHeader(hdr core.IHeader, payload []byte, codec core.IHeaderCodec) error {
	if codec == nil {
		return io.ErrNoProgress
	}
	frame, err := codec.Encode(hdr, payload)
	if err != nil {
		return err
	}
	return core.WriteAll(c.pipe, frame)
}
