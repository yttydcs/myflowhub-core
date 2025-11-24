package tests

import (
	"context"
	"net"
	"testing"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/bootstrap"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/server"
	"MyFlowHub-Core/internal/handler"
)

// Integration: Root (node=1) -> LoginServer (node=100) -> Hub (self-register) -> Client (Echo ping).
func TestRootHubPing(t *testing.T) {
	rootAddr := freeAddr()
	hubAddr := freeAddr()

	// Root handles login locally for self-register.
	rootCfg := config.NewMap(map[string]string{
		"addr": rootAddr,
	})
	rootHandlers := []core.ISubProcess{handler.NewLoginHandler(nil)}
	rootSrv := startTestServer(t, server.Options{
		Name:     "Root",
		Process:  makeProcess(t, rootCfg, rootHandlers),
		Codec:    header.HeaderTcpCodec{},
		Listener: tcp_listener.New(rootAddr),
		Config:   rootCfg,
		Manager:  connmgr.New(),
		NodeID:   1,
	})
	defer stopTestServer(t, rootSrv)

	waitListen(t, rootAddr, 2*time.Second)

	// Hub self-register to get node id from root.
	nodeID, _, err := bootstrap.SelfRegister(context.Background(), bootstrap.SelfRegisterOptions{
		ParentAddr: rootAddr,
		SelfID:     "hub-test",
		Timeout:    5 * time.Second,
		DoLogin:    false, // login可后续补上
	})
	if err != nil {
		t.Fatalf("self register: %v", err)
	}
	if nodeID == 0 {
		t.Fatalf("self register returned node id 0")
	}

	hubCfg := config.NewMap(map[string]string{
		"addr":                 hubAddr,
		config.KeyParentEnable: "true",
		config.KeyParentAddr:   rootAddr,
	})
	hubHandlers := []core.ISubProcess{handler.NewEchoHandler(nil), handler.NewLoginHandler(nil)}
	hubSrv := startTestServer(t, server.Options{
		Name:     "Hub",
		Process:  makeProcess(t, hubCfg, hubHandlers),
		Codec:    header.HeaderTcpCodec{},
		Listener: tcp_listener.New(hubAddr),
		Config:   hubCfg,
		Manager:  connmgr.New(),
		NodeID:   nodeID,
	})
	defer stopTestServer(t, hubSrv)
	waitListen(t, hubAddr, 2*time.Second)

	// Client sends echo to hub.
	conn, err := net.Dial("tcp", hubAddr)
	if err != nil {
		t.Fatalf("dial hub: %v", err)
	}
	defer conn.Close()

	codec := header.HeaderTcpCodec{}
	payload := []byte("ping")
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(handler.SubProtoEcho).
		WithSourceID(nodeID).
		WithTargetID(nodeID).
		WithMsgID(1).
		WithPayloadLength(uint32(len(payload)))
	frame, _ := codec.Encode(hdr, payload)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, respPayload, err := codec.Decode(conn)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(respPayload) != "ECHO: ping" {
		t.Fatalf("unexpected payload: %s", string(respPayload))
	}
}

func makeProcess(t *testing.T, cfg core.IConfig, handlers []core.ISubProcess) core.IProcess {
	base := process.NewPreRoutingProcess(nil).WithConfig(cfg)
	dp, err := process.NewDispatcherFromConfig(cfg, base, nil)
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	for _, h := range handlers {
		if err := dp.RegisterHandler(h); err != nil {
			t.Fatalf("register handler: %v", err)
		}
	}
	dp.RegisterDefaultHandler(handler.NewDefaultForwardHandler(cfg, nil))
	return dp
}

func startTestServer(t *testing.T, opts server.Options) core.IServer {
	srv, err := server.New(opts)
	if err != nil {
		t.Fatalf("server new: %v", err)
	}
	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	return srv
}

func stopTestServer(t *testing.T, srv core.IServer) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Logf("stop server: %v", err)
	}
}

func freeAddr() string {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitListen(t *testing.T, addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitListen timeout for %s", addr)
}
