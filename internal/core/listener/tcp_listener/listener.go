package tcp_listener

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	core "MyFlowHub-Core/internal/core"
)

// Options 配置 TCPListener 的行为。
type Options struct {
	// Addr 监听地址，例如 ":9000" 或 "127.0.0.1:9000"。
	Addr string
	// KeepAlive 是否对接受到的 TCP 连接开启 KeepAlive（默认取 New 的入参策略）。
	KeepAlive bool
	// KeepAlivePeriod KeepAlive 周期（默认 30s；仅在 KeepAlive 为 true 时生效）。
	KeepAlivePeriod time.Duration
	// Logger 可选日志器；若为空使用 slog.Default()。
	Logger *slog.Logger
}

func (o *Options) setDefaults() {
	if o.KeepAlivePeriod <= 0 {
		o.KeepAlivePeriod = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// TCPListener 实现 core.IListener，用于接受 TCP 连接并交由连接管理器管理。
type TCPListener struct {
	opts   Options
	ln     net.Listener
	closed atomic.Bool
}

// New 创建一个 TCPListener。
// 如果未传入 Options，则默认开启 KeepAlive。
func New(addr string, opts ...Options) *TCPListener {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	} else {
		// 默认开启 KeepAlive
		o.KeepAlive = true
	}
	o.Addr = addr
	o.setDefaults()
	return &TCPListener{opts: o}
}

// Protocol 返回协议标识。
func (l *TCPListener) Protocol() string { return "tcp" }

// Addr 返回监听地址（在 Listen 成功后可用）。
func (l *TCPListener) Addr() net.Addr {
	if l.ln != nil {
		return l.ln.Addr()
	}
	return nil
}

// Listen 启动监听并在接受到新连接时创建 IConnection 并添加到 cm。
func (l *TCPListener) Listen(ctx context.Context, cm core.IConnectionManager) error {
	if l.closed.Load() {
		return errors.New("tcp listener already closed")
	}
	if l.opts.Addr == "" {
		return errors.New("tcp listener addr is empty")
	}
	ln, err := net.Listen("tcp", l.opts.Addr)
	if err != nil {
		return err
	}
	l.ln = ln
	log := l.opts.Logger
	log.Info("tcp listener started", "addr", ln.Addr().String())

	// 监控 ctx，取消时关闭监听器以唤醒 Accept
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-ctxDone:
		}
	}()

	defer func() {
		close(ctxDone)
		_ = ln.Close()
		log.Info("tcp listener stopped")
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// 若是关闭导致或 ctx 取消，退出
			if l.closed.Load() || ctx.Err() != nil {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Warn("accept temporary error", "err", ne)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if strings.Contains(strings.ToLower(err.Error()), "closed") {
				return nil
			}
			return err
		}

		// 设置 TCP KeepAlive
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlive(l.opts.KeepAlive)
			if l.opts.KeepAlive {
				_ = tcp.SetKeepAlivePeriod(l.opts.KeepAlivePeriod)
			}
		}

		// 包装为 core.IConnection 并加入连接管理器
		c := NewTCPConnection(conn)
		if err := cm.Add(c); err != nil {
			log.Warn("failed to add connection to manager", "remote", conn.RemoteAddr().String(), "err", err)
			_ = conn.Close()
			continue
		}
		log.Debug("new connection accepted", "remote", conn.RemoteAddr().String())
	}
}

// Close 停止监听。
func (l *TCPListener) Close() error {
	l.closed.Store(true)
	if l.ln != nil {
		return l.ln.Close()
	}
	return nil
}
