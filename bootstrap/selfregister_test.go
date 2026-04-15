package bootstrap

// 本文件覆盖 Core 框架中与 `selfregister` 相关的行为。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/listener/tcp_listener"
)

func TestParseRegisterRespLegacySuccess(t *testing.T) {
	body := mustRegisterResp(t, map[string]any{
		"code":    1,
		"node_id": 7,
		"msg":     "ok",
	})
	nodeID, _, err := parseRegisterResp(nil, body)
	if err != nil {
		t.Fatalf("parseRegisterResp: %v", err)
	}
	if nodeID != 7 {
		t.Fatalf("unexpected node id: got %d want 7", nodeID)
	}
}

func TestParseRegisterRespApprovedStatus(t *testing.T) {
	body := mustRegisterResp(t, map[string]any{
		"code":       1,
		"node_id":    9,
		"status":     "approved",
		"request_id": "req-1",
	})
	nodeID, _, err := parseRegisterResp(nil, body)
	if err != nil {
		t.Fatalf("parseRegisterResp: %v", err)
	}
	if nodeID != 9 {
		t.Fatalf("unexpected node id: got %d want 9", nodeID)
	}
}

func TestParseRegisterRespPending(t *testing.T) {
	body := mustRegisterResp(t, map[string]any{
		"code":       202,
		"status":     "pending",
		"request_id": "req-pending",
		"reason":     "approval required",
	})
	_, _, err := parseRegisterResp(nil, body)
	var statusErr *RegisterStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected RegisterStatusError, got %v", err)
	}
	if statusErr.Status != "pending" {
		t.Fatalf("unexpected status: got %q want %q", statusErr.Status, "pending")
	}
	if statusErr.RequestID != "req-pending" {
		t.Fatalf("unexpected request id: got %q want %q", statusErr.RequestID, "req-pending")
	}
}

func TestParseRegisterRespRejected(t *testing.T) {
	body := mustRegisterResp(t, map[string]any{
		"code":   4001,
		"status": "rejected",
		"reason": "join permit device_id mismatch",
	})
	_, _, err := parseRegisterResp(nil, body)
	var statusErr *RegisterStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected RegisterStatusError, got %v", err)
	}
	if statusErr.Status != "rejected" {
		t.Fatalf("unexpected status: got %q want %q", statusErr.Status, "rejected")
	}
}

func TestSelfRegisterUsesInjectedDialer(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer server.Close()

		codec := header.HeaderTcpCodec{}
		reqHdr, reqBody, err := codec.Decode(server)
		if err != nil {
			errCh <- err
			return
		}
		msg, err := decodeBootstrapEnvelope(reqBody)
		if err != nil {
			errCh <- err
			return
		}
		if got := msg.Action; got != "register" {
			errCh <- fmt.Errorf("unexpected action: got %q want %q", got, "register")
			return
		}
		if got := msg.Data["device_id"]; got != "device-dialer" {
			errCh <- fmt.Errorf("unexpected device_id: got %v want %q", got, "device-dialer")
			return
		}
		if got := msg.Data["join_permit"]; got != "permit-1" {
			errCh <- fmt.Errorf("unexpected join_permit: got %v want %q", got, "permit-1")
			return
		}

		respPayload, err := json.Marshal(map[string]any{
			"action": "register_resp",
			"data": map[string]any{
				"code":    1,
				"status":  "approved",
				"node_id": 9,
			},
		})
		if err != nil {
			errCh <- err
			return
		}
		respHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(2).
			WithTargetID(reqHdr.SourceID()).
			WithMsgID(reqHdr.GetMsgID())
		frame, err := codec.Encode(respHdr, respPayload)
		if err != nil {
			errCh <- err
			return
		}
		_, err = server.Write(frame)
		errCh <- err
	}()

	dialCalled := false
	nodeID, _, err := SelfRegister(context.Background(), SelfRegisterOptions{
		SelfID:     "device-dialer",
		Timeout:    2 * time.Second,
		JoinPermit: "permit-1",
		Dial: func(context.Context) (core.IConnection, error) {
			dialCalled = true
			return tcp_listener.NewTCPConnection(client), nil
		},
	})
	if err != nil {
		t.Fatalf("SelfRegister: %v", err)
	}
	if !dialCalled {
		t.Fatalf("expected injected dialer to be used")
	}
	if nodeID != 9 {
		t.Fatalf("unexpected node id: got %d want %d", nodeID, 9)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("bootstrap server: %v", err)
	}
}

func TestSelfRegisterReturnsRegisterStatusErrorFromInjectedDialer(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer server.Close()

		codec := header.HeaderTcpCodec{}
		reqHdr, reqBody, err := codec.Decode(server)
		if err != nil {
			errCh <- err
			return
		}
		msg, err := decodeBootstrapEnvelope(reqBody)
		if err != nil {
			errCh <- err
			return
		}
		if got := msg.Action; got != "register" {
			errCh <- fmt.Errorf("unexpected action: got %q want %q", got, "register")
			return
		}
		if got := msg.Data["device_id"]; got != "device-pending" {
			errCh <- fmt.Errorf("unexpected device_id: got %v want %q", got, "device-pending")
			return
		}

		respPayload, err := json.Marshal(map[string]any{
			"action": "register_resp",
			"data": map[string]any{
				"code":       202,
				"status":     "pending",
				"request_id": "req-1",
				"reason":     "approval required",
			},
		})
		if err != nil {
			errCh <- err
			return
		}
		respHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(2).
			WithTargetID(reqHdr.SourceID()).
			WithMsgID(reqHdr.GetMsgID())
		frame, err := codec.Encode(respHdr, respPayload)
		if err != nil {
			errCh <- err
			return
		}
		_, err = server.Write(frame)
		errCh <- err
	}()

	_, _, err := SelfRegister(context.Background(), SelfRegisterOptions{
		SelfID:  "device-pending",
		Timeout: 2 * time.Second,
		Dial: func(context.Context) (core.IConnection, error) {
			return tcp_listener.NewTCPConnection(client), nil
		},
	})
	if err == nil {
		t.Fatalf("expected pending register to fail")
	}

	var statusErr *RegisterStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected RegisterStatusError, got %T", err)
	}
	if statusErr.Status != "pending" {
		t.Fatalf("unexpected status: got %q want %q", statusErr.Status, "pending")
	}
	if statusErr.RequestID != "req-1" {
		t.Fatalf("unexpected request id: got %q want %q", statusErr.RequestID, "req-1")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("bootstrap server: %v", err)
	}
}

type bootstrapEnvelope struct {
	Action string         `json:"action"`
	Data   map[string]any `json:"data"`
}

func decodeBootstrapEnvelope(payload []byte) (bootstrapEnvelope, error) {
	var msg bootstrapEnvelope
	if err := json.Unmarshal(payload, &msg); err != nil {
		return bootstrapEnvelope{}, err
	}
	return msg, nil
}

func mustRegisterResp(t *testing.T, data map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"action": "register_resp",
		"data":   data,
	})
	if err != nil {
		t.Fatalf("marshal register resp: %v", err)
	}
	return body
}
