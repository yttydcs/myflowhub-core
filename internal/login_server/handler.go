package login_server

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

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

// AuthorityHandler implements SubProto=2 as the authoritative login server backed by persistent storage.
type AuthorityHandler struct {
	log   *slog.Logger
	store Store

	cacheMu sync.RWMutex
	cache   map[string]bindingRecord

	defaultRole  string
	defaultPerms []string
	nodeRoles    map[uint32]string
	rolePerms    map[string][]string
}

func NewAuthorityHandler(store Store, log *slog.Logger) *AuthorityHandler {
	return NewAuthorityHandlerWithConfig(store, nil, log)
}

func NewAuthorityHandlerWithConfig(store Store, cfg core.IConfig, log *slog.Logger) *AuthorityHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &AuthorityHandler{
		log:         log,
		store:       store,
		cache:       make(map[string]bindingRecord),
		defaultRole: "node",
		nodeRoles:   make(map[uint32]string),
		rolePerms:   make(map[string][]string),
	}
	h.loadAuthConfig(cfg)
	return h
}

func (h *AuthorityHandler) SubProto() uint8 { return 2 }

func (h *AuthorityHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
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
	case actionLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, false)
	case actionAssistLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, true)
	case actionAssistQueryCred:
		h.handleAssistQuery(ctx, conn, hdr, msg.Data)
	case actionRevoke:
		h.handleRevoke(ctx, conn, hdr, msg.Data)
	case actionOffline, actionAssistOffline:
		h.handleOffline(msg.Data)
	case actionGetPerms:
		h.handleGetPerms(ctx, conn, msg.Data)
	case actionListRoles:
		h.handleListRoles(ctx, conn)
	case actionPermsInvalidate:
		h.handlePermsInvalidate(msg.Data)
	default:
		h.log.Debug("unknown login action", "action", act)
	}
}

func (h *AuthorityHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{Code: 400, Msg: "invalid register data"})
		return
	}
	nodeID, cred, err := h.store.UpsertDevice(ctx, req.DeviceID)
	if err != nil {
		h.log.Error("register failed", "err", err, "device", req.DeviceID)
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{Code: 500, Msg: "internal error"})
		return
	}
	h.remember(req.DeviceID, nodeID, cred, "", nil)
	h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     nodeID,
		Credential: cred,
		Role:       h.resolveRole(nodeID),
		Perms:      h.resolvePerms(nodeID),
	})
}

func (h *AuthorityHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req loginData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 400, Msg: "invalid login data"})
		return
	}
	rec, ok := h.lookup(req.DeviceID)
	if !ok {
		nodeID, cred, found, err := h.store.GetDevice(ctx, req.DeviceID)
		if err != nil {
			h.log.Error("login lookup failed", "err", err, "device", req.DeviceID)
			h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 500, Msg: "internal error"})
			return
		}
		if !found {
			h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 4001, Msg: "invalid credential"})
			return
		}
		rec = bindingRecord{NodeID: nodeID, Credential: cred}
		h.remember(req.DeviceID, rec.NodeID, rec.Credential, "", nil)
	}
	if rec.Credential != req.Credential {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 4001, Msg: "invalid credential"})
		return
	}
	h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     rec.NodeID,
		Credential: rec.Credential,
		Role:       rec.Role,
		Perms:      rec.Perms,
	})
}

func (h *AuthorityHandler) handleAssistQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req queryCredData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 400, Msg: "invalid query"})
		return
	}
	rec, ok := h.lookup(req.DeviceID)
	if !ok {
		nodeID, cred, found, err := h.store.GetDevice(ctx, req.DeviceID)
		if err != nil {
			h.log.Error("assist query failed", "err", err, "device", req.DeviceID)
			h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 500, Msg: "internal error"})
			return
		}
		if !found {
			h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 4001, Msg: "not found"})
			return
		}
		rec = bindingRecord{NodeID: nodeID, Credential: cred}
		h.remember(req.DeviceID, rec.NodeID, rec.Credential, "", nil)
	}
	h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     rec.NodeID,
		Credential: rec.Credential,
		Role:       rec.Role,
		Perms:      rec.Perms,
	})
}

func (h *AuthorityHandler) handleRevoke(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	nodeID, removed, mismatch, err := h.store.DeleteDevice(ctx, req.DeviceID, req.Credential)
	if err != nil {
		h.log.Error("revoke failed", "err", err, "device", req.DeviceID)
		return
	}
	h.forget(req.DeviceID)
	if mismatch {
		h.sendResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 4402, Msg: "credential mismatch", DeviceID: req.DeviceID, NodeID: nodeID})
		return
	}
	if removed {
		h.sendResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: nodeID})
	}
}

func (h *AuthorityHandler) handleOffline(data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	h.forget(req.DeviceID)
}

// get_perms / list_roles / perms_invalidate handlers
func (h *AuthorityHandler) handleGetPerms(ctx context.Context, conn core.IConnection, raw json.RawMessage) {
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

func (h *AuthorityHandler) handleListRoles(ctx context.Context, conn core.IConnection) {
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

func (h *AuthorityHandler) handlePermsInvalidate(raw json.RawMessage) {
	var req invalidateData
	_ = json.Unmarshal(raw, &req)
	h.invalidateCache(req.NodeIDs)
}

func (h *AuthorityHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
			h.log.Warn("send resp failed", "err", err)
		}
		return
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *AuthorityHandler) buildHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
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

func (h *AuthorityHandler) remember(deviceID string, nodeID uint32, credential string, role string, perms []string) {
	if role == "" {
		role = h.resolveRole(nodeID)
	}
	if perms == nil {
		perms = h.resolvePerms(nodeID)
	}
	h.cacheMu.Lock()
	h.cache[deviceID] = bindingRecord{NodeID: nodeID, Credential: credential, Role: role, Perms: perms}
	h.cacheMu.Unlock()
}

func (h *AuthorityHandler) forget(deviceID string) {
	h.cacheMu.Lock()
	delete(h.cache, deviceID)
	h.cacheMu.Unlock()
}

func (h *AuthorityHandler) lookup(deviceID string) (bindingRecord, bool) {
	h.cacheMu.RLock()
	rec, ok := h.cache[deviceID]
	h.cacheMu.RUnlock()
	if ok && rec.Role == "" {
		rec.Role = h.resolveRole(rec.NodeID)
		rec.Perms = h.resolvePerms(rec.NodeID)
		h.cacheMu.Lock()
		h.cache[deviceID] = rec
		h.cacheMu.Unlock()
	}
	return rec, ok
}

func chooseAction(assisted bool, assistedAct, normalAct string) string {
	if assisted {
		return assistedAct
	}
	return normalAct
}

// auth/permission helpers
func (h *AuthorityHandler) loadAuthConfig(cfg core.IConfig) {
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

func (h *AuthorityHandler) resolveRole(nodeID uint32) string {
	if nodeID != 0 {
		if r, ok := h.nodeRoles[nodeID]; ok && strings.TrimSpace(r) != "" {
			return strings.TrimSpace(r)
		}
	}
	return h.defaultRole
}

func (h *AuthorityHandler) resolvePerms(nodeID uint32) []string {
	role := h.resolveRole(nodeID)
	if perms, ok := h.rolePerms[role]; ok {
		return cloneSlice(perms)
	}
	return cloneSlice(h.defaultPerms)
}

func (h *AuthorityHandler) lookupByNode(nodeID uint32) (role string, perms []string, ok bool) {
	h.cacheMu.RLock()
	for _, rec := range h.cache {
		if rec.NodeID == nodeID {
			role = rec.Role
			perms = cloneSlice(rec.Perms)
			ok = true
			break
		}
	}
	h.cacheMu.RUnlock()
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

func (h *AuthorityHandler) listRolePerms() []rolePermEntry {
	seen := make(map[uint32]bool)
	h.cacheMu.RLock()
	for _, rec := range h.cache {
		if rec.NodeID == 0 {
			continue
		}
		seen[rec.NodeID] = true
	}
	h.cacheMu.RUnlock()

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

func (h *AuthorityHandler) invalidateCache(nodeIDs []uint32) {
	targets := make(map[uint32]bool)
	for _, id := range nodeIDs {
		if id != 0 {
			targets[id] = true
		}
	}
	h.cacheMu.Lock()
	if len(targets) == 0 {
		for k, rec := range h.cache {
			rec.Role = ""
			rec.Perms = nil
			h.cache[k] = rec
		}
	} else {
		for k, rec := range h.cache {
			if targets[rec.NodeID] {
				rec.Role = ""
				rec.Perms = nil
				h.cache[k] = rec
			}
		}
	}
	h.cacheMu.Unlock()
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
