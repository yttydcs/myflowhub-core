package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/listener/tcp_listener"
	"github.com/yttydcs/myflowhub-core/process"
	"github.com/yttydcs/myflowhub-core/reader"
)

// ReaderFactory 创建 IReader。
type ReaderFactory func(conn core.IConnection) core.IReader

// Options 配置 Server。
type Options struct {
	Name          string
	Logger        *slog.Logger
	Process       core.IProcess
	Codec         core.IHeaderCodec
	Listener      core.IListener
	Config        core.IConfig
	Manager       core.IConnectionManager
	ReaderFactory ReaderFactory
	NodeID        uint32 // 可选：节点 ID，缺省为 1
}

type parentConfig struct {
	enable    bool
	addr      string
	reconnect time.Duration
}

type parentState struct {
	parentConfig
	mu     sync.Mutex
	connID string
	down   chan struct{}
}

func (p *parentState) hasParent() bool {
	return p != nil && p.enable && p.addr != ""
}

func (p *parentState) setConn(id string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connID = id
	p.down = make(chan struct{})
	return p.down
}

func (p *parentState) notifyDown(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != "" && id == p.connID && p.down != nil {
		close(p.down)
		p.down = nil
		p.connID = ""
	}
}

// Server 是 IServer 的具体实现，负责协调 listener/manager/process。
type Server struct {
	opts   Options
	log    *slog.Logger
	cm     core.IConnectionManager
	proc   core.IProcess
	codec  core.IHeaderCodec
	cfg    core.IConfig
	lst    core.IListener
	rFac   ReaderFactory
	nodeID atomic.Uint32
	sender *process.SendDispatcher

	parent *parentState

	eb eventbus.IBus

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	start  bool
}

// New 构建 Server。
func New(opts Options) (*Server, error) {
	if opts.Listener == nil {
		return nil, errors.New("listener required")
	}
	if opts.Manager == nil {
		return nil, errors.New("manager required")
	}
	if opts.Codec == nil {
		return nil, errors.New("codec required")
	}
	if opts.Process == nil {
		return nil, errors.New("process required")
	}
	if opts.Config == nil {
		return nil, errors.New("config required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ReaderFactory == nil {
		opts.ReaderFactory = func(core.IConnection) core.IReader {
			return reader.NewTCP(opts.Logger)
		}
	}
	if opts.NodeID == 0 {
		opts.NodeID = 1
	}
	// 初始化发送调度器（使用同一配置来源）
	var sendDisp *process.SendDispatcher
	if sd, err := process.NewSendDispatcherFromConfig(opts.Config, opts.Logger); err == nil {
		sendDisp = sd
	} else {
		return nil, err
	}
	parent := buildParentState(opts.Config)
	s := &Server{
		opts:   opts,
		log:    opts.Logger,
		cm:     opts.Manager,
		proc:   opts.Process,
		codec:  opts.Codec,
		cfg:    opts.Config,
		lst:    opts.Listener,
		rFac:   opts.ReaderFactory,
		sender: sendDisp,
		parent: parent,
		eb:     eventbus.New(eventbus.Options{}),
	}
	s.nodeID.Store(opts.NodeID)
	return s, nil
}

// Start 启动监听与连接循环。
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start {
		return errors.New("server already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.ctx = core.WithServerContext(s.ctx, s)
	onAdd := func(c core.IConnection) {
		if _, ok := c.GetMeta(core.MetaRoleKey); !ok {
			c.SetMeta(core.MetaRoleKey, core.RoleChild)
		}
		c.OnReceive(func(c core.IConnection, hdr core.IHeader, payload []byte) {
			ctx2 := core.WithServerContext(s.ctx, s)
			s.proc.OnReceive(ctx2, c, hdr, payload)
		})
		s.proc.OnListen(c)
		s.wg.Add(1)
		go s.serveConn(c)
	}
	s.cm.SetHooks(core.ConnectionHooks{OnAdd: onAdd, OnRemove: func(c core.IConnection) {
		if s.sender != nil {
			s.sender.CloseConn(c.ID())
		}
		s.proc.OnClose(c)
		if s.parent != nil {
			s.parent.notifyDown(c.ID())
		}
		if s.eb != nil {
			_ = s.eb.Publish(core.WithServerContext(s.ctx, s), "conn.closed", map[string]any{
				"conn_id": c.ID(),
				"node_id": extractConnNodeID(c),
			}, nil)
		}
	}})
	s.start = true
	if s.parent.hasParent() {
		go s.runParentLink(s.ctx)
	}
	go func() {
		if err := s.lst.Listen(s.ctx, s.cm); err != nil {
			s.log.Error("listener exited", "err", err)
			_ = s.Stop(context.Background())
		}
	}()
	return nil
}

func (s *Server) serveConn(conn core.IConnection) {
	defer s.wg.Done()
	r := conn.Reader()
	if r == nil {
		r = s.rFac(conn)
		conn.SetReader(r)
	}
	if r == nil {
		s.log.Error("no reader available", "conn", conn.ID())
		return
	}
	if err := r.ReadLoop(s.ctx, conn, s.codec); err != nil {
		s.log.Warn("read loop exit", "conn", conn.ID(), "err", err)
	}
	if err := s.cm.Remove(conn.ID()); err != nil {
		s.log.Debug("remove conn", "conn", conn.ID(), "err", err)
	}
}

// Stop 停止服务并释放资源。
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.start {
		s.mu.Unlock()
		return nil
	}
	s.start = false
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if d, ok := s.proc.(interface{ Shutdown() }); ok {
		d.Shutdown()
	}
	if s.sender != nil {
		s.sender.Shutdown()
	}
	_ = s.lst.Close()
	if s.eb != nil {
		s.eb.Close()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.cm.CloseAll()
}

func (s *Server) Config() core.IConfig                 { return s.cfg }
func (s *Server) ConnManager() core.IConnectionManager { return s.cm }
func (s *Server) Process() core.IProcess               { return s.proc }
func (s *Server) HeaderCodec() core.IHeaderCodec       { return s.codec }
func (s *Server) NodeID() uint32                       { return s.nodeID.Load() }
func (s *Server) EventBus() eventbus.IBus              { return s.eb }
func (s *Server) UpdateNodeID(id uint32) {
	if id == 0 {
		return
	}
	s.nodeID.Store(id)
}
func (s *Server) Send(ctx context.Context, connID string, hdr core.IHeader, payload []byte) error {
	conn, ok := s.cm.Get(connID)
	if !ok {
		return errors.New("conn not found")
	}
	if err := s.proc.OnSend(ctx, conn, hdr, payload); err != nil {
		return err
	}
	if s.sender == nil {
		return conn.SendWithHeader(hdr, payload, s.codec)
	}
	return s.sender.Dispatch(ctx, conn, hdr, payload, s.codec, nil)
}

// Broadcast 通过发送调度器广播一帧（不触发 OnSend 钩子对每个连接重复调用，仅一次校验）。
func (s *Server) Broadcast(ctx context.Context, hdr core.IHeader, payload []byte) error {
	if s.sender == nil {
		return s.cm.Broadcast(payload) // 回退：原始 payload（假设已编码）
	}
	var firstErr error
	s.cm.Range(func(c core.IConnection) bool {
		// 不为每个连接重复调用 OnSend，假设 hdr/payload 已审计
		if err := s.sender.Dispatch(ctx, c, hdr, payload, s.codec, func(e error) {
			if e != nil && firstErr == nil {
				firstErr = e
			}
		}); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}

func (s *Server) runParentLink(ctx context.Context) {
	if s.parent == nil || !s.parent.hasParent() {
		return
	}
	retry := s.parent.reconnect
	if retry <= 0 {
		retry = 3 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		raw, err := net.Dial("tcp", s.parent.addr)
		if err != nil {
			s.log.Warn("dial parent failed", "addr", s.parent.addr, "err", err)
			time.Sleep(retry)
			continue
		}
		conn := tcp_listener.NewTCPConnection(raw)
		conn.SetMeta(core.MetaRoleKey, core.RoleParent)
		if err := s.cm.Add(conn); err != nil {
			s.log.Warn("add parent connection failed", "addr", s.parent.addr, "err", err)
			_ = raw.Close()
			time.Sleep(retry)
			continue
		}
		down := s.parent.setConn(conn.ID())
		s.log.Info("parent connected", "addr", s.parent.addr, "conn", conn.ID())
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return
		case <-down:
			s.log.Warn("parent connection closed, retrying", "addr", s.parent.addr)
			time.Sleep(retry)
		}
	}
}

func extractConnNodeID(c core.IConnection) uint32 {
	if c == nil {
		return 0
	}
	if v, ok := c.GetMeta("nodeID"); ok {
		switch vv := v.(type) {
		case uint32:
			return vv
		case uint64:
			return uint32(vv)
		case int:
			if vv >= 0 {
				return uint32(vv)
			}
		case int64:
			if vv >= 0 {
				return uint32(vv)
			}
		}
	}
	return 0
}

func buildParentState(cfg core.IConfig) *parentState {
	p := &parentState{
		parentConfig: parentConfig{
			enable:    false,
			addr:      "",
			reconnect: 3 * time.Second,
		},
	}
	if cfg == nil {
		return p
	}
	if raw, ok := cfg.Get(coreconfig.KeyParentEnable); ok {
		p.enable = core.ParseBool(raw, false)
	}
	if raw, ok := cfg.Get(coreconfig.KeyParentAddr); ok {
		p.addr = raw
	}
	if raw, ok := cfg.Get(coreconfig.KeyParentReconnectSec); ok {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			p.reconnect = time.Duration(v) * time.Second
		}
	}
	return p
}
