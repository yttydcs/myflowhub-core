package process

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/header"
)

// SendOptions 定义发送调度参数。
type SendOptions struct {
	Logger         *slog.Logger
	ChannelCount   int
	WorkersPerChan int
	ChannelBuffer  int
}

type sendTask struct {
	ctx     context.Context
	conn    core.IConnection
	hdr     header.IHeader
	payload []byte
	codec   core.IHeaderCodec
	cb      func(error)
}

// SendDispatcher 将发送请求放入按连接/子协议哈希的通道，由多个 worker 并发执行。
type SendDispatcher struct {
	log            *slog.Logger
	queues         []chan sendTask
	chanCount      int
	workersPerChan int
	wg             sync.WaitGroup
	startOnce      sync.Once
	shutdownOnce   sync.Once
	ctx            context.Context
	cancel         context.CancelFunc
}

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
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	queues := make([]chan sendTask, opts.ChannelCount)
	for i := range queues {
		queues[i] = make(chan sendTask, opts.ChannelBuffer)
	}
	return &SendDispatcher{
		log:            opts.Logger,
		queues:         queues,
		chanCount:      opts.ChannelCount,
		workersPerChan: opts.WorkersPerChan,
	}, nil
}

// NewSendDispatcherFromConfig 从配置构建。
func NewSendDispatcherFromConfig(cfg core.IConfig, logger *slog.Logger) (*SendDispatcher, error) {
	opts := SendOptions{
		Logger:         logger,
		ChannelCount:   readPositiveInt(cfg, coreconfig.KeySendChannelCount, 1),
		WorkersPerChan: readPositiveInt(cfg, coreconfig.KeySendWorkersPerChan, 1),
		ChannelBuffer:  readPositiveInt(cfg, coreconfig.KeySendChannelBuffer, 64),
	}
	return NewSendDispatcher(opts)
}

func (d *SendDispatcher) ensureStarted(ctx context.Context) {
	d.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		d.ctx, d.cancel = context.WithCancel(ctx)
		for i := range d.queues {
			q := d.queues[i]
			d.wg.Add(1)
			go func(ch <-chan sendTask) {
				defer d.wg.Done()
				for task := range ch {
					// 调用发送
					if task.conn == nil {
						d.log.Warn("nil conn in send task")
						continue
					}
					var err error
					if task.codec != nil {
						err = task.conn.SendWithHeader(task.hdr, task.payload, task.codec)
					} else {
						// 假设 payload 已编码
						err = task.conn.Send(task.payload)
					}
					if task.cb != nil {
						task.cb(err)
					}
				}
			}(q)
		}
		// 关闭监控
		go func() {
			<-d.ctx.Done()
			for _, q := range d.queues {
				close(q)
			}
		}()
	})
}

// Dispatch 发送任务。
func (d *SendDispatcher) Dispatch(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte, codec core.IHeaderCodec, cb func(error)) error {
	if conn == nil {
		return errors.New("nil connection")
	}
	d.ensureStarted(ctx)
	idx := d.selectQueue(conn, hdr)
	task := sendTask{ctx: ctx, conn: conn, hdr: hdr, payload: payload, codec: codec, cb: cb}
	select {
	case d.queues[idx] <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.ctx.Done():
		return errors.New("dispatcher closed")
	}
}

func (d *SendDispatcher) selectQueue(conn core.IConnection, hdr header.IHeader) int {
	if d.chanCount == 1 {
		return 0
	}
	// 优先以连接 ID hash 保证同连接的顺序性（在每个 queue 内保持 FIFO）
	if conn != nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(conn.ID()))
		return int(h.Sum32() % uint32(d.chanCount))
	}
	// fallback: 用子协议 hash
	if sp, ok := extractSubProto(hdr); ok {
		return int(sp) % d.chanCount
	}
	return 0
}

// Shutdown 关闭发送调度器。
func (d *SendDispatcher) Shutdown() {
	d.shutdownOnce.Do(func() {
		if d.cancel != nil {
			d.cancel()
		}
		d.wg.Wait()
	})
}

func (d *SendDispatcher) Snapshot() (channels, workers, buffer int) {
	channels = len(d.queues)
	workers = d.workersPerChan
	if channels > 0 {
		buffer = cap(d.queues[0])
	}
	return
}

// 使用 dispatcher.go 中的 extractSubProto 与 readPositiveInt
func (d *SendDispatcher) String() string {
	ch, w, b := d.Snapshot()
	return fmt.Sprintf("SendDispatcher{channels=%d workers=%d buffer=%d}", ch, w, b)
}
