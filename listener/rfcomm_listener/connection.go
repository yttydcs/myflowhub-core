package rfcomm_listener

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

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

func NewRFCOMMConnection(pipe core.IPipe, local, remote net.Addr) (*rfcommConnection, error) {
	if pipe == nil {
		return nil, errors.New("nil pipe")
	}
	id := ""
	if local != nil && remote != nil && local.String() != "" && remote.String() != "" {
		id = fmt.Sprintf("%s->%s", local.String(), remote.String())
	} else if remote != nil && remote.String() != "" {
		id = fmt.Sprintf("->%s", remote.String())
	}
	if id == "" {
		seq := rfcommConnSeq.Add(1)
		id = fmt.Sprintf("rfcomm#%d@%d", seq, time.Now().UnixNano())
	}
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

func (c *rfcommConnection) SetMeta(key string, val any) { c.mu.Lock(); c.meta[key] = val; c.mu.Unlock() }

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

func (c *rfcommConnection) DispatchReceive(h core.IHeader, payload []byte) {
	c.mu.RLock()
	recv := c.recvH
	c.mu.RUnlock()
	if recv != nil {
		recv(c, h, payload)
	}
}

func (c *rfcommConnection) Send(data []byte) error {
	_, err := c.pipe.Write(data)
	return err
}

func (c *rfcommConnection) SendWithHeader(hdr core.IHeader, payload []byte, codec core.IHeaderCodec) error {
	if codec == nil {
		return io.ErrNoProgress
	}
	frame, err := codec.Encode(hdr, payload)
	if err != nil {
		return err
	}
	_, err = c.pipe.Write(frame)
	return err
}

