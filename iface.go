package core

import (
	"context"
	"encoding/json"
	"io"
	"net"

	"github.com/yttydcs/myflowhub-core/eventbus"
)

// IHeader 定义协议头的通用接口：
// - 提供对 TCP 头部的全部只读访问方法
// - 提供修改方法（返回自身，便于链式调用）
// - Clone 需返回深拷贝，保证多协程复用安全
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

	// Clone 返回该头部的深拷贝
	Clone() IHeader
}

// IConfig 配置接口：用于读取服务配置，供 IServer 持有。
// 保持最小化约束，后续可扩展；Merge 用于覆盖合并其他配置。
type IConfig interface {
	// Get 按 key 读取配置，返回值与是否存在
	Get(key string) (string, bool)
	// Merge 将其他配置的键值覆盖到当前配置，返回合并结果
	Merge(other IConfig) IConfig
	// Set 运行期设置配置项（可选实现）
	Set(key, val string)
	// Keys 返回全部配置键（可选实现）
	Keys() []string
}

// IServer 服务接口：用于启动/停止服务，并持有核心组件。
type IServer interface {
	// Start 启动服务；阻塞/非阻塞由实现决定
	Start(ctx context.Context) error
	// Stop 优雅停止服务
	Stop(ctx context.Context) error

	// Config 返回服务配置
	Config() IConfig
	// ConnManager 返回连接管理器
	ConnManager() IConnectionManager
	// Process 返回当前处理管线
	Process() IProcess
	// HeaderCodec 返回编解码器
	HeaderCodec() IHeaderCodec
	// NodeID 返回当前节点 ID
	NodeID() uint32
	// UpdateNodeID 运行期更新当前节点 ID
	UpdateNodeID(uint32)
	// EventBus 返回事件总线实例
	EventBus() eventbus.IBus
	// Send 将 header+payload 发送给指定连接，并触发处理钩子
	Send(ctx context.Context, connID string, hdr IHeader, payload []byte) error
}

// IProcess 处理管线接口：Server 在连接监听/收发/关闭时调用。
type IProcess interface {
	// OnListen 在连接成功加入管理器后触发，可用于初始化元数据
	OnListen(conn IConnection)
	// OnReceive 在收到一帧数据后触发
	OnReceive(ctx context.Context, conn IConnection, hdr IHeader, payload []byte)
	// OnSend 在发送一帧数据前触发，可用于审计/修改
	OnSend(ctx context.Context, conn IConnection, hdr IHeader, payload []byte) error
	// OnClose 在连接移除/关闭后触发
	OnClose(conn IConnection)
}

// ISubProcess 子协议处理接口：Dispatcher 根据 SubProto 路由到对应实现。
type ISubProcess interface {
	// SubProto 返回该 handler 负责的子协议编号（0-63）
	SubProto() uint8
	// OnReceive 处理指定子协议的数据帧
	OnReceive(ctx context.Context, conn IConnection, hdr IHeader, payload []byte)
	// Init 执行子协议处理器的初始化，返回是否成功
	Init() bool
	// AcceptCmd 声明 Cmd 帧在目标非本地时是否仍需本地处理一次
	AcceptCmd() bool
	// AllowSourceMismatch 是否允许 SourceID 与连接元数据的 nodeID 不一致
	AllowSourceMismatch() bool
}

// SubProcessAction 抽象子协议内单个动作。
type SubProcessAction interface {
	Name() string
	RequireAuth() bool
	Handle(context.Context, IConnection, IHeader, json.RawMessage)
}

// IHeaderCodec 头编解码接口：不同协议实现各自的头部序列化与反序列化。
type IHeaderCodec interface {
	// Encode 将 header 与 payload 编码为单个帧字节切片
	Encode(header IHeader, payload []byte) ([]byte, error)
	// Decode 从流中解码出一帧，返回头与负载；可能阻塞直到读到完整帧
	Decode(r io.Reader) (IHeader, []byte, error)
}

// ReceiveHandler 接收事件回调：当连接收到一帧数据时触发。
type ReceiveHandler func(conn IConnection, header IHeader, payload []byte)

// ISender 发送者接口：抽象发送能力，可被连接或其他组件复用。
type ISender interface {
	// Send 发送原始字节（按具体协议决定是否已编码为完整帧）
	Send(data []byte) error
	// SendWithHeader 使用 Header 和 HeaderCodec 进行编码后发送
	SendWithHeader(header IHeader, payload []byte, codec IHeaderCodec) error
}

// IConnection 连接接口：封装实际连接与其元数据，支持发送、接收事件、关闭与元数据的读写。
type IConnection interface {
	ISender
	// ID 返回连接的唯一标识
	ID() string
	// Close 关闭连接
	Close() error
	// OnReceive 注册接收事件回调
	OnReceive(h ReceiveHandler)

	// 元数据相关
	SetMeta(key string, val any)
	GetMeta(key string) (any, bool)
	Metadata() map[string]any

	// 地址信息（可选）
	LocalAddr() net.Addr
	RemoteAddr() net.Addr

	// Reader 相关
	Reader() IReader
	SetReader(IReader)
	DispatchReceive(IHeader, []byte)
	RawConn() net.Conn
}

// IConnectionManager 连接管理器：由 IServer 持有，用于集中管理连接。
type IConnectionManager interface {
	// Add 添加连接
	Add(conn IConnection) error
	// Remove 按 ID 移除连接
	Remove(id string) error
	// Get 按 ID 获取连接
	Get(id string) (IConnection, bool)
	// Range 遍历所有连接；返回 false 可提前终止遍历
	Range(func(IConnection) bool)
	// Count 当前连接数量
	Count() int
	// Broadcast 向所有连接广播原始字节
	Broadcast(data []byte) error
	// CloseAll 关闭所有连接
	CloseAll() error
	// SetHooks 设置连接增删时的回调（可选）
	SetHooks(ConnectionHooks)
	// GetByNode 按节点 ID 获取连接（若支持）
	GetByNode(nodeID uint32) (IConnection, bool)
	// UpdateNodeIndex 更新节点索引映射（用于登陆/登出）
	UpdateNodeIndex(nodeID uint32, conn IConnection)
	// AddNodeIndex 追加节点索引（允许一个连接挂多个 nodeID）
	AddNodeIndex(nodeID uint32, conn IConnection)
	// RemoveNodeIndex 按 nodeID 删除索引
	RemoveNodeIndex(nodeID uint32)
	// GetByDevice 按设备 ID 获取连接（若支持）
	GetByDevice(deviceID string) (IConnection, bool)
	// UpdateDeviceIndex 更新设备索引映射（用于登陆/登出）
	UpdateDeviceIndex(deviceID string, conn IConnection)
}

// ConnectionHooks 连接事件钩子。
type ConnectionHooks struct {
	OnAdd    func(IConnection)
	OnRemove func(IConnection)
}

// IListener 监听者接口：每种协议对应一个监听者，用于接受新连接并加入连接管理器。
type IListener interface {
	// Protocol 返回协议标识（例如 "tcp"、"ws" 等）
	Protocol() string
	// Listen 启动监听；实现需要在接受到新连接时创建 IConnection 并添加到 cm
	Listen(ctx context.Context, cm IConnectionManager) error
	// Close 停止监听并释放资源
	Close() error
	// Addr 返回监听地址（可选，由实现决定是否可用）
	Addr() net.Addr
}

// IReader 读取者接口：负责从连接中按协议与 IHeaderCodec 读取数据帧。
type IReader interface {
	// ReadLoop 使用提供的 IHeaderCodec 持续从 conn 读取帧，并触发接收事件
	ReadLoop(ctx context.Context, conn IConnection, hc IHeaderCodec) error
}
