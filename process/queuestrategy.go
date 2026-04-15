package process

// 本文件承载 Core 框架中与 `queuestrategy` 相关的通用逻辑。

import (
	"hash/fnv"
	"strings"
	"sync/atomic"

	core "github.com/yttydcs/myflowhub-core"
)

// QueueSelectStrategy 定义事件映射到队列的策略接口。
// 实现需要在 queueCount<=0 或 queueCount==1 时返回 0。
// 必须保证返回值在 [0, queueCount) 范围。
type QueueSelectStrategy interface {
	SelectQueue(conn core.IConnection, hdr core.IHeader, queueCount int) int
	Name() string
}

// ConnHashStrategy 根据连接 ID 做 FNV32 哈希；若无连接退化到子协议。保证同连接顺序性。
type ConnHashStrategy struct{}

// Name 返回配置字符串，供配置解析与观测输出复用。
func (ConnHashStrategy) Name() string { return "conn" }

// SelectQueue 尽量让同一连接固定落到同一队列，避免同连接消息乱序。
func (ConnHashStrategy) SelectQueue(conn core.IConnection, hdr core.IHeader, n int) int {
	if n <= 1 {
		return 0
	}
	if conn != nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(conn.ID()))
		return int(h.Sum32() % uint32(n))
	}
	if hdr != nil {
		return int(hdr.SubProto()) % n
	}
	return 0
}

// SubProtoStrategy 按子协议号取模，适合高并发多子协议场景。
type SubProtoStrategy struct{}

// Name 返回该策略在配置中的名字。
func (SubProtoStrategy) Name() string { return "subproto" }

// SelectQueue 用子协议号分片，适合不同子协议工作负载差异明显的场景。
func (SubProtoStrategy) SelectQueue(_ core.IConnection, hdr core.IHeader, n int) int {
	if n <= 1 {
		return 0
	}
	if hdr == nil {
		return 0
	}
	return int(hdr.SubProto()) % n
}

// SourceTargetStrategy 按 Source/Target 组合哈希，保持同一对通信顺序。
type SourceTargetStrategy struct{}

// Name 返回该策略在配置中的名字。
func (SourceTargetStrategy) Name() string { return "source_target" }

// SelectQueue 用源/目标节点对做哈希，让同一通信双方的帧尽量在同一队列串行处理。
func (SourceTargetStrategy) SelectQueue(_ core.IConnection, hdr core.IHeader, n int) int {
	if n <= 1 {
		return 0
	}
	if hdr == nil {
		return 0
	}
	h := fnv.New64a()
	buf := make([]byte, 16)
	// 将 source/target 放入 buf
	s := hdr.SourceID()
	t := hdr.TargetID()
	for i := 0; i < 8; i++ {
		buf[i] = byte(s >> (56 - 8*i))
	}
	for i := 0; i < 8; i++ {
		buf[8+i] = byte(t >> (56 - 8*i))
	}
	_, _ = h.Write(buf)
	return int(h.Sum64() % uint64(n))
}

// RoundRobinStrategy 简单轮询（不保证顺序性）。
type RoundRobinStrategy struct{ counter uint64 }

// Name 返回该策略在配置中的名字。
func (RoundRobinStrategy) Name() string { return "roundrobin" }

// SelectQueue 用原子计数轮询分配队列，换取更平均的负载，但不承诺同连接顺序。
func (r *RoundRobinStrategy) SelectQueue(_ core.IConnection, _ core.IHeader, n int) int {
	if n <= 1 {
		return 0
	}
	return int(nextCounter(&r.counter) % uint64(n))
}

// nextCounter 原子自增计数。
func nextCounter(c *uint64) uint64 { return atomic.AddUint64(c, 1) - 1 }

// StrategyFromConfig 根据配置字符串创建策略实例；未知值返回默认 ConnHashStrategy。
func StrategyFromConfig(raw string) QueueSelectStrategy {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "subproto":
		return SubProtoStrategy{}
	case "source_target":
		return SourceTargetStrategy{}
	case "roundrobin":
		return &RoundRobinStrategy{}
	default:
		return ConnHashStrategy{}
	}
}
