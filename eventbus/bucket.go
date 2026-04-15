package eventbus

// 本文件承载 Core 框架中与 `bucket` 相关的通用逻辑。

import (
	"context"
	"sync"
)

type bucket struct {
	ch       chan Event
	handlers map[string]Handler
	mu       sync.RWMutex
	cancel   context.CancelFunc
}

// newBucket 为单个事件名创建独立队列与 worker 组。
func newBucket(opts Options) *bucket {
	ctx, cancel := context.WithCancel(context.Background())
	b := &bucket{
		ch:       make(chan Event, opts.DefaultBuffer),
		handlers: make(map[string]Handler),
		cancel:   cancel,
	}
	workers := opts.DefaultWorkers
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go b.loop(ctx)
	}
	return b
}

// addHandler 向当前事件桶注册一个订阅者。
func (b *bucket) addHandler(token string, h Handler) {
	b.mu.Lock()
	b.handlers[token] = h
	b.mu.Unlock()
}

// removeHandler 从当前事件桶删除一个订阅者。
func (b *bucket) removeHandler(token string) {
	b.mu.Lock()
	delete(b.handlers, token)
	b.mu.Unlock()
}

// loop 持续消费桶内事件，并把它们交给 dispatch。
func (b *bucket) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-b.ch:
			b.dispatch(ctx, ev)
		}
	}
}

// dispatch 在读锁下遍历订阅者，并用 panic 保护避免单个处理器拖垮整个事件桶。
func (b *bucket) dispatch(ctx context.Context, ev Event) {
	b.mu.RLock()
	for _, h := range b.handlers {
		func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					// ignore panic to protect loop
				}
			}()
			handler(ctx, ev)
		}(h)
	}
	b.mu.RUnlock()
}

// close 取消 bucket 的上下文，让后台 worker 尽快退出。
func (b *bucket) close() {
	if b.cancel != nil {
		b.cancel()
	}
}
