package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

// SelfRegisterOptions 配置自注册行为。
type SelfRegisterOptions struct {
	ParentAddr  string
	SelfID      string
	Timeout     time.Duration
	DialTimeout time.Duration
	DoLogin     bool
	Logger      *slog.Logger
}

// SelfRegister 通过 SubProto=2 的 register/login 获取 node_id 与 credential。
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
	regPayload, _ := json.Marshal(map[string]any{
		"action": "register",
		"data":   map[string]any{"device_id": opts.SelfID},
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
				"device_id":  opts.SelfID,
				"credential": cred,
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
		Code       int    `json:"code"`
		NodeID     uint32 `json:"node_id"`
		Credential string `json:"credential"`
		Msg        string `json:"msg"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return 0, "", err
	}
	if resp.Code != 1 || resp.NodeID == 0 || resp.Credential == "" {
		return 0, "", errors.New("register failed: " + resp.Msg)
	}
	return resp.NodeID, resp.Credential, nil
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
