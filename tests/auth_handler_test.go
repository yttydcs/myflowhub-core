package tests

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/handler"
)

func TestLoginHandlerGetPermsAndListRoles(t *testing.T) {
	cfg := config.NewMap(map[string]string{
		config.KeyAuthNodeRoles: "5:admin",
		config.KeyAuthRolePerms: "admin:p.read,p.write",
	})
	h := handler.NewLoginHandlerWithConfig(cfg, nil)

	cm := connmgr.New()
	conn := newAuthConn("c1")
	conn.SetMeta("nodeID", uint32(5))
	_ = cm.Add(conn)
	srv := newAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	// get_perms
	req := mustJSON(map[string]any{"action": "get_perms", "data": map[string]any{"node_id": 5}})
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	h.OnReceive(ctx, conn, hdr, req)

	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(conn.sent))
	}
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(conn.sent[0].payload, &msg)
	if msg.Action != "get_perms_resp" {
		t.Fatalf("unexpected action %s", msg.Action)
	}
	var data struct {
		Code  int      `json:"code"`
		Role  string   `json:"role"`
		Perms []string `json:"perms"`
	}
	_ = json.Unmarshal(msg.Data, &data)
	if data.Code != 1 || data.Role != "admin" || len(data.Perms) != 2 {
		t.Fatalf("unexpected perms resp %+v", data)
	}

	// list_roles
	conn.sent = nil
	reqList := mustJSON(map[string]any{"action": "list_roles", "data": map[string]any{}})
	h.OnReceive(ctx, conn, hdr, reqList)
	if len(conn.sent) != 1 {
		t.Fatalf("expected list_roles resp, got %d", len(conn.sent))
	}
	_ = json.Unmarshal(conn.sent[0].payload, &msg)
	if msg.Action != "list_roles_resp" {
		t.Fatalf("unexpected action %s", msg.Action)
	}
	var list struct {
		Code  int `json:"code"`
		Roles []struct {
			NodeID uint32 `json:"node_id"`
			Role   string `json:"role"`
		} `json:"roles"`
	}
	_ = json.Unmarshal(msg.Data, &list)
	if list.Code != 1 || len(list.Roles) == 0 || list.Roles[0].Role != "admin" {
		t.Fatalf("unexpected list_roles_resp %+v", list)
	}
}

func TestLoginHandlerPermsInvalidate(t *testing.T) {
	cfg := config.NewMap(map[string]string{
		config.KeyAuthNodeRoles: "5:admin;6:node",
	})
	h := handler.NewLoginHandlerWithConfig(cfg, nil)
	cm := connmgr.New()
	connTarget := newAuthConn("c5")
	_ = cm.Add(connTarget)

	connOther := newAuthConn("c6")
	connOther.SetMeta("nodeID", uint32(6))
	connOther.SetMeta("role", "node")
	_ = cm.Add(connOther)

	srv := newAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	// 注册写入绑定，分配 nodeID
	regMsg := mustJSON(map[string]any{"action": "register", "data": map[string]any{"device_id": "dev-1"}})
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	h.OnReceive(ctx, connTarget, hdr, regMsg)
	nodeIDVal, _ := connTarget.GetMeta("nodeID")
	nodeID, _ := nodeIDVal.(uint32)
	if nodeID == 0 {
		t.Fatalf("expected nodeID assigned")
	}
	connTarget.SetMeta("role", "admin")
	connTarget.SetMeta("perms", []string{"p.read"})

	// invalidate node 5
	req := mustJSON(map[string]any{"action": "perms_invalidate", "data": map[string]any{"node_ids": []uint32{nodeID}}})
	h.OnReceive(ctx, connTarget, hdr, req)

	// meta cleared for node 5
	if role, _ := connTarget.GetMeta("role"); role != "" {
		t.Fatalf("expected role cleared for node 5, got %v", role)
	}
	if perms, _ := connTarget.GetMeta("perms"); perms != nil {
		if v, ok := perms.([]string); !ok || len(v) != 0 {
			t.Fatalf("expected perms cleared for node 5, got %v", perms)
		}
	}
	// other node untouched
	if role, _ := connOther.GetMeta("role"); role == "" {
		t.Fatalf("unexpected role cleared for other node")
	}
}

// --- helpers ---

type authConn struct {
	id   string
	meta map[string]any
	sent []sentFrame
}

func newAuthConn(id string) *authConn {
	return &authConn{id: id, meta: make(map[string]any)}
}

func (c *authConn) ID() string                           { return c.id }
func (c *authConn) Close() error                         { return nil }
func (c *authConn) OnReceive(core.ReceiveHandler)        {}
func (c *authConn) SetMeta(key string, val any)          { c.meta[key] = val }
func (c *authConn) GetMeta(key string) (any, bool)       { v, ok := c.meta[key]; return v, ok }
func (c *authConn) Metadata() map[string]any             { return c.meta }
func (c *authConn) LocalAddr() net.Addr                  { return mockAddr{} }
func (c *authConn) RemoteAddr() net.Addr                 { return mockAddr{} }
func (c *authConn) Reader() core.IReader                 { return nil }
func (c *authConn) SetReader(core.IReader)               {}
func (c *authConn) DispatchReceive(core.IHeader, []byte) {}
func (c *authConn) RawConn() net.Conn                    { return nil }
func (c *authConn) Send([]byte) error                    { return nil }
func (c *authConn) SendWithHeader(h core.IHeader, payload []byte, _ core.IHeaderCodec) error {
	c.sent = append(c.sent, sentFrame{hdr: h, payload: payload})
	return nil
}

type authServer struct {
	nodeID uint32
	cm     core.IConnectionManager
}

func newAuthServer(nodeID uint32, cm core.IConnectionManager) *authServer {
	return &authServer{nodeID: nodeID, cm: cm}
}

func (s *authServer) Start(context.Context) error          { return nil }
func (s *authServer) Stop(context.Context) error           { return nil }
func (s *authServer) Config() core.IConfig                 { return config.NewMap(nil) }
func (s *authServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *authServer) Process() core.IProcess               { return nil }
func (s *authServer) HeaderCodec() core.IHeaderCodec       { return header.HeaderTcpCodec{} }
func (s *authServer) NodeID() uint32                       { return s.nodeID }
func (s *authServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *authServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	if c, ok := s.cm.Get(connID); ok {
		return c.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
	}
	return nil
}
