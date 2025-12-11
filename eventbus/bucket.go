package eventbus

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

func (b *bucket) addHandler(token string, h Handler) {
	b.mu.Lock()
	b.handlers[token] = h
	b.mu.Unlock()
}

func (b *bucket) removeHandler(token string) {
	b.mu.Lock()
	delete(b.handlers, token)
	b.mu.Unlock()
}

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

func (b *bucket) close() {
	if b.cancel != nil {
		b.cancel()
	}
}
