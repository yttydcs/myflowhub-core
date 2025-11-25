package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

const (
	actionVarSet           = "set"
	actionVarSetResp       = "set_resp"
	actionVarAssistSet     = "assist_set"
	actionVarGet           = "get"
	actionVarGetResp       = "get_resp"
	actionVarAssistGet     = "assist_get"
	actionVarAssistGetResp = "assist_get_resp"
	actionVarNotifyUpdate  = "notify_update"
	visibilityPublic       = "public"
	visibilityPrivate      = "private"
)

type varMessage struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

type setReq struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Visibility string `json:"visibility"`
	Type       string `json:"type,omitempty"`
	Notified   bool   `json:"notified,omitempty"` // 标记是否已向下通知 owner
}

type getReq struct {
	Name string `json:"name"`
}

type varResp struct {
	Code       int    `json:"code"`
	Msg        string `json:"msg,omitempty"`
	Name       string `json:"name,omitempty"`
	Value      string `json:"value,omitempty"`
	Owner      uint32 `json:"owner,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Type       string `json:"type,omitempty"`
}

type varRecord struct {
	Value      string
	Owner      uint32
	IsPublic   bool
	Visibility string
	Type       string
}

// VarStoreHandler implements SubProto=3 variable propagation with single set/get actions.
// - set: upsert variable (key = ownerID:name); only owner can create; others can update (with visibility rules); notify owner when updated by others.
// - get: read cached; forward upstream if missing.
type VarStoreHandler struct {
	log *slog.Logger

	mu      sync.RWMutex
	records map[string]varRecord       // key: ownerID:name
	pending map[string][]string        // name -> waiting connIDs for get responses
	cache   map[string]map[uint32]bool // name -> owners known
}

func NewVarStoreHandler(log *slog.Logger) *VarStoreHandler {
	if log == nil {
		log = slog.Default()
	}
	return &VarStoreHandler{
		log:     log,
		records: make(map[string]varRecord),
		pending: make(map[string][]string),
		cache:   make(map[string]map[uint32]bool),
	}
}

// AcceptCmd 声明 Cmd 帧在 target!=local 时也需要本地处理一次。
func (h *VarStoreHandler) AcceptCmd() bool { return true }

func (h *VarStoreHandler) SubProto() uint8 { return 3 }

func (h *VarStoreHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var msg varMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("varstore invalid payload", "err", err)
		return
	}
	act := strings.ToLower(strings.TrimSpace(msg.Action))
	switch act {
	case actionVarSet:
		h.handleSet(ctx, conn, hdr, msg.Data, false)
	case actionVarAssistSet:
		h.handleSet(ctx, conn, hdr, msg.Data, true)
	case actionVarGet:
		h.handleGet(ctx, conn, hdr, msg.Data, false)
	case actionVarAssistGet:
		h.handleGet(ctx, conn, hdr, msg.Data, true)
	case actionVarAssistGetResp:
		h.handleGetResp(ctx, msg.Data)
	default:
		h.log.Debug("unknown varstore action", "action", act)
	}
}

// set (upsert) handler
func (h *VarStoreHandler) handleSet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req setReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) {
		h.sendResp(ctx, conn, hdr, actionVarSetResp, varResp{Code: 400, Msg: "invalid set"})
		return
	}
	source := hdr.SourceID()
	existingRec, existingOwner, found := h.lookupAny(req.Name)

	// creation rule: only owner (SourceID) can create when not found
	if !found && source == 0 {
		h.sendResp(ctx, conn, hdr, actionVarSetResp, varResp{Code: 403, Msg: "owner required"})
		return
	}

	var owner uint32
	var rec varRecord
	if found {
		owner = existingOwner
		rec = existingRec
		// permission: private only owner can update
		if !rec.IsPublic && owner != source {
			h.sendResp(ctx, conn, hdr, actionVarSetResp, varResp{Code: 403, Msg: "forbidden"})
			return
		}
		rec.Value = req.Value
		if req.Type != "" {
			rec.Type = req.Type
		}
		if req.Visibility != "" {
			rec.IsPublic = strings.ToLower(req.Visibility) == visibilityPublic
			rec.Visibility = req.Visibility
		}
	} else {
		owner = source
		rec = varRecord{
			Value:      req.Value,
			Owner:      owner,
			IsPublic:   strings.ToLower(req.Visibility) == visibilityPublic,
			Visibility: req.Visibility,
			Type:       defaultType(req.Type),
		}
	}

	key := h.key(owner, req.Name)
	h.mu.Lock()
	h.records[key] = rec
	h.addOwnerCache(req.Name, owner)
	h.mu.Unlock()

	if !assisted {
		h.sendResp(ctx, conn, hdr, actionVarSetResp, varResp{Code: 1, Msg: "ok", Name: req.Name, Owner: owner, Visibility: visString(rec), Type: rec.Type})
	}

	// notify owner if updated by others
	if found && owner != 0 && owner != source && !req.Notified {
		h.notifyOwner(ctx, owner, req.Name, req.Value, rec.Type)
		req.Notified = true
	}

	// forward upstream
	if shouldForwardUp(ctx, hdr) {
		if parent := h.findParent(ctx); parent != nil {
			h.forward(ctx, parent, actionVarAssistSet, req, hdr.SourceID())
		}
	}
}

// get handler
func (h *VarStoreHandler) handleGet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) {
		h.sendResp(ctx, conn, hdr, actionVarGetResp, varResp{Code: 400, Msg: "invalid get"})
		return
	}
	if rec, owner, ok := h.lookupAny(req.Name); ok {
		if rec.IsPublic || owner == hdr.SourceID() {
			h.sendResp(ctx, conn, hdr, actionVarGetResp, varResp{
				Code:       1,
				Msg:        "ok",
				Name:       req.Name,
				Value:      rec.Value,
				Owner:      owner,
				Visibility: visString(rec),
				Type:       rec.Type,
			})
			return
		}
		h.sendResp(ctx, conn, hdr, actionVarGetResp, varResp{Code: 403, Msg: "forbidden"})
		return
	}
	if shouldForwardUp(ctx, hdr) {
		if parent := h.findParent(ctx); parent != nil {
			h.addPending(req.Name, conn.ID())
			h.forward(ctx, parent, actionVarAssistGet, req, hdr.SourceID())
			return
		}
		h.sendResp(ctx, conn, hdr, actionVarGetResp, varResp{Code: 404, Msg: "not found"})
		return
	}
	// 无法上行且未命中
	h.sendResp(ctx, conn, hdr, actionVarGetResp, varResp{Code: 404, Msg: "not found"})
}

// assist_get_resp fan-out
func (h *VarStoreHandler) handleGetResp(ctx context.Context, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.Name == "" {
		return
	}
	connIDs := h.popPending(resp.Name)
	srv := core.ServerFromContext(ctx)
	for _, id := range connIDs {
		if srv == nil {
			continue
		}
		if c, ok := srv.ConnManager().Get(id); ok {
			h.sendResp(ctx, c, nil, actionVarGetResp, resp)
		}
	}
	if resp.Code == 1 {
		h.mu.Lock()
		h.records[h.key(resp.Owner, resp.Name)] = varRecord{
			Value:      resp.Value,
			Owner:      resp.Owner,
			IsPublic:   strings.ToLower(resp.Visibility) == visibilityPublic,
			Visibility: resp.Visibility,
			Type:       resp.Type,
		}
		h.addOwnerCache(resp.Name, resp.Owner)
		h.mu.Unlock()
	}
}

// helpers
func (h *VarStoreHandler) lookupAny(name string) (varRecord, uint32, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if owners, ok := h.cache[name]; ok {
		for owner := range owners {
			if rec, ok2 := h.records[h.key(owner, name)]; ok2 {
				return rec, owner, true
			}
		}
	}
	for k, v := range h.records {
		if strings.HasSuffix(k, ":"+name) {
			if owner, err := strconv.ParseUint(strings.SplitN(k, ":", 2)[0], 10, 32); err == nil {
				return v, uint32(owner), true
			}
		}
	}
	return varRecord{}, 0, false
}

func (h *VarStoreHandler) addOwnerCache(name string, owner uint32) {
	if owner == 0 || name == "" {
		return
	}
	if _, ok := h.cache[name]; !ok {
		h.cache[name] = make(map[uint32]bool)
	}
	h.cache[name][owner] = true
}

func (h *VarStoreHandler) key(owner uint32, name string) string {
	return strconv.FormatUint(uint64(owner), 10) + ":" + name
}

func (h *VarStoreHandler) addPending(name, connID string) {
	h.mu.Lock()
	h.pending[name] = append(h.pending[name], connID)
	h.mu.Unlock()
}

func (h *VarStoreHandler) popPending(name string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.pending[name]
	delete(h.pending, name)
	return conns
}

func (h *VarStoreHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data varResp) {
	msg := varMessage{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr, action)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		_ = srv.Send(ctx, conn.ID(), hdr, payload)
		return
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *VarStoreHandler) buildHeader(ctx context.Context, reqHdr core.IHeader, action string) core.IHeader {
	var base core.IHeader = &header.HeaderTcp{}
	if reqHdr != nil {
		base = reqHdr.Clone()
	}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	major := base.Major()
	if action == actionVarGetResp || action == actionVarAssistGetResp || action == actionVarNotifyUpdate {
		major = header.MajorCmd
	} else {
		major = header.MajorOKResp
	}
	return base.WithMajor(major).WithSubProto(3).WithSourceID(src).WithTargetID(base.TargetID())
}

func (h *VarStoreHandler) forward(ctx context.Context, target core.IConnection, action string, data any, srcID uint32) {
	if target == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := varMessage{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if srcID != 0 {
			hdr.WithSourceID(srcID)
		} else {
			hdr.WithSourceID(srv.NodeID())
		}
	}
	if meta, ok := target.GetMeta("nodeID"); ok {
		if v, ok2 := meta.(uint32); ok2 {
			hdr.WithTargetID(v)
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, target.ID(), hdr, payload)
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = target.SendWithHeader(hdr, payload, codec)
}

func (h *VarStoreHandler) findParent(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := findParentConn(srv.ConnManager()); ok {
		return c
	}
	return nil
}

func (h *VarStoreHandler) notifyOwner(ctx context.Context, owner uint32, name, value, typ string) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || owner == 0 {
		return
	}
	cm := srv.ConnManager()
	resp := varResp{Code: 1, Msg: "updated", Name: name, Value: value, Owner: owner, Type: typ}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(srv.NodeID()).
		WithTargetID(owner)
	payloadData, _ := json.Marshal(resp)
	msg := varMessage{Action: actionVarNotifyUpdate, Data: payloadData}
	payload, _ := json.Marshal(msg)

	// direct to owner if connected
	if c, ok := cm.GetByNode(owner); ok {
		_ = srv.Send(ctx, c.ID(), hdr, payload)
		return
	}
	// upward to parent if direct not found
	cm.Range(func(c core.IConnection) bool {
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				_ = srv.Send(ctx, c.ID(), hdr, payload)
				return false
			}
		}
		return true
	})
}

func visString(rec varRecord) string {
	if rec.IsPublic {
		return visibilityPublic
	}
	if rec.Visibility != "" {
		return rec.Visibility
	}
	return visibilityPrivate
}

func validVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '_' {
			continue
		}
		return false
	}
	return true
}

func defaultType(typ string) string {
	if strings.TrimSpace(typ) == "" {
		return "string"
	}
	return typ
}

func shouldForwardUp(ctx context.Context, hdr core.IHeader) bool {
	if hdr == nil {
		return true
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return true
	}
	local := srv.NodeID()
	tgt := hdr.TargetID()
	// 仅在目标是本地或 0（上送父）时由 handler 主动上行；否则预路由已转发，无需重复。
	return tgt == 0 || tgt == local
}
