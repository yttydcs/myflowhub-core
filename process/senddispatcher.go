package process

// 本文件承载 Core 框架中与 `senddispatcher` 相关的通用逻辑。

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

var (
	errNilConn          = errors.New("nil connection")
	errNilCodec         = errors.New("nil codec")
	errNilPipe          = errors.New("nil pipe")
	errWriterClosed     = errors.New("writer closed")
	errEnqueueTimeout   = errors.New("enqueue timeout")
	errDispatcherClosed = errors.New("dispatcher closed")
)

// SendOptions 定义发送调度器的并发与排队参数。
type SendOptions struct {
	Logger         *slog.Logger
	ChannelCount   int
	WorkersPerChan int
	ChannelBuffer  int
	ConnBuffer     int           // 单连接发送队列长度。
	EnqueueTimeout time.Duration // 分片队列与单连接队列共用的入队超时。
	EncodeInWriter bool          // 是否在单连接 writer goroutine 内完成编码。
}

type sendTask struct {
	ctx     context.Context
	conn    core.IConnection
	hdr     core.IHeader
	payload []byte
	codec   core.IHeaderCodec
	cb      func(error)
}

type connWriter struct {
	conn           core.IConnection
	ch             chan sendTask
	log            *slog.Logger
	encodeInWriter bool
	enqueueTimeout time.Duration

	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex
	wg        sync.WaitGroup
}

// start 启动单连接 writer 的后台循环，持续串行消费该连接上的发送任务。
func (w *connWriter) start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for task := range w.ch {
			err := w.write(task)
			if task.cb != nil {
				task.cb(err)
			}
		}
	}()
}

// write 在单连接串行 writer 中真正落盘，确保同一连接上的帧不会并发交错。
func (w *connWriter) write(task sendTask) error {
	if task.codec == nil {
		return errNilCodec
	}
	pipe := w.conn.Pipe()
	if pipe == nil {
		return errNilPipe
	}

	if w.encodeInWriter {
		return WriteFrame(pipe, task.codec, core.Frame{Header: task.hdr, Payload: task.payload})
	}
	// 非编码模式下认为 payload 已经是最终线上的字节序列。
	return core.WriteAll(pipe, task.payload)
}

// enqueue 把发送任务放进该连接私有队列，并在关闭或超时时尽快失败返回。
func (w *connWriter) enqueue(task sendTask) (err error) {
	w.mu.RLock()
	closed := w.closed
	w.mu.RUnlock()
	if closed {
		return errWriterClosed
	}
	defer func() {
		if r := recover(); r != nil {
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
			err = errWriterClosed
		}
	}()
	if w.enqueueTimeout <= 0 {
		w.ch <- task
		return nil
	}
	timer := time.NewTimer(w.enqueueTimeout)
	defer timer.Stop()
	select {
	case w.ch <- task:
		return nil
	case <-timer.C:
		return errEnqueueTimeout
	}
}

// stop 幂等关闭单连接 writer，并等待已经入队的任务消费完成。
func (w *connWriter) stop() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.ch)
		w.mu.Unlock()
	})
	w.wg.Wait()
}

// SendDispatcher 把全局发送请求分流到“按连接串行”的 writer，兼顾并发与单连接有序。
type SendDispatcher struct {
	log            *slog.Logger
	shards         []chan sendTask
	shardCount     int
	workersPerChan int
	connBuffer     int
	enqueueTimeout time.Duration
	encodeInWriter bool

	startOnce    sync.Once
	shutdownOnce sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	mu      sync.RWMutex
	writers map[string]*connWriter
}

// NewSendDispatcher 根据发送并发参数构建调度器，并补齐最小安全默认值。
func NewSendDispatcher(opts SendOptions) (*SendDispatcher, error) {
	if opts.ChannelCount <= 0 {
		opts.ChannelCount = 1
	}
	if opts.WorkersPerChan <= 0 {
		opts.WorkersPerChan = 1
	}
	if opts.ChannelBuffer < 0 {
		opts.ChannelBuffer = 0
	}
	if opts.ConnBuffer <= 0 {
		opts.ConnBuffer = 64
	}
	if opts.EnqueueTimeout < 0 {
		opts.EnqueueTimeout = 0
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if !opts.EncodeInWriter {
		opts.EncodeInWriter = true
	}
	shards := make([]chan sendTask, opts.ChannelCount)
	for i := range shards {
		shards[i] = make(chan sendTask, opts.ChannelBuffer)
	}
	return &SendDispatcher{
		log:            opts.Logger,
		shards:         shards,
		shardCount:     opts.ChannelCount,
		workersPerChan: opts.WorkersPerChan,
		connBuffer:     opts.ConnBuffer,
		enqueueTimeout: opts.EnqueueTimeout,
		encodeInWriter: opts.EncodeInWriter,
		writers:        make(map[string]*connWriter),
	}, nil
}

// NewSendDispatcherFromConfig 从配置读取发送并发参数，供 Server 统一装配。
func NewSendDispatcherFromConfig(cfg core.IConfig, logger *slog.Logger) (*SendDispatcher, error) {
	opts := SendOptions{
		Logger:         logger,
		ChannelCount:   readPositiveInt(cfg, coreconfig.KeySendChannelCount, 1),
		WorkersPerChan: readPositiveInt(cfg, coreconfig.KeySendWorkersPerChan, 1),
		ChannelBuffer:  readPositiveInt(cfg, coreconfig.KeySendChannelBuffer, 64),
		ConnBuffer:     readPositiveInt(cfg, coreconfig.KeySendConnBuffer, 64),
		EnqueueTimeout: readDurationMs(cfg, coreconfig.KeySendEnqueueTimeoutMS, 100),
		EncodeInWriter: true,
	}
	return NewSendDispatcher(opts)
}

// ensureStarted 延迟启动分片 worker 和清理协程，避免未使用时提前占用 goroutine。
func (d *SendDispatcher) ensureStarted(ctx context.Context) {
	d.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		d.ctx, d.cancel = context.WithCancel(ctx)
		for i := range d.shards {
			q := d.shards[i]
			d.wg.Add(1)
			go func(ch <-chan sendTask) {
				defer d.wg.Done()
				for task := range ch {
					if task.conn == nil {
						d.log.Warn("nil conn in send task")
						continue
					}
					writer := d.getOrCreateWriter(task.conn)
					if writer == nil {
						if task.cb != nil {
							task.cb(errWriterClosed)
						}
						continue
					}
					err := writer.enqueue(task)
					if err != nil && task.cb != nil {
						task.cb(err)
					}
				}
			}(q)
		}
		go func() {
			<-d.ctx.Done()
			for _, q := range d.shards {
				close(q)
			}
			d.mu.Lock()
			for _, w := range d.writers {
				w.stop()
			}
			d.writers = make(map[string]*connWriter)
			d.mu.Unlock()
		}()
	})
}

// Dispatch 把发送任务投递到分片队列，再由分片 worker 转交给具体连接 writer。
func (d *SendDispatcher) Dispatch(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, codec core.IHeaderCodec, cb func(error)) error {
	if conn == nil {
		return errNilConn
	}
	d.ensureStarted(ctx)
	idx := d.selectQueue(conn, hdr)
	task := sendTask{ctx: ctx, conn: conn, hdr: hdr, payload: payload, codec: codec, cb: cb}
	if d.enqueueTimeout <= 0 {
		select {
		case d.shards[idx] <- task:
			return nil
		case <-d.ctx.Done():
			return errDispatcherClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timer := time.NewTimer(d.enqueueTimeout)
	defer timer.Stop()
	select {
	case d.shards[idx] <- task:
		return nil
	case <-timer.C:
		return errEnqueueTimeout
	case <-d.ctx.Done():
		return errDispatcherClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// selectQueue 用连接 ID 做稳定分片，尽量让同一连接的 writer 查找命中同一 shard。
func (d *SendDispatcher) selectQueue(conn core.IConnection, hdr core.IHeader) int {
	if d.shardCount == 1 {
		return 0
	}
	if conn != nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(conn.ID()))
		return int(h.Sum32() % uint32(d.shardCount))
	}
	if hdr != nil {
		return int(hdr.SubProto()) % d.shardCount
	}
	return 0
}

// getOrCreateWriter 惰性创建连接专属 writer，避免为短生命周期连接预先分配资源。
func (d *SendDispatcher) getOrCreateWriter(conn core.IConnection) *connWriter {
	id := conn.ID()
	d.mu.RLock()
	if w, ok := d.writers[id]; ok {
		d.mu.RUnlock()
		return w
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	if w, ok := d.writers[id]; ok {
		return w
	}
	w := &connWriter{
		conn:           conn,
		ch:             make(chan sendTask, d.connBuffer),
		log:            d.log,
		encodeInWriter: d.encodeInWriter,
		enqueueTimeout: d.enqueueTimeout,
	}
	w.start()
	d.writers[id] = w
	return w
}

// CloseConn 在连接移除时同步清理其 writer，避免遗留队列继续写向失效 pipe。
func (d *SendDispatcher) CloseConn(connID string) {
	d.mu.Lock()
	w, ok := d.writers[connID]
	if ok {
		delete(d.writers, connID)
	}
	d.mu.Unlock()
	if ok {
		w.stop()
	}
}

// Shutdown 关闭全部分片和连接 writer，并等待后台 goroutine 退出。
func (d *SendDispatcher) Shutdown() {
	d.shutdownOnce.Do(func() {
		if d.cancel != nil {
			d.cancel()
		}
		d.wg.Wait()
	})
}

// Snapshot 返回当前调度器的并发配置，便于测试和运行时观测。
func (d *SendDispatcher) Snapshot() (channels, workers, buffer int) {
	channels = len(d.shards)
	workers = d.workersPerChan
	if channels > 0 {
		buffer = cap(d.shards[0])
	}
	return
}

// String 输出调度器关键参数，便于日志或调试时快速识别配置。
func (d *SendDispatcher) String() string {
	ch, w, b := d.Snapshot()
	return fmt.Sprintf("SendDispatcher{channels=%d workers=%d buffer=%d connBuffer=%d enqueueTimeout=%s}", ch, w, b, d.connBuffer, d.enqueueTimeout)
}

// readDurationMs 把毫秒配置解析为 time.Duration，非法值统一退回默认值。
func readDurationMs(cfg core.IConfig, key string, def int) time.Duration {
	if cfg == nil {
		return time.Duration(def) * time.Millisecond
	}
	if raw, ok := cfg.Get(key); ok {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return time.Duration(v) * time.Millisecond
		}
	}
	return time.Duration(def) * time.Millisecond
}
