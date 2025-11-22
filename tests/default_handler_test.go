package tests

import (
	"context"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/handler"
)

// 验证：未配置 default forward 时，默认转发到父节点
func TestDefaultHandlerForwardToParentByDefault(t *testing.T) {
	cm := connmgr.New()
	parent := newStubConn("parent-1")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent: %v", err)
	}
	srv := &stubServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := handler.NewDefaultForwardHandler(config.NewMap(nil), nil)
	hdr := (&header.HeaderTcp{}).WithSubProto(99).WithTargetID(123)
	h.OnReceive(ctx, parent, hdr, []byte("data"))

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 send to parent, got %d", len(srv.sends))
	}
	if srv.sends[0].connID != parent.ID() {
		t.Fatalf("expected send to parent, got %s", srv.sends[0].connID)
	}
}

// 验证：显式关闭 forward 时丢弃
func TestDefaultHandlerDropWhenDisabled(t *testing.T) {
	cm := connmgr.New()
	parent := newStubConn("parent-1")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	_ = cm.Add(parent)
	srv := &stubServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	cfg := config.NewMap(map[string]string{
		config.KeyDefaultForwardEnable: "false",
	})
	h := handler.NewDefaultForwardHandler(cfg, nil)
	hdr := (&header.HeaderTcp{}).WithSubProto(99).WithTargetID(123)
	h.OnReceive(ctx, parent, hdr, []byte("data"))
	if len(srv.sends) != 0 {
		t.Fatalf("expected drop, but sent %d", len(srv.sends))
	}
}
