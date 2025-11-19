package tests

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/server"
)

// TestServerIntegration 测试服务器的完整启动和停止流程
func TestServerIntegration(t *testing.T) {
	// 创建配置
	cfg := config.NewMap(map[string]string{
		"addr": ":19001", // 使用测试端口
	})

	// 创建连接管理器
	cm := connmgr.New()

	// 创建简单的 Process
	proc := process.NewSimple(slog.Default())

	// 创建 TCP 监听器
	listener := tcp_listener.New(":19001", tcp_listener.Options{
		KeepAlive:       true,
		KeepAlivePeriod: 30 * time.Second,
		Logger:          slog.Default(),
	})

	// 创建 HeaderTcp 编解码器
	codec := header.HeaderTcpCodec{}

	// 创建 Server
	srv, err := server.New(server.Options{
		Name:     "TestServer",
		Logger:   slog.Default(),
		Process:  proc,
		Codec:    codec,
		Listener: listener,
		Config:   cfg,
		Manager:  cm,
	})
	if err != nil {
		t.Fatalf("创建服务失败: %v", err)
	}

	// 启动服务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}

	// 等待服务启动
	time.Sleep(100 * time.Millisecond)

	t.Log("服务器已启动")

	// 验证连接管理器初始为空
	if srv.ConnManager().Count() != 0 {
		t.Errorf("初始连接数应为 0，实际为 %d", srv.ConnManager().Count())
	}

	// 停止服务
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("停止服务失败: %v", err)
	}

	t.Log("服务器已停止")
}

// TestHeaderCodecIntegration 测试 header 编解码的完整流程
func TestHeaderCodecIntegration(t *testing.T) {
	codec := header.HeaderTcpCodec{}

	testCases := []struct {
		name    string
		header  header.HeaderTcp
		payload []byte
	}{
		{
			name: "空payload",
			header: header.HeaderTcp{
				MsgID:      1,
				Source:     0x11223344,
				Target:     0x55667788,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 0,
			},
			payload: nil,
		},
		{
			name: "小payload",
			header: header.HeaderTcp{
				MsgID:      2,
				Source:     0xAABBCCDD,
				Target:     0xEEFF0011,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 5,
			},
			payload: []byte("hello"),
		},
		{
			name: "大payload",
			header: header.HeaderTcp{
				MsgID:      3,
				Source:     0x12345678,
				Target:     0x9ABCDEF0,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 1024,
			},
			payload: bytes.Repeat([]byte("X"), 1024),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 使用指针以满足 IHeader（含修改方法）
			h := &tc.header
			h.WithMajor(header.MajorMsg).WithSubProto(7)

			// 编码
			frame, err := codec.Encode(h, tc.payload)
			if err != nil {
				t.Fatalf("编码失败: %v", err)
			}

			// 解码
			buf := bytes.NewReader(frame)
			gotH, gotPayload, err := codec.Decode(buf)
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}

			// 验证 header
			vh, ok := gotH.(*header.HeaderTcp)
			if !ok {
				t.Fatalf("header 类型错误: %T", gotH)
			}

			if vh.Major() != header.MajorMsg {
				t.Errorf("Major 不匹配: got %d, want %d", vh.Major(), header.MajorMsg)
			}
			if vh.SubProto() != 7 {
				t.Errorf("SubProto 不匹配: got %d, want 7", vh.SubProto())
			}
			if vh.MsgID != tc.header.MsgID {
				t.Errorf("MsgID 不匹配: got %d, want %d", vh.MsgID, tc.header.MsgID)
			}
			if vh.Source != tc.header.Source {
				t.Errorf("Source 不匹配: got 0x%08X, want 0x%08X", vh.Source, tc.header.Source)
			}
			if vh.Target != tc.header.Target {
				t.Errorf("Target 不匹配: got 0x%08X, want 0x%08X", vh.Target, tc.header.Target)
			}

			// 验证 payload
			if !bytes.Equal(gotPayload, tc.payload) {
				t.Errorf("payload 不匹配: got %d bytes, want %d bytes", len(gotPayload), len(tc.payload))
			}
		})
	}
}

// TestHeaderCodecVariousFrames 测试 header 编解码的各种帧
func TestHeaderCodecVariousFrames(t *testing.T) {
	codec := header.HeaderTcpCodec{}

	testCases := []struct {
		name    string
		header  *header.HeaderTcp // 修改为指针类型
		payload []byte
	}{
		{
			name: "空payload",
			header: &header.HeaderTcp{
				MsgID:      1,
				Source:     0x11223344,
				Target:     0x55667788,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 0,
			},
			payload: nil,
		},
		{
			name: "小payload",
			header: &header.HeaderTcp{
				MsgID:      2,
				Source:     0xAABBCCDD,
				Target:     0xEEFF0011,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 5,
			},
			payload: []byte("hello"),
		},
		{
			name: "大payload",
			header: &header.HeaderTcp{
				MsgID:      3,
				Source:     0x12345678,
				Target:     0x9ABCDEF0,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: 1024,
			},
			payload: bytes.Repeat([]byte("X"), 1024),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.header.WithMajor(header.MajorMsg).WithSubProto(7)

			// 编码
			frame, err := codec.Encode(tc.header, tc.payload)
			if err != nil {
				t.Fatalf("编码失败: %v", err)
			}

			// 解码
			buf := bytes.NewReader(frame)
			gotH, gotPayload, err := codec.Decode(buf)
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}

			// 验证 header
			vh, ok := gotH.(*header.HeaderTcp)
			if !ok {
				t.Fatalf("header 类型错误: %T", gotH)
			}

			if vh.Major() != header.MajorMsg {
				t.Errorf("Major 不匹配: got %d, want %d", vh.Major(), header.MajorMsg)
			}
			if vh.SubProto() != 7 {
				t.Errorf("SubProto 不匹配: got %d, want 7", vh.SubProto())
			}
			if vh.MsgID != tc.header.MsgID {
				t.Errorf("MsgID 不匹配: got %d, want %d", vh.MsgID, tc.header.MsgID)
			}
			if vh.Source != tc.header.Source {
				t.Errorf("Source 不匹配: got 0x%08X, want 0x%08X", vh.Source, tc.header.Source)
			}
			if vh.Target != tc.header.Target {
				t.Errorf("Target 不匹配: got 0x%08X, want 0x%08X", vh.Target, tc.header.Target)
			}

			// 验证 payload
			if !bytes.Equal(gotPayload, tc.payload) {
				t.Errorf("payload 不匹配: got %d bytes, want %d bytes", len(gotPayload), len(tc.payload))
			}
		})
	}
}

// TestConnectionManager 测试连接管理器的基本功能
func TestConnectionManager(t *testing.T) {
	cm := connmgr.New()

	// 创建模拟连接
	mockConn := &mockConnection{id: "test-conn-1"}

	// 添加连接
	if err := cm.Add(mockConn); err != nil {
		t.Fatalf("添加连接失败: %v", err)
	}

	// 验证数量
	if cm.Count() != 1 {
		t.Errorf("连接数应为 1，实际为 %d", cm.Count())
	}

	// 获取连接
	conn, ok := cm.Get("test-conn-1")
	if !ok {
		t.Error("应该能获取到连接")
	}
	if conn.ID() != "test-conn-1" {
		t.Errorf("连接 ID 不匹配: got %s, want test-conn-1", conn.ID())
	}

	// 遍历连接
	visited := false
	cm.Range(func(c core.IConnection) bool {
		visited = true
		if c.ID() != "test-conn-1" {
			t.Errorf("遍历到错误的连接: %s", c.ID())
		}
		return true
	})
	if !visited {
		t.Error("Range 应该访问到连接")
	}

	// 移除连接
	if err := cm.Remove("test-conn-1"); err != nil {
		t.Fatalf("移除连接失败: %v", err)
	}

	// 验证数量
	if cm.Count() != 0 {
		t.Errorf("连接数应为 0，实际为 %d", cm.Count())
	}

	// 获取应失败
	if _, ok := cm.Get("test-conn-1"); ok {
		t.Error("不应该能获取到已移除的连接")
	}
}
