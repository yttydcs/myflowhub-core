package multi_listener

// Context: This file provides shared Core framework logic around listener_test.

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

type fakeListener struct {
	proto string
	addr  net.Addr

	closeCalled atomic.Bool

	listen func(ctx context.Context) error
}

func (l *fakeListener) Protocol() string { return l.proto }
func (l *fakeListener) Addr() net.Addr   { return l.addr }

func (l *fakeListener) Listen(ctx context.Context, _ core.IConnectionManager) error {
	if l.listen == nil {
		<-ctx.Done()
		return nil
	}
	return l.listen(ctx)
}

func (l *fakeListener) Close() error {
	l.closeCalled.Store(true)
	return nil
}

func TestMultiListener_ClosesOthersOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	bad := &fakeListener{
		proto: "bad",
		listen: func(context.Context) error {
			return errors.New("boom")
		},
	}
	block := &fakeListener{
		proto: "block",
		listen: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
	}

	ml, err := New(bad, block)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = ml.Listen(ctx, connmgr.New())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !block.closeCalled.Load() {
		t.Fatalf("expected other listener to be closed")
	}
}
