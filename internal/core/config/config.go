package config

import "sync"

// MapConfig 是最简单的 IConfig 实现：在内存 map 中存储键值。
type MapConfig struct {
	mu   sync.RWMutex
	data map[string]string
}

const (
	KeyProcChannelCount     = "process.channel_count"
	KeyProcWorkersPerChan   = "process.workers_per_channel"
	KeyProcChannelBuffer    = "process.channel_buffer"
	KeySendChannelCount     = "send.channel_count"
	KeySendWorkersPerChan   = "send.workers_per_channel"
	KeySendChannelBuffer    = "send.channel_buffer"
	KeyRoutingForwardRemote = "routing.forward_remote"
	KeyProcQueueStrategy    = "process.queue_strategy" // conn|subproto|source_target|roundrobin
	KeyDefaultForwardEnable = "routing.default_forward_enable"
	KeyDefaultForwardTarget = "routing.default_forward_target"
	KeyDefaultForwardMap    = "routing.default_forward_map"
)

// NewMap 使用传入 map 构建 MapConfig；若 data 为空则初始化为空 map。
func NewMap(data map[string]string) *MapConfig {
	mc := &MapConfig{data: make(map[string]string)}
	for k, v := range data {
		mc.data[k] = v
	}
	ensureDefault(mc.data, KeyProcChannelCount, "1")
	ensureDefault(mc.data, KeyProcWorkersPerChan, "1")
	ensureDefault(mc.data, KeyProcChannelBuffer, "64")
	ensureDefault(mc.data, KeySendChannelCount, "1")
	ensureDefault(mc.data, KeySendWorkersPerChan, "1")
	ensureDefault(mc.data, KeySendChannelBuffer, "64")
	ensureDefault(mc.data, KeyRoutingForwardRemote, "true")
	ensureDefault(mc.data, KeyProcQueueStrategy, "conn")
	ensureDefault(mc.data, KeyDefaultForwardEnable, "false")
	ensureDefault(mc.data, KeyDefaultForwardTarget, "")
	ensureDefault(mc.data, KeyDefaultForwardMap, "")
	return mc
}

func ensureDefault(m map[string]string, key, val string) {
	if _, ok := m[key]; !ok {
		m[key] = val
	}
}

// Get 实现 core.IConfig；返回值与是否存在。
func (m *MapConfig) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	return val, ok
}

// Set 允许在运行期更新配置。
func (m *MapConfig) Set(key, val string) {
	m.mu.Lock()
	m.data[key] = val
	m.mu.Unlock()
}
