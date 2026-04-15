package process

// 本文件覆盖 Core 框架中与 `header_router` 相关的行为。

import (
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

type routerStubLink struct{}

func (routerStubLink) ID() string                 { return "link-1" }
func (routerStubLink) Pipe() core.IPipe           { return nil }
func (routerStubLink) Close() error               { return nil }
func (routerStubLink) SetMeta(string, any)        {}
func (routerStubLink) GetMeta(string) (any, bool) { return nil, false }
func (routerStubLink) Metadata() map[string]any   { return nil }
func (routerStubLink) LocalAddr() net.Addr        { return nil }
func (routerStubLink) RemoteAddr() net.Addr       { return nil }

func TestHeaderRouterDecide(t *testing.T) {
	router := NewHeaderRouter()
	link := routerStubLink{}

	mkHdr := func(major, sub uint8, source, target uint32) core.IHeader {
		var hdr header.HeaderTcp
		hdr.WithMajor(major).WithSubProto(sub).WithSourceID(source).WithTargetID(target)
		return &hdr
	}

	cases := []struct {
		name string
		hdr  core.IHeader
		want RouteDecisionKind
	}{
		{name: "drop source zero non auth", hdr: mkHdr(header.MajorMsg, 3, 0, 1), want: RouteDecisionDrop},
		{name: "auth subproto enters hop dispatch", hdr: mkHdr(header.MajorMsg, 2, 0, 1), want: RouteDecisionHopDispatch},
		{name: "major cmd enters hop dispatch", hdr: mkHdr(header.MajorCmd, 5, 10, 999), want: RouteDecisionHopDispatch},
		{name: "broadcast children", hdr: mkHdr(header.MajorMsg, 5, 10, 0), want: RouteDecisionBroadcastChildren},
		{name: "fast forward remote target", hdr: mkHdr(header.MajorOKResp, 5, 10, 9), want: RouteDecisionFastForward},
		{name: "local dispatch", hdr: mkHdr(header.MajorMsg, 5, 10, 7), want: RouteDecisionLocalDispatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := router.Decide(7, link, tc.hdr)
			if got.Kind != tc.want {
				t.Fatalf("Decide() kind=%s, want=%s", got.Kind.String(), tc.want.String())
			}
		})
	}
}
