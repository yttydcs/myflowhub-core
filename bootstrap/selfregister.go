package bootstrap

// 本文件承载 Core 框架中与 `selfregister` 相关的通用逻辑。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/listener/tcp_listener"
)

// SelfRegisterOptions 配置自注册行为。
type SelfRegisterOptions struct {
	ParentAddr  string
	Dial        func(context.Context) (core.IConnection, error)
	SelfID      string
	JoinPermit  string
	Timeout     time.Duration
	DialTimeout time.Duration
	DoLogin     bool
	Logger      *slog.Logger
}

// RegisterStatusError reports a non-approved register outcome.
type RegisterStatusError struct {
	Code      int
	Status    string
	RequestID string
	Reason    string
	Msg       string
}

// Error 将 register 返回的状态细化成便于日志和上层 UI 理解的错误文本。
func (e *RegisterStatusError) Error() string {
	if e == nil {
		return "register failed"
	}
	detail := strings.TrimSpace(e.Reason)
	if detail == "" {
		detail = strings.TrimSpace(e.Msg)
	}
	if detail == "" {
		detail = "register failed"
	}
	status := strings.TrimSpace(e.Status)
	if status == "" {
		return "register failed: " + detail
	}
	if e.RequestID != "" {
		return fmt.Sprintf("register %s (request_id=%s): %s", status, e.RequestID, detail)
	}
	return fmt.Sprintf("register %s: %s", status, detail)
}

// SelfRegister 通过 SubProto=2 的 register/login 获取 node_id（旧版会返回 credential，现已不要求）。
// 适用于有父节点且未预设 node_id 的 Hub/节点。
func SelfRegister(ctx context.Context, opts SelfRegisterOptions) (uint32, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Dial == nil && opts.ParentAddr == "" {
		return 0, "", errors.New("parent address required")
	}
	if opts.SelfID == "" {
		return 0, "", errors.New("self id required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 5 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	conn, err := dialSelfRegisterConn(cctx, opts)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()

	codec := header.HeaderTcpCodec{}
	msgID := uint32(1)

	// register
	regData := map[string]any{"device_id": opts.SelfID}
	if strings.TrimSpace(opts.JoinPermit) != "" {
		regData["join_permit"] = strings.TrimSpace(opts.JoinPermit)
	}
	regPayload, _ := json.Marshal(map[string]any{
		"action": "register",
		"data":   regData,
	})
	regHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(0).
		WithTargetID(0).
		WithMsgID(msgID)
	if err := sendFrame(cctx, conn, codec, regHdr, regPayload); err != nil {
		return 0, "", err
	}
	rHdr, rBody, err := recvFrame(cctx, conn, codec)
	if err != nil {
		return 0, "", err
	}
	nodeID, cred, err := parseRegisterResp(rHdr, rBody)
	if err != nil {
		return 0, "", err
	}

	if opts.DoLogin {
		msgID++
		loginPayload, _ := json.Marshal(map[string]any{
			"action": "login",
			"data": map[string]any{
				"device_id": opts.SelfID,
				// 新版登录需签名支持，这里仅保留占位，实际流程应在调用处构造签名。
			},
		})
		loginHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(2).
			WithSourceID(nodeID).
			WithTargetID(0).
			WithMsgID(msgID)
		if err := sendFrame(cctx, conn, codec, loginHdr, loginPayload); err != nil {
			return 0, "", err
		}
		_, loginResp, err := recvFrame(cctx, conn, codec)
		if err != nil {
			return 0, "", err
		}
		if err := assertLoginOK(loginResp); err != nil {
			return 0, "", err
		}
	}
	opts.Logger.Info("self register done", "node_id", nodeID, "self_id", opts.SelfID)
	return nodeID, cred, nil
}

// dialSelfRegisterConn 优先复用调用方注入的 dialer，否则退回旧版 TCP 直连。
func dialSelfRegisterConn(ctx context.Context, opts SelfRegisterOptions) (core.IConnection, error) {
	if opts.Dial != nil {
		conn, err := opts.Dial(ctx)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, errors.New("self register dial returned nil connection")
		}
		return conn, nil
	}

	dialer := net.Dialer{Timeout: opts.DialTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", opts.ParentAddr)
	if err != nil {
		return nil, err
	}
	_ = raw.SetDeadline(time.Now().Add(opts.Timeout))
	return tcp_listener.NewTCPConnection(raw), nil
}

// sendFrame 在 context 保护下发送一帧 bootstrap 请求。
func sendFrame(ctx context.Context, conn core.IConnection, codec header.HeaderTcpCodec, hdr core.IHeader, payload []byte) error {
	return runConnOp(ctx, conn, func() error {
		return conn.SendWithHeader(hdr, payload, codec)
	})
}

// recvFrame 同步读取一帧响应，并在超时时主动关闭连接打断阻塞读取。
func recvFrame(ctx context.Context, conn core.IConnection, codec header.HeaderTcpCodec) (core.IHeader, []byte, error) {
	type decodeResult struct {
		hdr  core.IHeader
		body []byte
		err  error
	}

	done := make(chan decodeResult, 1)
	go func() {
		hdr, body, err := codec.Decode(conn.Pipe())
		done <- decodeResult{hdr: hdr, body: body, err: err}
	}()

	select {
	case result := <-done:
		return result.hdr, result.body, result.err
	case <-ctx.Done():
		_ = conn.Close()
		return nil, nil, ctx.Err()
	}
}

// runConnOp 为单次连接操作补上超时感知，避免 bootstrap 卡在底层 I/O。
func runConnOp(ctx context.Context, conn core.IConnection, op func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- op()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = conn.Close()
		return ctx.Err()
	}
}

// parseRegisterResp 兼容旧新两种 register 响应形态，并把 pending/rejected 提升为显式错误。
func parseRegisterResp(_ core.IHeader, body []byte) (uint32, string, error) {
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return 0, "", err
	}
	var resp struct {
		Code      int    `json:"code"`
		NodeID    uint32 `json:"node_id"`
		Msg       string `json:"msg"`
		Status    string `json:"status"`
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return 0, "", err
	}
	status := strings.ToLower(strings.TrimSpace(resp.Status))
	switch status {
	case "approved":
		if resp.Code != 1 || resp.NodeID == 0 {
			return 0, "", &RegisterStatusError{
				Code:      resp.Code,
				Status:    resp.Status,
				RequestID: resp.RequestID,
				Reason:    coalesceRegisterReason(resp.Reason, resp.Msg, "register approved without node_id"),
				Msg:       resp.Msg,
			}
		}
		return resp.NodeID, "", nil
	case "pending", "rejected":
		return 0, "", &RegisterStatusError{
			Code:      resp.Code,
			Status:    resp.Status,
			RequestID: resp.RequestID,
			Reason:    resp.Reason,
			Msg:       resp.Msg,
		}
	case "":
		if resp.Code == 1 && resp.NodeID != 0 {
			return resp.NodeID, "", nil
		}
	default:
		if resp.Code == 1 && resp.NodeID != 0 {
			return resp.NodeID, "", nil
		}
	}
	return 0, "", &RegisterStatusError{
		Code:      resp.Code,
		Status:    resp.Status,
		RequestID: resp.RequestID,
		Reason:    resp.Reason,
		Msg:       resp.Msg,
	}
}

// assertLoginOK 只校验 bootstrap 登录占位请求是否拿到成功 code。
func assertLoginOK(body []byte) error {
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return err
	}
	if resp.Code != 1 {
		return errors.New("login failed: " + resp.Msg)
	}
	return nil
}

// coalesceRegisterReason 选取第一条非空原因文本，减少错误分支里的重复判断。
func coalesceRegisterReason(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
