package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

// DispatchOptions 定义 DispatcherProcess 的运行参数。
type DispatchOptions struct {
	Logger         *slog.Logger
	ChannelCount   int
	WorkersPerChan int
	ChannelBuffer  int
	Base           core.IProcess
	Strategy       QueueSelectStrategy
}

type dispatchEvent struct {
	ctx     context.Context
	conn    core.IConnection
	hdr     core.IHeader
	payload []byte
}

// DispatcherProcess 提供基于子协议路由的处理管线，支持多通道+多 worker 并发。
type DispatcherProcess struct {
	log      *slog.Logger
	base     core.IProcess
	handlers map[uint8]core.ISubProcess
	fallback core.ISubProcess

	queues         []chan dispatchEvent
	chanCount      int
	workersPerChan int

	strategy QueueSelectStrategy

	startOnce  sync.Once
	runtimeCtx context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// NewDispatcher 构建 DispatcherProcess。
func NewDispatcher(opts DispatchOptions) (*DispatcherProcess, error) {
	if opts.ChannelCount <= 0 {
		opts.ChannelCount = 1
	}
	if opts.WorkersPerChan <= 0 {
		opts.WorkersPerChan = 1
	}
	if opts.ChannelBuffer < 0 {
		opts.ChannelBuffer = 0
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	queues := make([]chan dispatchEvent, opts.ChannelCount)
	for i := range queues {
		queues[i] = make(chan dispatchEvent, opts.ChannelBuffer)
	}
	if opts.Strategy == nil { // allow future extension in options
		opts.Strategy = ConnHashStrategy{}
	}
	return &DispatcherProcess{
		log:            log,
		base:           opts.Base,
		handlers:       make(map[uint8]core.ISubProcess),
		queues:         queues,
		chanCount:      opts.ChannelCount,
		workersPerChan: opts.WorkersPerChan,
		strategy:       opts.Strategy,
	}, nil
}

// NewDispatcherFromConfig 根据配置创建 DispatcherProcess。
func NewDispatcherFromConfig(cfg core.IConfig, base core.IProcess, logger *slog.Logger) (*DispatcherProcess, error) {
	rawStrategy := ""
	if cfg != nil {
		if v, ok := cfg.Get(coreconfig.KeyProcQueueStrategy); ok {
			rawStrategy = v
		}
	}
	opts := DispatchOptions{
		Logger:         logger,
		Base:           base,
		ChannelCount:   readPositiveInt(cfg, coreconfig.KeyProcChannelCount, 1),
		WorkersPerChan: readPositiveInt(cfg, coreconfig.KeyProcWorkersPerChan, 1),
		ChannelBuffer:  readPositiveInt(cfg, coreconfig.KeyProcChannelBuffer, 64),
		Strategy:       StrategyFromConfig(rawStrategy),
	}
	return NewDispatcher(opts)
}

// RegisterHandler 注册子协议处理器。
func (p *DispatcherProcess) RegisterHandler(h core.ISubProcess) error {
	if h == nil {
		return errors.New("sub process nil")
	}
	sub := h.SubProto()
	if sub > 63 {
		return fmt.Errorf("sub proto %d out of range", sub)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.handlers[sub]; exists {
		return fmt.Errorf("sub proto %d already registered", sub)
	}
	p.handlers[sub] = h
	return nil
}

// RegisterDefaultHandler 注册默认子协议处理器。
func (p *DispatcherProcess) RegisterDefaultHandler(h core.ISubProcess) {
	if h == nil {
		return
	}
	p.mu.Lock()
	p.fallback = h
	p.mu.Unlock()
}

// ensureRuntime 启动 worker 池。
func (p *DispatcherProcess) ensureRuntime(ctx context.Context) {
	p.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		runtimeCtx, cancel := context.WithCancel(ctx)
		p.runtimeCtx = runtimeCtx
		p.cancel = cancel
		for i := range p.queues {
			queue := p.queues[i]
			p.wg.Add(1)
			go func(q chan dispatchEvent) {
				defer p.wg.Done()
				workers := p.workersPerChan
				var wg sync.WaitGroup
				wg.Add(workers)
				for k := 0; k < workers; k++ {
					go func() {
						defer wg.Done()
						for evt := range q {
							p.route(evt)
						}
					}()
				}
				<-runtimeCtx.Done()
				close(q)
				wg.Wait()
			}(queue)
		}
	})
}

type preRouteDecider interface {
	PreRoute(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) bool
}

// preRouteDecider 可选接口：基础流程可实现以在子协议分发前决定是否继续。
// 默认实现直接调用 OnReceive，子类可选择覆盖。
func (p *DispatcherProcess) preRoute(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) bool {
	if p.base == nil {
		return true
	}
	if pr, ok := p.base.(preRouteDecider); ok {
		return pr.PreRoute(ctx, conn, hdr, payload)
	}
	p.base.OnReceive(ctx, conn, hdr, payload)
	return true
}

func (p *DispatcherProcess) selectHandler(hdr core.IHeader) (core.ISubProcess, uint8) {
	sub, ok := extractSubProto(hdr)
	if !ok {
		return p.getFallback(), 0
	}
	h := p.getHandler(sub)
	if h == nil {
		return p.getFallback(), sub
	}
	return h, sub
}

func (p *DispatcherProcess) callHandler(ctx context.Context, handler core.ISubProcess, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if handler == nil {
		return
	}
	// panic 防护，避免单个 handler 崩溃影响整个 worker。
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("handler panic", "recover", r, "subproto", handler.SubProto(), "conn", conn.ID())
		}
	}()
	handler.OnReceive(ctx, conn, hdr, payload)
}

// route 现在仅组合三步。
func (p *DispatcherProcess) route(evt dispatchEvent) {
	handler, sub := p.selectHandler(evt.hdr)
	if handler == nil {
		p.log.Warn("no handler for sub proto", "subproto", sub, "conn", evt.conn.ID())
		return
	}

	if sourceMismatch(handler, evt.conn, evt.hdr) {
		if p.log != nil {
			p.log.Warn("drop frame due to source mismatch", "subproto", sub, "conn", evt.conn.ID(), "hdr_source", evt.hdr.SourceID(), "meta_node", extractNodeID(evt.conn))
		}
		return
	}

	cont := p.preRoute(evt.ctx, evt.conn, evt.hdr, evt.payload)
	if cont {
		p.callHandler(evt.ctx, handler, evt.conn, evt.hdr, evt.payload)
		return
	}
	// preRoute 已处理/转发。若是 Cmd 帧且 handler 声明接受 Cmd，则仍本地处理一次（不影响转发）。
	if shouldInterceptCmd(handler, evt.hdr) {
		p.callHandler(evt.ctx, handler, evt.conn, evt.hdr, evt.payload)
	}
}

func extractSubProto(h core.IHeader) (uint8, bool) {
	if h == nil {
		return 0, false
	}
	return h.SubProto(), true
}

func (p *DispatcherProcess) getHandler(sub uint8) core.ISubProcess {
	p.mu.RLock()
	h := p.handlers[sub]
	p.mu.RUnlock()
	return h
}

func (p *DispatcherProcess) getFallback() core.ISubProcess {
	p.mu.RLock()
	fb := p.fallback
	p.mu.RUnlock()
	return fb
}

func sourceMismatch(h core.ISubProcess, conn core.IConnection, hdr core.IHeader) bool {
	if h == nil || conn == nil || hdr == nil {
		return false
	}
	if opt, ok := h.(SourceCheckOpt); ok && opt.AllowSourceMismatch() {
		return false
	}
	metaNode := extractNodeID(conn)
	// 未绑定 nodeID 视为未登录，拒绝处理（登录类 handler 可通过 AllowSourceMismatch 放行）
	if metaNode == 0 {
		return true
	}
	return hdr.SourceID() != metaNode
}

// CmdInterceptable 可选接口：声明 handler 是否需要在 Cmd 帧目标非本地时仍本地处理一次。
type CmdInterceptable interface {
	AcceptCmd() bool
}

// SourceCheckOpt 可选接口：声明是否允许 SourceID 与连接元数据的 nodeID 不一致。
// 默认不允许，返回 true 表示跳过校验（例如登录协议需要在未绑定 nodeID 前工作）。
type SourceCheckOpt interface {
	AllowSourceMismatch() bool
}

func shouldInterceptCmd(h core.ISubProcess, hdr core.IHeader) bool {
	if h == nil || hdr == nil {
		return false
	}
	if hdr.Major() != header.MajorCmd {
		return false
	}
	if ci, ok := h.(CmdInterceptable); ok {
		return ci.AcceptCmd()
	}
	return false
}

// Shutdown 关闭 worker 池。
func (p *DispatcherProcess) Shutdown() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

// ConfigSnapshot 返回当前通道/worker 配置，便于观测与测试。
func (p *DispatcherProcess) ConfigSnapshot() (channels, workers, buffer int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	channels = len(p.queues)
	workers = p.workersPerChan
	if channels > 0 {
		buffer = cap(p.queues[0])
	}
	return
}

func (p *DispatcherProcess) selectQueue(conn core.IConnection, hdr core.IHeader) int {
	return p.strategy.SelectQueue(conn, hdr, p.chanCount)
}

// OnListen 实现 core.IProcess。
func (p *DispatcherProcess) OnListen(conn core.IConnection) {
	if p.base != nil {
		p.base.OnListen(conn)
	}
}

// OnReceive 将事件写入通道，供 worker 并发处理。
func (p *DispatcherProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.ensureRuntime(ctx)
	idx := p.selectQueue(conn, hdr)
	evt := dispatchEvent{ctx: ctx, conn: conn, hdr: hdr, payload: payload}
	select {
	case p.queues[idx] <- evt:
		// 成功入队
	case <-ctx.Done():
		// 上下文取消
	case <-p.runtimeCtx.Done():
		// runtime 已关闭
	default:
		// 队列已满（非阻塞保护）
		p.log.Warn("process queue full, drop frame", "queue", idx, "conn", conn.ID())
	}
}

func (p *DispatcherProcess) OnSend(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) error {
	if p.base != nil {
		return p.base.OnSend(ctx, conn, hdr, payload)
	}
	return nil
}

func (p *DispatcherProcess) OnClose(conn core.IConnection) {
	if p.base != nil {
		p.base.OnClose(conn)
	}
}

func readPositiveInt(cfg core.IConfig, key string, def int) int {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg.Get(key); ok {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return def
}
