package multi_listener

// 本文件承载 Core 框架中与 `listener` 相关的通用逻辑。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	core "github.com/yttydcs/myflowhub-core"
)

var errNoListeners = errors.New("no listeners")

// MultiListener 组合多个 listener，使其可以同时 Listen，并在任一 listener 退出/报错时统一收敛关闭。
//
// 设计目标：
// - 便于 Server 同时启用 TCP + 未来 RFCOMM 等多入口；
// - 发生错误时尽快停止其他 listener，避免“半存活”；
// - ctx cancel 作为正常退出路径：返回 nil（与 TCPListener 保持一致）。
type MultiListener struct {
	listeners []core.IListener

	closed    atomic.Bool
	closeOnce sync.Once
}

// New 校验并封装多个子 listener，供 server 统一启动与收敛。
func New(listeners ...core.IListener) (*MultiListener, error) {
	if len(listeners) == 0 {
		return nil, errNoListeners
	}
	cp := make([]core.IListener, 0, len(listeners))
	for i, l := range listeners {
		if l == nil {
			return nil, fmt.Errorf("nil listener at index %d", i)
		}
		cp = append(cp, l)
	}
	return &MultiListener{listeners: cp}, nil
}

func (l *MultiListener) Protocol() string { return "multi" }

func (l *MultiListener) Addr() net.Addr { return nil }

// Listen 并行拉起全部子 listener，并在任一退出时触发整体收敛。
func (l *MultiListener) Listen(ctx context.Context, cm core.IConnectionManager) error {
	if l.closed.Load() {
		return errors.New("multi listener already closed")
	}
	if len(l.listeners) == 0 {
		return errNoListeners
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	// 确保 ctx 取消时，尽快触发 Close 以唤醒可能阻塞在 Accept 的子 listener。
	go func() {
		<-ctx2.Done()
		_ = l.Close()
	}()

	errCh := make(chan error, len(l.listeners))
	var wg sync.WaitGroup
	for _, child := range l.listeners {
		wg.Add(1)
		go func(c core.IListener) {
			defer wg.Done()
			errCh <- c.Listen(ctx2, cm)
		}(child)
	}

	var firstErr error
	for i := 0; i < len(l.listeners); i++ {
		err := <-errCh
		if err != nil && firstErr == nil {
			firstErr = err
			cancel()
			_ = l.Close()
		}
	}
	wg.Wait()

	// ctx cancel 视为正常退出，不向上抛错（与现有 TCPListener 行为对齐）。
	if ctx.Err() != nil {
		return nil
	}
	return firstErr
}

// Close 尽力关闭全部子 listener，并返回遇到的首个错误。
func (l *MultiListener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
	})
	var firstErr error
	for _, child := range l.listeners {
		if child == nil {
			continue
		}
		if err := child.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
