package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/header"
)

const (
	actionRegister            = "register"
	actionAssistRegister      = "assist_register"
	actionRegisterResp        = "register_resp"
	actionAssistRegisterResp  = "assist_register_resp"
	actionLogin               = "login"
	actionAssistLogin         = "assist_login"
	actionLoginResp           = "login_resp"
	actionAssistLoginResp     = "assist_login_resp"
	actionRevoke              = "revoke"
	actionRevokeResp          = "revoke_resp"
	actionAssistQueryCred     = "assist_query_credential"
	actionAssistQueryCredResp = "assist_query_credential_resp"
	actionOffline             = "offline"
	actionAssistOffline       = "assist_offline"
	actionGetPerms            = "get_perms"
	actionGetPermsResp        = "get_perms_resp"
	actionListRoles           = "list_roles"
	actionListRolesResp       = "list_roles_resp"
	actionPermsInvalidate     = "perms_invalidate"
)

type message struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

type registerData struct {
	DeviceID string `json:"device_id"`
}

type loginData struct {
	DeviceID   string `json:"device_id"`
	Credential string `json:"credential"`
}

type revokeData struct {
	DeviceID   string `json:"device_id"`
	NodeID     uint32 `json:"node_id,omitempty"`
	Credential string `json:"credential,omitempty"`
}

type queryCredData struct {
	DeviceID string `json:"device_id"`
	NodeID   uint32 `json:"node_id,omitempty"`
}

type offlineData struct {
	DeviceID string `json:"device_id"`
	NodeID   uint32 `json:"node_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type respData struct {
	Code       int      `json:"code"`
	Msg        string   `json:"msg,omitempty"`
	DeviceID   string   `json:"device_id,omitempty"`
	NodeID     uint32   `json:"node_id,omitempty"`
	Credential string   `json:"credential,omitempty"`
	Role       string   `json:"role,omitempty"`
	Perms      []string `json:"perms,omitempty"`
}

type bindingRecord struct {
	NodeID     uint32
	Credential string
	Role       string
	Perms      []string
}

type permsQueryData struct {
	NodeID uint32 `json:"node_id"`
}

type invalidateData struct {
	NodeIDs []uint32 `json:"node_ids,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type rolePermEntry struct {
	NodeID uint32   `json:"node_id,omitempty"`
	Role   string   `json:"role,omitempty"`
	Perms  []string `json:"perms,omitempty"`
}

// LoginHandler implements register/login/revoke/offline flows with action+data payload.
type LoginHandler struct {
	log *slog.Logger

	nextID atomic.Uint32

	mu          sync.RWMutex
	whitelist   map[string]bindingRecord // deviceID -> record
	pendingConn map[string]string        // deviceID -> connID (in-flight assist)

	authNode uint32

	defaultRole  string
	defaultPerms []string
	nodeRoles    map[uint32]string
	rolePerms    map[string][]string
}

func NewLoginHandler(log *slog.Logger) *LoginHandler {
	return NewLoginHandlerWithConfig(nil, log)
}

func NewLoginHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *LoginHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &LoginHandler{
		log:         log,
		whitelist:   make(map[string]bindingRecord),
		pendingConn: make(map[string]string),
		defaultRole: "node",
		nodeRoles:   make(map[uint32]string),
		rolePerms:   make(map[string][]string),
	}
	h.loadAuthConfig(cfg)
	h.nextID.Store(2)
	return h
}

func (h *LoginHandler) SubProto() uint8 { return 2 }

func (h *LoginHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("invalid login payload", "err", err)
		return
	}
	act := strings.ToLower(strings.TrimSpace(msg.Action))
	switch act {
	case actionRegister:
		h.handleRegister(ctx, conn, hdr, msg.Data, false)
	case actionAssistRegister:
		h.handleRegister(ctx, conn, hdr, msg.Data, true)
	case actionRegisterResp:
		h.handleRegisterResp(ctx, msg.Data)
	case actionLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, false)
	case actionAssistLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, true)
	case actionLoginResp:
		h.handleLoginResp(ctx, msg.Data)
	case actionRevoke:
		h.handleRevoke(ctx, conn, msg.Data)
	case actionAssistQueryCred:
		h.handleAssistQuery(ctx, conn, hdr, msg.Data)
	case actionAssistQueryCredResp:
		h.handleAssistQueryResp(ctx, msg.Data)
	case actionOffline:
		h.handleOffline(ctx, conn, msg.Data, false)
	case actionAssistOffline:
		h.handleOffline(ctx, conn, msg.Data, true)
	case actionGetPerms:
		h.handleGetPerms(ctx, conn, msg.Data)
	case actionListRoles:
		h.handleListRoles(ctx, conn)
	case actionPermsInvalidate:
		h.handlePermsInvalidate(ctx, msg.Data)
	default:
		h.log.Debug("unknown login action", "action", act)
	}
}

// register handling
func (h *LoginHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionRegisterResp, respData{Code: 400, Msg: "invalid register data"})
		return
	}
	if assisted {
		// being processed at authority
		nodeID := h.ensureNodeID(req.DeviceID)
		cred := h.ensureCredential(req.DeviceID)
		h.sendResp(ctx, conn, hdr, actionAssistRegisterResp, respData{
			Code:       1,
			Msg:        "ok",
			DeviceID:   req.DeviceID,
			NodeID:     nodeID,
			Credential: cred,
			Role:       h.resolveRole(nodeID),
			Perms:      h.resolvePerms(nodeID),
		})
		return
	}
	authority := h.selectAuthority(ctx)
	if authority != nil {
		h.setPending(req.DeviceID, conn.ID())
		h.forward(ctx, authority, actionAssistRegister, req)
		return
	}
	// self authority
	nodeID := h.ensureNodeID(req.DeviceID)
	cred := h.ensureCredential(req.DeviceID)
	h.saveBinding(ctx, conn, req.DeviceID, nodeID, cred)
	h.sendResp(ctx, conn, hdr, actionRegisterResp, respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     nodeID,
		Credential: cred,
		Role:       h.resolveRole(nodeID),
		Perms:      h.resolvePerms(nodeID),
	})
}

func (h *LoginHandler) handleRegisterResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
		h.applyRolePerms(resp.DeviceID, resp.NodeID, resp.Role, resp.Perms, c)
		h.sendResp(ctx, c, nil, actionRegisterResp, resp)
	}
}

// login handling
func (h *LoginHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req loginData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 400, Msg: "invalid login data"})
		return
	}
	if assisted {
		rec, ok := h.lookup(req.DeviceID)
		if !ok || rec.Credential != req.Credential {
			h.sendResp(ctx, conn, hdr, actionAssistLoginResp, respData{Code: 4001, Msg: "invalid credential"})
			return
		}
		h.sendResp(ctx, conn, hdr, actionAssistLoginResp, respData{
			Code:       1,
			Msg:        "ok",
			DeviceID:   req.DeviceID,
			NodeID:     rec.NodeID,
			Credential: rec.Credential,
		})
		return
	}
	// local check
	if rec, ok := h.lookup(req.DeviceID); ok {
		if rec.Credential == req.Credential {
			h.saveBinding(ctx, conn, req.DeviceID, rec.NodeID, rec.Credential)
			h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: rec.NodeID, Credential: rec.Credential})
			return
		}
		h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid credential"})
		return
	}
	// not found locally, try authority
	authority := h.selectAuthority(ctx)
	if authority != nil {
		h.setPending(req.DeviceID, conn.ID())
		h.forward(ctx, authority, actionAssistLogin, req)
		return
	}
	h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid credential"})
}

func (h *LoginHandler) handleLoginResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		if resp.Code == 1 {
			h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
			h.applyRolePerms(resp.DeviceID, resp.NodeID, resp.Role, resp.Perms, c)
		}
		h.sendResp(ctx, c, nil, actionLoginResp, resp)
	}
}

// revoke handling: broadcast; respond only if deleted or credential mismatch
func (h *LoginHandler) handleRevoke(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	removed, mismatch := h.removeBinding(req.DeviceID, req.Credential)
	if removed || mismatch {
		// respond only when changed/mismatch
		if mismatch {
			h.sendResp(ctx, conn, nil, actionRevokeResp, respData{Code: 4402, Msg: "credential mismatch", DeviceID: req.DeviceID, NodeID: req.NodeID})
		} else {
			h.sendResp(ctx, conn, nil, actionRevokeResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: req.NodeID})
		}
	}
	// broadcast downstream and upstream except source
	h.broadcast(ctx, conn, actionRevoke, req)
}

// assist query credential
func (h *LoginHandler) handleAssistQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req queryCredData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 400, Msg: "invalid query"})
		return
	}
	if rec, ok := h.lookup(req.DeviceID); ok {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: rec.NodeID, Credential: rec.Credential, Role: rec.Role, Perms: rec.Perms})
		return
	}
	h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 4001, Msg: "not found"})
}

func (h *LoginHandler) handleAssistQueryResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		if resp.Code == 1 {
			h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
			h.applyRolePerms(resp.DeviceID, resp.NodeID, resp.Role, resp.Perms, c)
			h.sendResp(ctx, c, nil, actionLoginResp, respData{Code: 1, Msg: "ok", DeviceID: resp.DeviceID, NodeID: resp.NodeID, Credential: resp.Credential, Role: resp.Role, Perms: resp.Perms})
			return
		}
		h.sendResp(ctx, c, nil, actionLoginResp, respData{Code: resp.Code, Msg: resp.Msg})
	}
}

// offline handling: no response required
func (h *LoginHandler) handleOffline(ctx context.Context, conn core.IConnection, data json.RawMessage, assisted bool) {
	var req offlineData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	h.removeBinding(req.DeviceID, "")
	h.removeIndexes(ctx, req.NodeID, conn)
	if !assisted {
		// forward to parent
		if parent := h.selectAuthorityConn(ctx); parent != nil && (conn == nil || parent.ID() != conn.ID()) {
			h.forward(ctx, parent, actionAssistOffline, req)
		}
	}
}

// helpers
func (h *LoginHandler) saveBinding(ctx context.Context, conn core.IConnection, deviceID string, nodeID uint32, cred string) {
	role, perms := h.resolveRolePerms(nodeID)
	h.mu.Lock()
	h.whitelist[deviceID] = bindingRecord{NodeID: nodeID, Credential: cred, Role: role, Perms: perms}
	h.mu.Unlock()
	conn.SetMeta("nodeID", nodeID)
	conn.SetMeta("deviceID", deviceID)
	conn.SetMeta("role", role)
	conn.SetMeta("perms", perms)
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

func (h *LoginHandler) removeBinding(deviceID, cred string) (removed bool, mismatch bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec, ok := h.whitelist[deviceID]
	if !ok {
		return false, false
	}
	if cred != "" && rec.Credential != cred {
		return false, true
	}
	delete(h.whitelist, deviceID)
	return true, false
}

func (h *LoginHandler) removeIndexes(ctx context.Context, nodeID uint32, conn core.IConnection) {
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if updater, ok := cm.(interface {
				UpdateNodeIndex(uint32, core.IConnection)
				UpdateDeviceIndex(string, core.IConnection)
			}); ok {
				if nodeID != 0 {
					updater.UpdateNodeIndex(nodeID, nil)
				}
			}
		}
	}
}

func (h *LoginHandler) lookup(deviceID string) (bindingRecord, bool) {
	h.mu.RLock()
	rec, ok := h.whitelist[deviceID]
	h.mu.RUnlock()
	if ok && rec.Role == "" {
		rec.Role, rec.Perms = h.resolveRolePerms(rec.NodeID)
		h.mu.Lock()
		h.whitelist[deviceID] = rec
		h.mu.Unlock()
	}
	return rec, ok
}

func (h *LoginHandler) ensureNodeID(deviceID string) uint32 {
	h.mu.RLock()
	if rec, ok := h.whitelist[deviceID]; ok {
		h.mu.RUnlock()
		return rec.NodeID
	}
	h.mu.RUnlock()
	next := h.nextID.Add(1) - 1
	return next
}

func (h *LoginHandler) ensureCredential(deviceID string) string {
	h.mu.RLock()
	if rec, ok := h.whitelist[deviceID]; ok && rec.Credential != "" {
		h.mu.RUnlock()
		return rec.Credential
	}
	h.mu.RUnlock()
	token := generateCredential()
	return token
}

func generateCredential() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (h *LoginHandler) setPending(deviceID, connID string) {
	h.mu.Lock()
	h.pendingConn[deviceID] = connID
	h.mu.Unlock()
}

func (h *LoginHandler) popPending(deviceID string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.pendingConn[deviceID]
	if ok {
		delete(h.pendingConn, deviceID)
	}
	return id, ok
}

func (h *LoginHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if conn != nil {
			if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
				h.log.Warn("send resp failed", "err", err)
			}
			return
		}
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *LoginHandler) buildHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	var base core.IHeader = &header.HeaderTcp{}
	if reqHdr != nil {
		base = reqHdr.Clone()
	}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	return base.WithMajor(header.MajorOKResp).WithSubProto(2).WithSourceID(src).WithTargetID(0)
}

func (h *LoginHandler) forward(ctx context.Context, targetConn core.IConnection, action string, data any) {
	if targetConn == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv := core.ServerFromContext(ctx); srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	if nid, ok := targetConn.GetMeta("nodeID"); ok {
		if v, ok2 := nid.(uint32); ok2 {
			hdr.WithTargetID(v)
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, targetConn.ID(), hdr, payload)
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = targetConn.SendWithHeader(hdr, payload, codec)
}

func (h *LoginHandler) selectAuthority(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if h.authNode == 0 && srv.Config() != nil {
		if raw, ok := srv.Config().Get(coreconfig.KeyParentAddr); ok && raw != "" {
			// no explicit node id, use parent conn if exists
		}
		if raw, ok := srv.Config().Get("authority.node_id"); ok {
			// optional config
			if id, err := parseUint32(raw); err == nil && id != 0 {
				h.authNode = id
			}
		}
	}
	if h.authNode != 0 {
		if c, ok := srv.ConnManager().GetByNode(h.authNode); ok {
			return c
		}
	}
	if parent := h.selectAuthorityConn(ctx); parent != nil {
		return parent
	}
	return nil
}

func (h *LoginHandler) selectAuthorityConn(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := findParentConnLogin(srv.ConnManager()); ok {
		return c
	}
	return nil
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

func (h *LoginHandler) broadcast(ctx context.Context, src core.IConnection, action string, data any) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if src != nil && c.ID() == src.ID() {
			return true
		}
		if err := srv.Send(ctx, c.ID(), hdr, payload); err != nil {
			h.log.Warn("broadcast revoke failed", "conn", c.ID(), "err", err)
		}
		return true
	})
}

// get_perms / list_roles / perms_invalidate handlers
func (h *LoginHandler) handleGetPerms(ctx context.Context, conn core.IConnection, raw json.RawMessage) {
	var req permsQueryData
	if err := json.Unmarshal(raw, &req); err != nil || req.NodeID == 0 {
		h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 400, Msg: "invalid node_id"})
		return
	}
	role, perms, ok := h.lookupByNode(req.NodeID)
	if !ok || role == "" {
		h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 4404, Msg: "not found", NodeID: req.NodeID})
		return
	}
	h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 1, Msg: "ok", NodeID: req.NodeID, Role: role, Perms: perms})
}

func (h *LoginHandler) handleListRoles(ctx context.Context, conn core.IConnection) {
	snapshot := h.listRolePerms()
	data := struct {
		Code  int             `json:"code"`
		Msg   string          `json:"msg,omitempty"`
		Roles []rolePermEntry `json:"roles,omitempty"`
	}{
		Code:  1,
		Msg:   "ok",
		Roles: snapshot,
	}
	payload, _ := json.Marshal(data)
	msg := message{Action: actionListRolesResp, Data: payload}
	body, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, nil)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		_ = srv.Send(ctx, conn.ID(), hdr, body)
		return
	}
	if conn != nil {
		_ = conn.SendWithHeader(hdr, body, header.HeaderTcpCodec{})
	}
}

func (h *LoginHandler) handlePermsInvalidate(ctx context.Context, raw json.RawMessage) {
	var req invalidateData
	_ = json.Unmarshal(raw, &req)
	h.invalidateCache(req.NodeIDs)
	// 清理当前连接的 meta
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			targets := make(map[uint32]bool)
			for _, id := range req.NodeIDs {
				if id != 0 {
					targets[id] = true
				}
			}
			cm.Range(func(c core.IConnection) bool {
				if len(targets) == 0 {
					c.SetMeta("role", "")
					c.SetMeta("perms", []string(nil))
					return true
				}
				if nid, ok := c.GetMeta("nodeID"); ok {
					if v, ok2 := nid.(uint32); ok2 && targets[v] {
						c.SetMeta("role", "")
						c.SetMeta("perms", []string(nil))
					}
				}
				return true
			})
		}
		// 广播给子节点（不回父）
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if role, ok := c.GetMeta(core.MetaRoleKey); ok {
				if s, ok2 := role.(string); ok2 && s == core.RoleParent {
					return true
				}
			}
			msg := message{Action: actionPermsInvalidate, Data: raw}
			body, _ := json.Marshal(msg)
			hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(srv.NodeID()).WithTargetID(0)
			_ = srv.Send(ctx, c.ID(), hdr, body)
			return true
		})
	}
}

// auth/permission helpers
func (h *LoginHandler) loadAuthConfig(cfg core.IConfig) {
	if cfg == nil {
		return
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultRole); ok && strings.TrimSpace(raw) != "" {
		h.defaultRole = strings.TrimSpace(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultPerms); ok {
		h.defaultPerms = parseList(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthNodeRoles); ok {
		h.nodeRoles = parseNodeRoles(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRolePerms); ok {
		h.rolePerms = parseRolePerms(raw)
	}
}

func (h *LoginHandler) resolveRole(nodeID uint32) string {
	if nodeID != 0 {
		if r, ok := h.nodeRoles[nodeID]; ok && strings.TrimSpace(r) != "" {
			return strings.TrimSpace(r)
		}
	}
	return h.defaultRole
}

func (h *LoginHandler) resolvePerms(nodeID uint32) []string {
	role := h.resolveRole(nodeID)
	if perms, ok := h.rolePerms[role]; ok {
		return cloneSlice(perms)
	}
	return cloneSlice(h.defaultPerms)
}

func (h *LoginHandler) resolveRolePerms(nodeID uint32) (string, []string) {
	return h.resolveRole(nodeID), h.resolvePerms(nodeID)
}

func (h *LoginHandler) applyRolePerms(deviceID string, nodeID uint32, role string, perms []string, conn core.IConnection) {
	if role == "" && len(perms) == 0 {
		return
	}
	h.mu.Lock()
	rec, ok := h.whitelist[deviceID]
	if ok && rec.NodeID == nodeID {
		if role != "" {
			rec.Role = role
		}
		if perms != nil {
			rec.Perms = cloneSlice(perms)
		}
		h.whitelist[deviceID] = rec
	}
	h.mu.Unlock()
	if conn != nil {
		if role != "" {
			conn.SetMeta("role", role)
		}
		if perms != nil {
			conn.SetMeta("perms", cloneSlice(perms))
		}
	}
}

func (h *LoginHandler) lookupByNode(nodeID uint32) (role string, perms []string, ok bool) {
	h.mu.RLock()
	for _, rec := range h.whitelist {
		if rec.NodeID == nodeID {
			role = rec.Role
			perms = cloneSlice(rec.Perms)
			ok = true
			break
		}
	}
	h.mu.RUnlock()
	if ok && role != "" {
		return role, perms, true
	}
	role = h.resolveRole(nodeID)
	perms = h.resolvePerms(nodeID)
	if role == "" && len(perms) == 0 {
		return "", nil, false
	}
	return role, perms, true
}

func (h *LoginHandler) listRolePerms() []rolePermEntry {
	seen := make(map[uint32]bool)
	h.mu.RLock()
	for _, rec := range h.whitelist {
		if rec.NodeID == 0 {
			continue
		}
		seen[rec.NodeID] = true
	}
	h.mu.RUnlock()

	entries := make([]rolePermEntry, 0, len(seen)+len(h.nodeRoles))
	for nid := range seen {
		role, perms, ok := h.lookupByNode(nid)
		if !ok {
			continue
		}
		entries = append(entries, rolePermEntry{NodeID: nid, Role: role, Perms: perms})
	}
	for nid, role := range h.nodeRoles {
		if seen[nid] {
			continue
		}
		perms := h.resolvePerms(nid)
		entries = append(entries, rolePermEntry{NodeID: nid, Role: role, Perms: perms})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].NodeID < entries[j].NodeID })
	return entries
}

func (h *LoginHandler) invalidateCache(nodeIDs []uint32) {
	targets := make(map[uint32]bool)
	for _, id := range nodeIDs {
		if id != 0 {
			targets[id] = true
		}
	}
	h.mu.Lock()
	if len(targets) == 0 {
		for k, rec := range h.whitelist {
			rec.Role = ""
			rec.Perms = nil
			h.whitelist[k] = rec
		}
	} else {
		for k, rec := range h.whitelist {
			if targets[rec.NodeID] {
				rec.Role = ""
				rec.Perms = nil
				h.whitelist[k] = rec
			}
		}
	}
	h.mu.Unlock()
}

func parseList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseNodeRoles(raw string) map[uint32]string {
	m := make(map[uint32]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		id, err1 := strconv.ParseUint(strings.TrimSpace(kv[0]), 10, 32)
		role := strings.TrimSpace(kv[1])
		if err1 == nil && role != "" {
			m[uint32(id)] = role
		}
	}
	return m
}

func parseRolePerms(raw string) map[string][]string {
	m := make(map[string][]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		role := strings.TrimSpace(kv[0])
		if role == "" {
			continue
		}
		m[role] = parseList(kv[1])
	}
	return m
}

func cloneSlice[T any](src []T) []T {
	if len(src) == 0 {
		return nil
	}
	out := make([]T, len(src))
	copy(out, src)
	return out
}

// Errors placeholder
var (
	ErrInvalidAction = errors.New("invalid action")
)
