package tests

import (
	"net"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/process"
)

// mockConnQS implements core.IConnection minimally for queue strategy tests.
type mockConnQS struct{ id string }

func (m *mockConnQS) ID() string                                                         { return m.id }
func (m *mockConnQS) Close() error                                                       { return nil }
func (m *mockConnQS) OnReceive(_ core.ReceiveHandler)                                    {}
func (m *mockConnQS) SetMeta(_ string, _ any)                                            {}
func (m *mockConnQS) GetMeta(_ string) (any, bool)                                       { return nil, false }
func (m *mockConnQS) Metadata() map[string]any                                           { return nil }
func (m *mockConnQS) LocalAddr() net.Addr                                                { return dummyAddr("local") }
func (m *mockConnQS) RemoteAddr() net.Addr                                               { return dummyAddr("remote") }
func (m *mockConnQS) Reader() core.IReader                                               { return nil }
func (m *mockConnQS) SetReader(_ core.IReader)                                           {}
func (m *mockConnQS) DispatchReceive(_ core.IHeader, _ []byte)                           {}
func (m *mockConnQS) RawConn() net.Conn                                                  { return nil }
func (m *mockConnQS) Send(_ []byte) error                                                { return nil }
func (m *mockConnQS) SendWithHeader(_ core.IHeader, _ []byte, _ core.IHeaderCodec) error { return nil }

type dummyAddr string

func (d dummyAddr) Network() string { return "mock" }
func (d dummyAddr) String() string  { return string(d) }

// Compile-time assertion
var _ core.IConnection = (*mockConnQS)(nil)

func TestConnHashStrategyDeterministic(t *testing.T) {
	st := process.ConnHashStrategy{}
	c1 := &mockConnQS{id: "A"}
	c2 := &mockConnQS{id: "B"}
	for i := 0; i < 5; i++ {
		if st.SelectQueue(c1, nil, 8) != st.SelectQueue(c1, nil, 8) {
			t.Fatal("conn hash not deterministic for A")
		}
		if st.SelectQueue(c2, nil, 8) != st.SelectQueue(c2, nil, 8) {
			t.Fatal("conn hash not deterministic for B")
		}
	}
	// 不强制不同，但记录潜在碰撞
	if st.SelectQueue(c1, nil, 8) == st.SelectQueue(c2, nil, 8) {
		t.Log("warning: A and B mapped to same queue; acceptable but unlikely")
	}
}

func TestSubProtoStrategy(t *testing.T) {
	st := process.SubProtoStrategy{}
	for sp := 0; sp < 10; sp++ {
		h := &header.HeaderTcp{}
		h.WithSubProto(uint8(sp))
		q := st.SelectQueue(nil, h, 4)
		if q != sp%4 {
			t.Fatalf("subproto strategy mismatch: sub=%d got=%d", sp, q)
		}
	}
}

func TestRoundRobinStrategy(t *testing.T) {
	st := &process.RoundRobinStrategy{}
	seen := make(map[int]int)
	for i := 0; i < 15; i++ {
		q := st.SelectQueue(nil, nil, 3)
		if q < 0 || q >= 3 {
			t.Fatalf("queue out of range: %d", q)
		}
		seen[q]++
	}
	if len(seen) != 3 {
		t.Fatalf("roundrobin did not utilize all queues: %+v", seen)
	}
}
