package core

import (
	"context"
	"io"
	"net"
)

// IHeader 定义协议头的通用接口：提供当前 TCP 头部的全部只读访问方法，并提供修改方法（流式）。
// 注意：具体实现需使用指针接收者实现修改方法，以便原地修改。
type IHeader interface {
	// 读取方法
	Major() uint8
	SubProto() uint8
	SourceID() uint32
	TargetID() uint32
	GetFlags() uint8
	GetMsgID() uint32
	GetTimestamp() uint32
	PayloadLength() uint32
	GetReserved() uint16

	// 修改方法（返回 IHeader 以支持链式调用）
	WithMajor(uint8) IHeader
	WithSubProto(uint8) IHeader
	WithSourceID(uint32) IHeader
	WithTargetID(uint32) IHeader
	WithFlags(uint8) IHeader
	WithMsgID(uint32) IHeader
	WithTimestamp(uint32) IHeader
	WithPayloadLength(uint32) IHeader
	WithReserved(uint16) IHeader
	// Clone 返回该头部的深拷贝，实现需确保可在多协程安全复用。
	Clone() IHeader
}

// IConfig 配置接口：用于读取服务配置，由 IServer 的具体实现持有。
// 尽量保持最小化约束，后续可以扩展。
type IConfig interface {
	// Get 按 key 读取配置，返回值与是否存在。
	Get(key string) (string, bool)
}

// IServer 服务接口：用于启动/停止服务，并持有 IConfig 与 IConnectionManager。
type IServer interface {
	// Start 启动服务；阻塞/非阻塞由实现决定。
	Start(ctx context.Context) error
	// Stop 优雅停止服务。
	Stop(ctx context.Context) error

	// Config 返回服务配置。
	Config() IConfig
	// ConnManager 返回连接管理器。
	ConnManager() IConnectionManager
	// Process 返回当前处理管线。
	Process() IProcess
	// HeaderCodec 返回编解码器。
	HeaderCodec() IHeaderCodec
	// NodeID 返回当前节点 ID。
	NodeID() uint32
	// Send 将 header+payload 发送给指定连接，并触发处理钩子。
	Send(ctx context.Context, connID string, hdr IHeader, payload []byte) error
}

// IProcess 处理管线接口：Server 在连接建立/收发/关闭时调用。
type IProcess interface {
	// OnListen 在连接成功加入管理器后触发，可用于初始化元数据。
	OnListen(conn IConnection)
	// OnReceive 在收到一帧数据后触发。
	OnReceive(ctx context.Context, conn IConnection, hdr IHeader, payload []byte)
	// OnSend 在发送一帧数据前触发，可用于审计/修改。
	OnSend(ctx context.Context, conn IConnection, hdr IHeader, payload []byte) error
	// OnClose 在连接移除/关闭后触发。
	OnClose(conn IConnection)
}

// ISubProcess 子协议处理接口：Dispatcher 根据 SubProto 路由到对应实现。
type ISubProcess interface {
	// SubProto 返回该 handler 负责的子协议编号（0-63）。
	SubProto() uint8
	// OnReceive 处理指定子协议的数据帧。
	OnReceive(ctx context.Context, conn IConnection, hdr IHeader, payload []byte)
}

// IHeaderCodec 头编解码接口：不同协议实现各自的头部序列化与反序列化。
type IHeaderCodec interface {
	// Encode 将 header 与 payload 编码为单个帧字节切片。
	Encode(header IHeader, payload []byte) ([]byte, error)
	// Decode 从流中解码出一帧，返回头与负载；可能阻塞直到读到完整帧。
	Decode(r io.Reader) (IHeader, []byte, error)
}

// ReceiveHandler 接收事件回调：当连接收到一帧数据时触发。
type ReceiveHandler func(conn IConnection, header IHeader, payload []byte)

// ISender 发送者接口：抽象发送能力，可被连接或其他组件复用。
type ISender interface {
	// Send 发送原始字节（按具体协议决定是否已编码为完整帧）。
	Send(data []byte) error
	// SendWithHeader 使用 Header 与 HeaderCodec 进行编码后发送（可选实现）。
	SendWithHeader(header IHeader, payload []byte, codec IHeaderCodec) error
}

// IConnection 连接接口：封装实际连接与其元数据，支持发送、接收事件、关闭与元数据的读写。
type IConnection interface {
	ISender
	// ID 返回连接的唯一标识。
	ID() string
	// Close 关闭连接。
	Close() error
	// OnReceive 注册接收事件回调（幂等/覆盖策略由实现定义）。
	OnReceive(h ReceiveHandler)

	// 元数据相关。
	SetMeta(key string, val any)
	GetMeta(key string) (any, bool)
	Metadata() map[string]any

	// 地址信息（可选）。
	LocalAddr() net.Addr
	RemoteAddr() net.Addr

	// Reader 返回与该连接绑定的读取者（如有）。
	Reader() IReader
	// SetReader 绑定读取器，Server 在启动读循环前调用。
	SetReader(IReader)
	// DispatchReceive 由 Reader 调用，用于触发接收事件。
	DispatchReceive(IHeader, []byte)
	// RawConn 返回底层 net.Conn，供 Reader 读取。
	RawConn() net.Conn
}

// IConnectionManager 连接管理器：由 IServer 持有，用于集中管理连接。
type IConnectionManager interface {
	// Add 添加连接。
	Add(conn IConnection) error
	// Remove 按 ID 移除连接。
	Remove(id string) error
	// Get 按 ID 获取连接。
	Get(id string) (IConnection, bool)
	// Range 遍历所有连接；返回 false 以提前终止遍历。
	Range(func(IConnection) bool)
	// Count 当前连接数量。
	Count() int
	// Broadcast 向所有连接广播原始字节。
	Broadcast(data []byte) error
	// CloseAll 关闭所有连接。
	CloseAll() error
	// SetHooks 设置连接增删时的回调（可选）。
	SetHooks(ConnectionHooks)
	// GetByNode 按节点 ID 获取连接（若支持）。
	GetByNode(nodeID uint32) (IConnection, bool)
	// UpdateNodeIndex 更新节点索引映射（用于登录/登出）。
	UpdateNodeIndex(nodeID uint32, conn IConnection)
}

// ConnectionHooks 连接事件钩子。
type ConnectionHooks struct {
	OnAdd    func(IConnection)
	OnRemove func(IConnection)
}

// IListener 监听者接口：每种协议对应一个监听者，用于接受新连接并加入连接管理器。
type IListener interface {
	// Protocol 返回协议标识（例如 "tcp"、"ws" 等）。
	Protocol() string
	// Listen 启动监听；实现需要在接受到新连接时创建 IConnection 并添加到 cm。
	Listen(ctx context.Context, cm IConnectionManager) error
	// Close 停止监听并释放资源。
	Close() error
	// Addr 返回监听地址（可选，由实现决定是否可用）。
	Addr() net.Addr
}

// IReader 读取者接口：负责从连接中按协议与 IHeaderCodec 读取数据帧。
type IReader interface {
	// ReadLoop 使用提供的 IHeaderCodec 持续从 conn 读取帧，并在合适时机触发 IConnection 的接收事件。
	ReadLoop(ctx context.Context, conn IConnection, hc IHeaderCodec) error
}

// 移除 IRoutingHeader，统一使用 IHeader 作为通用头接口。
// 任何协议头实现 IHeader 后，即可被路由、分发与编码器统一处理。
