package core

// 本文件承载 Core 框架中与 `link` 相关的通用逻辑。

import "net"

// ILink is the minimal transport-facing link abstraction.
//
// Design goals:
// - expose only the byte-stream pipe + minimal routing/lifecycle metadata;
// - avoid leaking transport-specific raw connection types (TCP/RFCOMM/etc.);
// - allow the current IConnection model to degrade into a compatibility adapter.
type ILink interface {
	ID() string
	Pipe() IPipe
	Close() error

	SetMeta(key string, val any)
	GetMeta(key string) (any, bool)
	Metadata() map[string]any

	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

// LinkHooks defines lifecycle callbacks for link-oriented managers.
type LinkHooks struct {
	OnAdd    func(ILink)
	OnRemove func(ILink)
}

// ILinkManager is the new link-oriented management interface.
//
// Notes:
//   - compatibility implementations may still be backed by IConnection internally;
//   - Update* methods return error so compatibility managers can reject unsupported
//     non-IConnection link implementations explicitly instead of failing silently.
type ILinkManager interface {
	AddLink(link ILink) error
	RemoveLink(id string) error
	GetLink(id string) (ILink, bool)
	RangeLinks(func(ILink) bool)
	Count() int
	CloseAll() error
	SetLinkHooks(LinkHooks)

	GetLinkByNode(nodeID uint32) (ILink, bool)
	UpdateNodeLink(nodeID uint32, link ILink) error
	AddNodeLink(nodeID uint32, link ILink) error
	RemoveNodeLink(nodeID uint32)

	GetLinkByDevice(deviceID string) (ILink, bool)
	UpdateDeviceLink(deviceID string, link ILink) error
}

// LinkFromConnection returns the link view of an existing connection.
func LinkFromConnection(conn IConnection) ILink {
	return conn
}

// ConnectionFromLink returns the compatibility connection view when available.
func ConnectionFromLink(link ILink) (IConnection, bool) {
	conn, ok := link.(IConnection)
	return conn, ok
}
