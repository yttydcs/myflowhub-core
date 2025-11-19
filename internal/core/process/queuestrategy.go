package process

import (
	"hash/fnv"
	"strings"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
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

func (ConnHashStrategy) Name() string { return "conn" }
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

func (SubProtoStrategy) Name() string { return "subproto" }
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

func (SourceTargetStrategy) Name() string { return "source_target" }
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

func (RoundRobinStrategy) Name() string { return "roundrobin" }

// 这里不做原子自增（可后续优化）；当前 dispatcher 事件入队本身可跨 goroutine，需线程安全。
func (r *RoundRobinStrategy) SelectQueue(_ core.IConnection, _ core.IHeader, n int) int {
	if n <= 1 {
		return 0
	}
	// 使用原子自增
	// 由于结构体不可变，这里设计为指针使用（dispatcher 设置时用 &RoundRobinStrategy{}）
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
