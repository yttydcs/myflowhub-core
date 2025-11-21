package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/reader"
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
	nodeID uint32
	sender *process.SendDispatcher

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
	return &Server{
		opts:   opts,
		log:    opts.Logger,
		cm:     opts.Manager,
		proc:   opts.Process,
		codec:  opts.Codec,
		cfg:    opts.Config,
		lst:    opts.Listener,
		rFac:   opts.ReaderFactory,
		nodeID: opts.NodeID,
		sender: sendDisp,
	}, nil
}

// Start 启动监听与连接循环。
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start {
		return errors.New("server already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	// 使用匿名 key 避免额外依赖
	s.ctx = context.WithValue(s.ctx, struct{ S string }{"server"}, s)
	onAdd := func(c core.IConnection) {
		c.OnReceive(func(c core.IConnection, hdr core.IHeader, payload []byte) {
			ctx2 := context.WithValue(ctx, struct{ S string }{"server"}, s)
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
	}})
	s.start = true
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
func (s *Server) NodeID() uint32                       { return s.nodeID }
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
