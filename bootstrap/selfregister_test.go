package bootstrap

import (
	"encoding/json"
	"errors"
	"testing"
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
