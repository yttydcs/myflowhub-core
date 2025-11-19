package tests

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/process"
)

const (
	testSubProtoEcho  = 1
	testSubProtoUpper = 2
)

func TestDispatcherRoutesSubProtocols(t *testing.T) {
	cfg := config.NewMap(map[string]string{
		config.KeyProcChannelCount:   "1",
		config.KeyProcWorkersPerChan: "1",
		config.KeyProcChannelBuffer:  "8",
	})
	base := &spyBaseProcess{}
	dispatcher, err := process.NewDispatcherFromConfig(cfg, base, slog.Default())
	if err != nil {
		t.Fatalf("failed to create dispatcher: %v", err)
	}
	defer dispatcher.Shutdown()

	chEcho := make(chan string, 1)
	chUpper := make(chan string, 1)
	if err := dispatcher.RegisterHandler(&recordHandler{sub: testSubProtoEcho, ch: chEcho}); err != nil {
		t.Fatalf("register echo handler: %v", err)
	}
	if err := dispatcher.RegisterHandler(&recordHandler{sub: testSubProtoUpper, ch: chUpper}); err != nil {
		t.Fatalf("register upper handler: %v", err)
	}

	conn := &mockConnection{id: "test-conn"}
	hdrEcho := &header.HeaderTcp{}
	hdrEcho.WithSubProto(testSubProtoEcho)
	hdrUpper := &header.HeaderTcp{}
	hdrUpper.WithSubProto(testSubProtoUpper)

	dispatcher.OnReceive(context.Background(), conn, hdrEcho, []byte("hello"))
	dispatcher.OnReceive(context.Background(), conn, hdrUpper, []byte("world"))

	expectMessage(t, chEcho, "test-conn|hello")
	expectMessage(t, chUpper, "test-conn|world")

	if got := base.receives.Load(); got != 2 {
		t.Fatalf("base OnReceive called %d times, want 2", got)
	}
}

func TestDispatcherConfigSnapshot(t *testing.T) {
	cfg := config.NewMap(map[string]string{
		config.KeyProcChannelCount:   "3",
		config.KeyProcWorkersPerChan: "2",
		config.KeyProcChannelBuffer:  "32",
	})
	dispatcher, err := process.NewDispatcherFromConfig(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("failed to create dispatcher: %v", err)
	}
	defer dispatcher.Shutdown()

	channels, workers, buffer := dispatcher.ConfigSnapshot()
	if channels != 3 || workers != 2 || buffer != 32 {
		t.Fatalf("snapshot mismatch: got channels=%d workers=%d buffer=%d", channels, workers, buffer)
	}
}

type spyBaseProcess struct {
	listens  atomic.Int64
	receives atomic.Int64
	sends    atomic.Int64
	closes   atomic.Int64
}

func (s *spyBaseProcess) OnListen(core.IConnection) { s.listens.Add(1) }
func (s *spyBaseProcess) OnReceive(context.Context, core.IConnection, core.IHeader, []byte) {
	s.receives.Add(1)
}
func (s *spyBaseProcess) OnSend(context.Context, core.IConnection, core.IHeader, []byte) error {
	s.sends.Add(1)
	return nil
}
func (s *spyBaseProcess) OnClose(core.IConnection) { s.closes.Add(1) }

type recordHandler struct {
	sub uint8
	ch  chan<- string
}

func (h *recordHandler) SubProto() uint8 { return h.sub }
func (h *recordHandler) OnReceive(ctx context.Context, conn core.IConnection, _ core.IHeader, payload []byte) {
	h.ch <- fmt.Sprintf("%s|%s", conn.ID(), string(payload))
}

func expectMessage(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("unexpected message: got %q want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for message %q", want)
	}
}
