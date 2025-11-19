// filepath: d:\project\MyFlowHub-Core\demo\client\main.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"MyFlowHub-Core/internal/core/header"
)

const (
	subProtoEcho  = 1
	subProtoLogin = 2
)

// 该示例实现一个简单的 TCP 客户端，使用 HeaderTcp 协议：
// 1) 连接到服务端（默认 127.0.0.1:9000，可通过环境变量 DEMO_ADDR 修改）；
// 2) 定期发送带 Header 的消息；
// 3) 接收并解析服务端响应。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	var intervalSec int
	var msgCount int
	var targetID uint
	flag.IntVar(&intervalSec, "i", 3, "message send interval, seconds")
	flag.IntVar(&msgCount, "n", 5, "number of messages to send (0=infinite)")
	flag.UintVar(&targetID, "target", 1, "target node id (default 1=server)")
	flag.Parse()

	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	slog.Info("开始连接", "addr", addr)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		slog.Error("连接失败", "err", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}

	slog.Info("连接成功", "local", conn.LocalAddr(), "remote", conn.RemoteAddr())

	codec := header.HeaderTcpCodec{}

	// 1) 登录，获取分配的节点 ID
	myID, err := loginAndGetID(conn, codec)
	if err != nil {
		slog.Error("登录失败", "err", err)
		return
	}
	slog.Info("登录成功", "node_id", myID)

	// 2) 启动接收协程：处理响应与作为目标时的回显
	go recvLoop(conn, codec, myID)

	// 3) 定时发送 MSG 子协议 1 到 target
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	sent := 0
	for {
		select {
		case <-ticker.C:
			payload := []byte(fmt.Sprintf("Hello from node %d, msg #%d", myID, sent))
			hdr := header.HeaderTcp{
				MsgID:      uint32(sent + 1),
				Source:     myID,
				Target:     uint32(targetID),
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: uint32(len(payload)),
			}
			hdr.WithMajor(header.MajorMsg).WithSubProto(subProtoEcho)

			frame, err := codec.Encode(hdr, payload)
			if err != nil {
				slog.Error("编码失败", "err", err)
				return
			}
			if _, err := conn.Write(frame); err != nil {
				slog.Error("发送失败", "err", err)
				return
			}
			slog.Info("已发送", "msgid", hdr.MsgID, "to", hdr.Target, "payload", string(payload))

			sent++
			if msgCount > 0 && sent >= msgCount {
				slog.Info("已发送指定数量消息，等待2秒后退出", "count", sent)
				time.Sleep(2 * time.Second)
				return
			}
		}
	}
}

func loginAndGetID(conn net.Conn, codec header.HeaderTcpCodec) (uint32, error) {
	// 发送登录请求（SubProto=2），Target=1（默认 server）
	hdr := header.HeaderTcp{
		MsgID:      1,
		Source:     0,
		Target:     1,
		Timestamp:  uint32(time.Now().Unix()),
		PayloadLen: 0,
	}
	hdr.WithMajor(header.MajorMsg).WithSubProto(subProtoLogin)
	frame, err := codec.Encode(hdr, nil)
	if err != nil {
		return 0, err
	}
	if _, err := conn.Write(frame); err != nil {
		return 0, err
	}
	// 同步读取登录响应
	h, payload, err := codec.Decode(conn)
	if err != nil {
		return 0, err
	}
	resp, ok := h.(header.HeaderTcp)
	if !ok || resp.Major() != header.MajorOKResp || resp.SubProto() != subProtoLogin {
		return 0, fmt.Errorf("unexpected login response header: %+v", h)
	}
	var obj struct {
		ID uint32 `json:"id"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &obj); err != nil {
			return 0, err
		}
	}
	if obj.ID == 0 {
		return 0, fmt.Errorf("invalid assigned id: %d", obj.ID)
	}
	return obj.ID, nil
}

func recvLoop(conn net.Conn, codec header.HeaderTcpCodec, myID uint32) {
	for {
		h, payload, err := codec.Decode(conn)
		if err != nil {
			if err == io.EOF {
				slog.Info("服务端关闭连接")
			} else {
				slog.Error("解码失败", "err", err)
			}
			return
		}

		hdr, ok := h.(header.HeaderTcp)
		if !ok {
			slog.Error("header 类型错误")
			continue
		}

		switch hdr.Major() {
		case header.MajorMsg:
			// 如果我是目标，且子协议为1，则按规则回显
			if hdr.Target == myID && hdr.SubProto() == subProtoEcho {
				resp := header.HeaderTcp{
					MsgID:      hdr.MsgID,
					Source:     myID,
					Target:     hdr.Source,
					Timestamp:  uint32(time.Now().Unix()),
					PayloadLen: uint32(len(payload)),
				}
				resp.WithMajor(header.MajorOKResp).WithSubProto(subProtoEcho)
				frame, err := codec.Encode(resp, payload)
				if err == nil {
					_, err = conn.Write(frame)
				}
				if err != nil {
					slog.Error("回显响应发送失败", "err", err)
				} else {
					slog.Info("已回显", "to", resp.Target, "bytes", len(payload))
				}
			} else {
				slog.Info("收到消息", "from", hdr.Source, "to", hdr.Target, "subproto", hdr.SubProto(), "bytes", len(payload))
			}
		case header.MajorOKResp:
			slog.Info("收到响应", "from", hdr.Source, "to", hdr.Target, "subproto", hdr.SubProto(), "payload", string(payload))
		default:
			slog.Info("收到帧", "major", hdr.Major(), "subproto", hdr.SubProto(), "bytes", len(payload))
		}
	}
}

// getenv 读取环境变量，不存在则返回默认值
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
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
