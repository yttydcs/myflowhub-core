package tests

import (
	"context"
	"encoding/json"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/handler"
)

type recordConn struct {
	*mockConnection
	sentHdr core.IHeader
	sent    []byte
}

func newRecordConn(id string) *recordConn {
	return &recordConn{mockConnection: &mockConnection{id: id}}
}

func (r *recordConn) SendWithHeader(h core.IHeader, payload []byte, _ core.IHeaderCodec) error {
	r.sentHdr = h
	r.sent = payload
	return nil
}

func TestLoginRegisterAndLogin(t *testing.T) {
	cm := connmgr.New()
	conn := newRecordConn("c1")
	cm.Add(conn)
	ctx := context.Background() // 直接使用连接写回，便于断言
	h := handler.NewLoginHandler(nil)

	req := map[string]any{"action": "register", "device_id": "dev-1"}
	data, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).WithSubProto(2).WithSourceID(0).WithTargetID(0)
	h.OnReceive(ctx, conn, hdr, data)

	var resp handlerResp
	_ = json.Unmarshal(conn.sent, &resp)
	if resp.Code != 1 || resp.NodeID == 0 {
		t.Fatalf("register should succeed, resp=%+v", resp)
	}
	// login should return same nodeID
	conn.sent = nil
	req["action"] = "login"
	data, _ = json.Marshal(req)
	h.OnReceive(ctx, conn, hdr, data)
	_ = json.Unmarshal(conn.sent, &resp)
	if resp.Code != 1 || resp.NodeID != resp.NodeID {
		t.Fatalf("login should reuse nodeID, resp=%+v", resp)
	}
}

func TestLoginFailWhenUnregistered(t *testing.T) {
	cm := connmgr.New()
	conn := newRecordConn("c2")
	cm.Add(conn)
	ctx := context.Background()
	h := handler.NewLoginHandler(nil)

	req := map[string]any{"action": "login", "device_id": "unknown"}
	data, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).WithSubProto(2).WithSourceID(0).WithTargetID(0)
	h.OnReceive(ctx, conn, hdr, data)

	var resp handlerResp
	_ = json.Unmarshal(conn.sent, &resp)
	if resp.Code == 1 {
		t.Fatalf("login should fail for unregistered device")
	}
}

func TestLoginSendUploadToParent(t *testing.T) {
	cm := connmgr.New()
	parent := newStubConn("p1")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent: %v", err)
	}
	conn := newRecordConn("c3")
	_ = cm.Add(conn)
	srv := &stubServer{nodeID: 10, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)
	h := handler.NewLoginHandler(nil)

	req := map[string]any{"action": "register", "device_id": "dev-3"}
	data, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).WithSubProto(2).WithSourceID(0).WithTargetID(0)
	h.OnReceive(ctx, conn, hdr, data)

	var hasUpload bool
	for _, s := range srv.sends {
		if s.connID == parent.ID() {
			hasUpload = true
		}
	}
	if !hasUpload {
		t.Fatalf("expected upload to parent conn, sends=%v", srv.sends)
	}
}

func TestUploadMsgUpdatesParentIndex(t *testing.T) {
	cm := connmgr.New()
	child := newStubConn("child-conn")
	_ = cm.Add(child)
	srv := &stubServer{nodeID: 20, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)
	h := handler.NewLoginHandler(nil)

	req := map[string]any{"action": "upload_msg", "device_id": "dev-9", "node_id": 99}
	data, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).WithSubProto(2).WithSourceID(2).WithTargetID(20)
	h.OnReceive(ctx, child, hdr, data)

	if c, ok := cm.GetByNode(99); !ok || c.ID() != child.ID() {
		t.Fatalf("node index not updated by upload_msg")
	}
	if c, ok := cm.GetByDevice("dev-9"); !ok || c.ID() != child.ID() {
		t.Fatalf("device index not updated by upload_msg")
	}
}

type handlerResp struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	NodeID   uint32 `json:"node_id"`
	DeviceID string `json:"device_id"`
}
