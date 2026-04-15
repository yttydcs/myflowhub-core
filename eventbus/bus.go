package eventbus

// 本文件承载 Core 框架中与 `bus` 相关的通用逻辑。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event 描述一条事件。
type Event struct {
	Name string
	Data any
	Meta map[string]any
	Time time.Time
}

// Handler 事件处理函数。
type Handler func(ctx context.Context, evt Event)

// IBus 事件总线接口。
type IBus interface {
	// Publish 将事件发布到异步队列，若 ctx 结束则返回错误。
	Publish(ctx context.Context, name string, data any, meta map[string]any) error
	// PublishSync 同步触发，直接在当前 goroutine 调用订阅者。
	PublishSync(ctx context.Context, name string, data any, meta map[string]any)
	// Subscribe 注册事件处理函数，返回 token。
	Subscribe(name string, h Handler) string
	// Unsubscribe 通过 token 取消订阅。
	Unsubscribe(name, token string)
	// Close 关闭总线，停止所有 worker。
	Close()
}

// Options 配置事件总线默认桶参数。
type Options struct {
	// 默认每个事件的缓冲区大小。
	DefaultBuffer int
	// 默认每个事件的 worker 数。
	DefaultWorkers int
}

type bus struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	opts    Options
	closed  atomic.Bool
	counter atomic.Uint64
}

// New 创建事件总线。
func New(opts Options) IBus {
	if opts.DefaultBuffer <= 0 {
		opts.DefaultBuffer = 64
	}
	if opts.DefaultWorkers <= 0 {
		opts.DefaultWorkers = 1
	}
	return &bus{
		buckets: make(map[string]*bucket),
		opts:    opts,
	}
}

// normalize 将事件名归一化为内部统一使用的小写键。
func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Publish 把事件投递到异步桶队列，由 bucket worker 后台分发。
func (b *bus) Publish(ctx context.Context, name string, data any, meta map[string]any) error {
	if b.closed.Load() {
		return fmt.Errorf("eventbus closed")
	}
	key := normalize(name)
	if key == "" {
		return nil
	}
	bkt := b.getOrCreateBucket(key)
	ev := Event{Name: key, Data: data, Meta: meta, Time: time.Now()}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case bkt.ch <- ev:
		return nil
	}
}

// PublishSync 直接在当前 goroutine 内调用订阅者，适合需要同步可见性的路径。
func (b *bus) PublishSync(ctx context.Context, name string, data any, meta map[string]any) {
	if b.closed.Load() {
		return
	}
	key := normalize(name)
	if key == "" {
		return
	}
	bkt := b.getOrCreateBucket(key)
	ev := Event{Name: key, Data: data, Meta: meta, Time: time.Now()}
	bkt.dispatch(ctx, ev)
}

// Subscribe 为指定事件名注册处理器，并返回可反注册的 token。
func (b *bus) Subscribe(name string, h Handler) string {
	if b.closed.Load() {
		return ""
	}
	if h == nil {
		return ""
	}
	key := normalize(name)
	if key == "" {
		return ""
	}
	bkt := b.getOrCreateBucket(key)
	token := fmt.Sprintf("%s#%d", key, b.counter.Add(1))
	bkt.addHandler(token, h)
	return token
}

// Unsubscribe 按 token 移除订阅，避免后续事件继续命中旧 handler。
func (b *bus) Unsubscribe(name, token string) {
	key := normalize(name)
	if key == "" || token == "" {
		return
	}
	b.mu.RLock()
	bkt := b.buckets[key]
	b.mu.RUnlock()
	if bkt != nil {
		bkt.removeHandler(token)
	}
}

// Close 停止全部 bucket worker，并拒绝后续 publish/subscribe。
func (b *bus) Close() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.mu.Lock()
	for _, bkt := range b.buckets {
		bkt.close()
	}
	b.buckets = nil
	b.mu.Unlock()
}

// getOrCreateBucket 惰性创建事件桶，把不同事件名的队列与 worker 隔离开。
func (b *bus) getOrCreateBucket(key string) *bucket {
	b.mu.RLock()
	if bkt, ok := b.buckets[key]; ok {
		b.mu.RUnlock()
		return bkt
	}
	b.mu.RUnlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	if bkt, ok := b.buckets[key]; ok {
		return bkt
	}
	bkt := newBucket(b.opts)
	b.buckets[key] = bkt
	return bkt
}
