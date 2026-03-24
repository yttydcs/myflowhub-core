package bootstrap

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
)

// SelfRegisterOptions 配置自注册行为。
type SelfRegisterOptions struct {
	ParentAddr  string
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
	if opts.ParentAddr == "" {
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

	dialer := net.Dialer{Timeout: opts.DialTimeout}
	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	conn, err := dialer.DialContext(cctx, "tcp", opts.ParentAddr)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(opts.Timeout))

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
	if err := sendFrame(conn, codec, regHdr, regPayload); err != nil {
		return 0, "", err
	}
	rHdr, rBody, err := codec.Decode(conn)
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
		if err := sendFrame(conn, codec, loginHdr, loginPayload); err != nil {
			return 0, "", err
		}
		_, loginResp, err := codec.Decode(conn)
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

func sendFrame(conn net.Conn, codec header.HeaderTcpCodec, hdr core.IHeader, payload []byte) error {
	frame, err := codec.Encode(hdr, payload)
	if err != nil {
		return err
	}
	_, err = conn.Write(frame)
	return err
}

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

func coalesceRegisterReason(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
