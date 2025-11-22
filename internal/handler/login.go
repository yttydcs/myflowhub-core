package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

const (
	actionRegister       = "register"
	actionLogin          = "login"
	actionAssistRegister = "assist_register"
	actionAssistLogin    = "assist_login"
	actionUpload         = "upload_msg"
)

type loginRequest struct {
	Action   string `json:"action"`
	DeviceID string `json:"device_id"`
	NodeID   uint32 `json:"node_id,omitempty"`
}

type loginResponse struct {
	Code     int    `json:"code"`
	Msg      string `json:"msg"`
	NodeID   uint32 `json:"node_id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

// LoginHandler (SubProto=2) 负责注册/登录与协助上送。
// - deviceID 与 nodeID 绑定，不重复分配。
// - 未注册的 login 返回失败。
// - 响应 TargetID=0，由最近 Hub 按 deviceID/连接索引送回。
type LoginHandler struct {
	log     *slog.Logger
	nextID  atomic.Uint32
	binding struct {
		mu    sync.RWMutex
		table map[string]uint32
	}
}

func NewLoginHandler(log *slog.Logger) *LoginHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &LoginHandler{log: log}
	h.nextID.Store(2) // 1 留给默认 hub
	h.binding.table = make(map[string]uint32)
	return h
}

func (h *LoginHandler) SubProto() uint8 { return 2 }

func (h *LoginHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	req, err := h.parseRequest(payload)
	if err != nil {
		h.reply(ctx, conn, hdr, loginResponse{Code: 400, Msg: "invalid payload"})
		return
	}
	if req.DeviceID == "" {
		h.reply(ctx, conn, hdr, loginResponse{Code: 400, Msg: "device_id required"})
		return
	}
	switch strings.ToLower(req.Action) {
	case actionRegister:
		h.handleRegister(ctx, conn, hdr, req.DeviceID)
	case actionLogin:
		h.handleLogin(ctx, conn, hdr, req.DeviceID)
	case actionAssistRegister:
		h.handleRegister(ctx, conn, hdr, req.DeviceID)
	case actionAssistLogin:
		h.handleLogin(ctx, conn, hdr, req.DeviceID)
	case actionUpload:
		h.handleUpload(ctx, conn, req.DeviceID, req.NodeID)
	default:
		h.reply(ctx, conn, hdr, loginResponse{Code: 400, Msg: "unknown action"})
	}
}

func (h *LoginHandler) parseRequest(payload []byte) (loginRequest, error) {
	var req loginRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return req, err
	}
	req.Action = strings.TrimSpace(strings.ToLower(req.Action))
	return req, nil
}

func (h *LoginHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, deviceID string) {
	nodeID := h.ensureBinding(deviceID)
	h.attachMeta(ctx, conn, nodeID, deviceID)
	h.reply(ctx, conn, hdr, loginResponse{
		Code:     1,
		Msg:      "ok",
		NodeID:   nodeID,
		DeviceID: deviceID,
	})
	h.sendUpload(ctx, deviceID, nodeID, conn)
}

func (h *LoginHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, deviceID string) {
	nodeID, ok := h.lookup(deviceID)
	if !ok {
		h.reply(ctx, conn, hdr, loginResponse{
			Code: 4001,
			Msg:  "unregistered device",
		})
		return
	}
	h.attachMeta(ctx, conn, nodeID, deviceID)
	h.reply(ctx, conn, hdr, loginResponse{
		Code:     1,
		Msg:      "ok",
		NodeID:   nodeID,
		DeviceID: deviceID,
	})
	h.sendUpload(ctx, deviceID, nodeID, conn)
}

func (h *LoginHandler) handleUpload(ctx context.Context, conn core.IConnection, deviceID string, nodeID uint32) {
	if deviceID == "" || nodeID == 0 {
		h.log.Warn("invalid upload_msg", "device_id", deviceID, "node_id", nodeID)
		return
	}
	// 更新本地绑定与索引（指向下级 hub 连接）
	h.setBinding(deviceID, nodeID)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if updater, ok := cm.(interface {
				UpdateNodeIndex(uint32, core.IConnection)
				UpdateDeviceIndex(string, core.IConnection)
			}); ok {
				updater.UpdateNodeIndex(nodeID, conn)
				updater.UpdateDeviceIndex(deviceID, conn)
			}
		}
		h.forwardUpload(ctx, srv, deviceID, nodeID, conn)
	}
}

func (h *LoginHandler) ensureBinding(deviceID string) uint32 {
	if deviceID == "" {
		return 0
	}
	h.binding.mu.RLock()
	if id, ok := h.binding.table[deviceID]; ok {
		h.binding.mu.RUnlock()
		return id
	}
	h.binding.mu.RUnlock()

	h.binding.mu.Lock()
	defer h.binding.mu.Unlock()
	if id, ok := h.binding.table[deviceID]; ok {
		return id
	}
	next := h.nextID.Add(1) - 1
	h.binding.table[deviceID] = next
	return next
}

func (h *LoginHandler) setBinding(deviceID string, nodeID uint32) uint32 {
	if deviceID == "" || nodeID == 0 {
		return 0
	}
	h.binding.mu.Lock()
	defer h.binding.mu.Unlock()
	if existing, ok := h.binding.table[deviceID]; ok && existing != 0 {
		return existing
	}
	h.binding.table[deviceID] = nodeID
	return nodeID
}

func (h *LoginHandler) lookup(deviceID string) (uint32, bool) {
	h.binding.mu.RLock()
	id, ok := h.binding.table[deviceID]
	h.binding.mu.RUnlock()
	return id, ok
}

func (h *LoginHandler) attachMeta(ctx context.Context, conn core.IConnection, nodeID uint32, deviceID string) {
	conn.SetMeta("nodeID", nodeID)
	conn.SetMeta("deviceID", deviceID)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if updater, ok := cm.(interface {
				UpdateNodeIndex(uint32, core.IConnection)
				UpdateDeviceIndex(string, core.IConnection)
			}); ok {
				updater.UpdateNodeIndex(nodeID, conn)
				updater.UpdateDeviceIndex(deviceID, conn)
			}
		}
	}
}

func (h *LoginHandler) reply(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, resp loginResponse) {
	data, _ := json.Marshal(resp)
	success := resp.Code == 1
	hdr := h.buildResponseHeader(reqHdr, success, conn, ctx)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if err := srv.Send(ctx, conn.ID(), hdr, data); err != nil {
			h.log.Error("send login response failed", "err", err)
		}
		return
	}
	codec := header.HeaderTcpCodec{}
	if err := conn.SendWithHeader(hdr, data, codec); err != nil {
		h.log.Error("send login response failed (direct)", "err", err)
	}
}

func (h *LoginHandler) buildResponseHeader(reqHdr core.IHeader, ok bool, conn core.IConnection, ctx context.Context) core.IHeader {
	var base core.IHeader
	if reqHdr != nil {
		base = reqHdr.Clone()
	} else {
		base = &header.HeaderTcp{}
	}
	major := header.MajorErrResp
	if ok {
		major = header.MajorOKResp
	}
	srcID := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		srcID = srv.NodeID()
	}
	return base.
		WithMajor(major).
		WithSubProto(2).
		WithSourceID(srcID).
		WithTargetID(0)
}

// Helper for tests to reset state.
func (h *LoginHandler) reset(start uint32) {
	h.nextID.Store(start)
	h.binding.mu.Lock()
	h.binding.table = make(map[string]uint32)
	h.binding.mu.Unlock()
}

func (h *LoginHandler) sendUpload(ctx context.Context, deviceID string, nodeID uint32, srcConn core.IConnection) {
	if deviceID == "" || nodeID == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	parent, ok := findParentConnLogin(srv.ConnManager())
	if !ok {
		return
	}
	payload, _ := json.Marshal(loginRequest{
		Action:   actionUpload,
		DeviceID: deviceID,
		NodeID:   nodeID,
	})
	target := uint32(0)
	if meta, ok := parent.GetMeta("nodeID"); ok {
		if nid, ok2 := meta.(uint32); ok2 {
			target = nid
		}
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(2).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	if err := srv.Send(ctx, parent.ID(), hdr, payload); err != nil {
		h.log.Warn("send upload_msg to parent failed", "err", err, "device", deviceID)
	}
}

func (h *LoginHandler) forwardUpload(ctx context.Context, srv core.IServer, deviceID string, nodeID uint32, fromConn core.IConnection) {
	parent, ok := findParentConnLogin(srv.ConnManager())
	if !ok || (fromConn != nil && parent.ID() == fromConn.ID()) {
		return
	}
	payload, _ := json.Marshal(loginRequest{
		Action:   actionUpload,
		DeviceID: deviceID,
		NodeID:   nodeID,
	})
	target := uint32(0)
	if meta, ok := parent.GetMeta("nodeID"); ok {
		if nid, ok2 := meta.(uint32); ok2 {
			target = nid
		}
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(2).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	if err := srv.Send(ctx, parent.ID(), hdr, payload); err != nil {
		h.log.Warn("forward upload_msg failed", "err", err, "device", deviceID)
	}
}

func findParentConnLogin(cm core.IConnectionManager) (core.IConnection, bool) {
	if cm == nil {
		return nil, false
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				parent = c
				return false
			}
		}
		return true
	})
	return parent, parent != nil
}

// Errors for potential future use.
var (
	errInvalidAction = errors.New("invalid action")
)
