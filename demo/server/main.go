// filepath: d:\project\MyFlowHub-Core\demo\server\main.go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/server"
	"MyFlowHub-Core/internal/handler"
)

const (
	// 使用 handler 包中的常量，保留这里方便 demo 注释引用
	subProtoEcho  = handler.SubProtoEcho
	subProtoUpper = handler.SubProtoUpper
)

// 该示例使用 MyFlowHub-Core 框架实现一个 TCP 服务端：
// 1) 使用 HeaderTcp 协议进行消息帧编解码；
// 2) 支持消息回显：收到什么返回什么，payload 前缀 "ECHO: "；
// 3) 优雅退出：Ctrl+C 等信号触发关闭；
// 4) 地址配置：通过环境变量 DEMO_ADDR（默认 :9000）。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	// 可选的发送通道配置环境变量
	sendCh := getenv("SEND_CHANNEL_COUNT", "1")
	sendW := getenv("SEND_WORKERS_PER_CHANNEL", "1")
	sendBuf := getenv("SEND_CHANNEL_BUFFER", "64")

	slog.Info("服务端启动", "listen", addr)

	procChannels := getenvInt("DEMO_PROC_CHANNELS", 2)
	procWorkers := getenvInt("DEMO_PROC_WORKERS", 2)
	procBuffer := getenvInt("DEMO_PROC_BUFFER", 128)

	cfg := config.NewMap(map[string]string{
		"addr":                       addr,
		config.KeyProcChannelCount:   strconv.Itoa(procChannels),
		config.KeyProcWorkersPerChan: strconv.Itoa(procWorkers),
		config.KeyProcChannelBuffer:  strconv.Itoa(procBuffer),
		// 发送配置
		config.KeySendChannelCount:   sendCh,
		config.KeySendWorkersPerChan: sendW,
		config.KeySendChannelBuffer:  sendBuf,
	})

	// 创建连接管理器
	cm := connmgr.New()

	dispatcher, err := buildProcess(cfg, slog.Default())
	if err != nil {
		slog.Error("创建 Process 失败", "err", err)
		os.Exit(1)
	}
	ch, workers, buf := dispatcher.ConfigSnapshot()
	slog.Info("Process pipeline ready", "channels", ch, "workers_per_channel", workers, "channel_buffer", buf)

	// 发送调度器快照由 server 初始化后打印
	// 注意：这里为演示直接根据配置打印预期值
	slog.Info("Send pipeline config", "channels", sendCh, "workers_per_channel", sendW, "channel_buffer", sendBuf)

	// 创建 TCP 监听器
	listener := tcp_listener.New(addr, tcp_listener.Options{
		KeepAlive:       true,
		KeepAlivePeriod: 30 * time.Second,
		Logger:          slog.Default(),
	})

	// 创建 HeaderTcp 编解码器
	codec := header.HeaderTcpCodec{}

	// 创建 Server
	srv, err := server.New(server.Options{
		Name:     "DispatchServer",
		Logger:   slog.Default(),
		Process:  dispatcher,
		Codec:    codec,
		Listener: listener,
		Config:   cfg,
		Manager:  cm,
	})
	if err != nil {
		slog.Error("创建服务失败", "err", err)
		os.Exit(1)
	}

	// 启动服务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		slog.Error("启动服务失败", "err", err)
		os.Exit(1)
	}

	slog.Info("服务器已启动", "addr", listener.Addr())

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	slog.Info("收到退出信号，正在关闭服务器")

	// 优雅停止
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	if err := srv.Stop(stopCtx); err != nil {
		slog.Error("停止服务失败", "err", err)
	}
	slog.Info("服务器已停止")
}

func buildProcess(cfg core.IConfig, logger *slog.Logger) (*process.DispatcherProcess, error) {
	base := process.NewPreRoutingProcess(logger)
	dispatcher, err := process.NewDispatcherFromConfig(cfg, base, logger)
	if err != nil {
		return nil, err
	}
	if err := dispatcher.RegisterHandler(handler.NewEchoHandler(logger)); err != nil {
		return nil, err
	}
	if err := dispatcher.RegisterHandler(handler.NewUpperHandler(logger)); err != nil {
		return nil, err
	}
	if err := dispatcher.RegisterHandler(handler.NewLoginHandler(logger)); err != nil {
		return nil, err
	}
	return dispatcher, nil
}

// getenv 读取环境变量，不存在则返回默认值
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return def
}

// initLoggerFromEnv 基于环境变量初始化 slog 的默认 Logger。
// 支持：LOG_LEVEL=DEBUG|INFO|WARN|ERROR（默认 INFO）
//
//	LOG_JSON=true|false（默认 false）
//	LOG_CALLER=true|false（默认 false）
func initLoggerFromEnv() {
	lv := strings.TrimSpace(strings.ToUpper(getenv("LOG_LEVEL", "INFO")))
	jsonOut := parseBool(getenv("LOG_JSON", "false"), false)
	addSource := parseBool(getenv("LOG_CALLER", "false"), false)

	level := new(slog.LevelVar)
	switch lv {
	case "DEBUG":
		level.Set(slog.LevelDebug)
	case "WARN", "WARNING":
		level.Set(slog.LevelWarn)
	case "ERROR":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}

	h := slog.HandlerOptions{Level: level, AddSource: addSource}
	var handler slog.Handler
	if jsonOut {
		handler = slog.NewJSONHandler(os.Stdout, &h)
	} else {
		handler = slog.NewTextHandler(os.Stdout, &h)
	}
	slog.SetDefault(slog.New(handler))
}

func parseBool(s string, def bool) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "true" || s == "1" || s == "yes" || s == "y" {
		return true
	}
	if s == "false" || s == "0" || s == "no" || s == "n" {
		return false
	}
	return def
}
