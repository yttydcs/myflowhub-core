package rfcomm_listener

// 本文件承载 Core 框架中与 `connection` 相关的通用逻辑。

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	core "github.com/yttydcs/myflowhub-core"
)

type rfcommConnection struct {
	pipe   core.IPipe
	id     string
	local  net.Addr
	remote net.Addr

	mu     sync.RWMutex
	meta   map[string]any
	recvH  core.ReceiveHandler
	reader core.IReader
}

var rfcommConnSeq atomic.Uint64

// NewRFCOMMConnection 为 RFCOMM pipe 分配框架连接 ID，并初始化元数据容器。
func NewRFCOMMConnection(pipe core.IPipe, local, remote net.Addr) (*rfcommConnection, error) {
	if pipe == nil {
		return nil, errors.New("nil pipe")
	}
	id := fmt.Sprintf("rfcomm#%d", rfcommConnSeq.Add(1))
	return &rfcommConnection{
		pipe:   pipe,
		id:     id,
		local:  local,
		remote: remote,
		meta:   make(map[string]any),
	}, nil
}

// Compile-time assertions.
var _ core.IConnection = (*rfcommConnection)(nil)
var _ core.ISender = (*rfcommConnection)(nil)

func (c *rfcommConnection) ID() string { return c.id }

func (c *rfcommConnection) Pipe() core.IPipe { return c.pipe }

func (c *rfcommConnection) Close() error { return c.pipe.Close() }

func (c *rfcommConnection) OnReceive(h core.ReceiveHandler) { c.mu.Lock(); c.recvH = h; c.mu.Unlock() }

func (c *rfcommConnection) SetMeta(key string, val any) {
	c.mu.Lock()
	c.meta[key] = val
	c.mu.Unlock()
}

func (c *rfcommConnection) GetMeta(key string) (any, bool) {
	c.mu.RLock()
	v, ok := c.meta[key]
	c.mu.RUnlock()
	return v, ok
}

func (c *rfcommConnection) Metadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]any, len(c.meta))
	for k, v := range c.meta {
		cp[k] = v
	}
	return cp
}

func (c *rfcommConnection) LocalAddr() net.Addr  { return c.local }
func (c *rfcommConnection) RemoteAddr() net.Addr { return c.remote }

func (c *rfcommConnection) Reader() core.IReader     { c.mu.RLock(); defer c.mu.RUnlock(); return c.reader }
func (c *rfcommConnection) SetReader(r core.IReader) { c.mu.Lock(); c.reader = r; c.mu.Unlock() }

// DispatchReceive 把读取器解码出的帧投递给连接绑定的 receive handler。
func (c *rfcommConnection) DispatchReceive(h core.IHeader, payload []byte) {
	c.mu.RLock()
	recv := c.recvH
	c.mu.RUnlock()
	if recv != nil {
		recv(c, h, payload)
	}
}

// Send 直接把原始字节完整写入 RFCOMM pipe。
func (c *rfcommConnection) Send(data []byte) error {
	return core.WriteAll(c.pipe, data)
}

// SendWithHeader 先编码整帧，再经 RFCOMM pipe 发出。
func (c *rfcommConnection) SendWithHeader(hdr core.IHeader, payload []byte, codec core.IHeaderCodec) error {
	if codec == nil {
		return io.ErrNoProgress
	}
	frame, err := codec.Encode(hdr, payload)
	if err != nil {
		return err
	}
	return core.WriteAll(c.pipe, frame)
}
