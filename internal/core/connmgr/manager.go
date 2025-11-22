package connmgr

import (
	"errors"
	"sync"

	core "MyFlowHub-Core/internal/core"
)

// Manager 是内存连接管理器实现。
type Manager struct {
	mu        sync.RWMutex
	conns     map[string]core.IConnection
	hooks     core.ConnectionHooks
	nodeIndex map[uint32]core.IConnection
	devIndex  map[string]core.IConnection
}

func New() *Manager {
	return &Manager{
		conns:     make(map[string]core.IConnection),
		nodeIndex: make(map[uint32]core.IConnection),
		devIndex:  make(map[string]core.IConnection),
	}
}

// SetHooks 注册连接钩子。
func (m *Manager) SetHooks(h core.ConnectionHooks) {
	m.mu.Lock()
	m.hooks = h
	m.mu.Unlock()
}

func (m *Manager) Add(conn core.IConnection) error {
	if conn == nil {
		return errors.New("conn nil")
	}
	m.mu.Lock()
	if _, ok := m.conns[conn.ID()]; ok {
		m.mu.Unlock()
		return errors.New("conn exists")
	}
	m.conns[conn.ID()] = conn
	m.addNodeIndexLocked(conn)
	m.addDeviceIndexLocked(conn)
	h := m.hooks
	m.mu.Unlock()
	if h.OnAdd != nil {
		h.OnAdd(conn)
	}
	return nil
}

func (m *Manager) addNodeIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
	if nodeID, ok := conn.GetMeta("nodeID"); ok {
		if nid, ok2 := nodeID.(uint32); ok2 && nid != 0 {
			m.nodeIndex[nid] = conn
		}
	}
}

func (m *Manager) addDeviceIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
	if devID, ok := conn.GetMeta("deviceID"); ok {
		if s, ok2 := devID.(string); ok2 && s != "" {
			m.devIndex[s] = conn
		}
	}
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	conn, ok := m.conns[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("conn not found")
	}
	m.removeNodeIndexLocked(conn)
	m.removeDeviceIndexLocked(conn)
	delete(m.conns, id)
	h := m.hooks
	m.mu.Unlock()
	if h.OnRemove != nil {
		h.OnRemove(conn)
	}
	return conn.Close()
}

func (m *Manager) removeNodeIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
	if nodeID, ok := conn.GetMeta("nodeID"); ok {
		if nid, ok2 := nodeID.(uint32); ok2 && nid != 0 {
			if existing, ok3 := m.nodeIndex[nid]; ok3 && existing == conn {
				delete(m.nodeIndex, nid)
			}
		}
	}
}

func (m *Manager) removeDeviceIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
	// 清理所有指向该连接的设备索引，防止未存 meta 时泄漏
	for dev, c := range m.devIndex {
		if c == conn {
			delete(m.devIndex, dev)
		}
	}
}

func (m *Manager) Get(id string) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.conns[id]
	return conn, ok
}

func (m *Manager) GetByNode(id uint32) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.nodeIndex[id]
	return c, ok
}

func (m *Manager) UpdateNodeIndex(nodeID uint32, conn core.IConnection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if nodeID == 0 {
		return
	}
	if conn == nil {
		if existing, ok := m.nodeIndex[nodeID]; ok {
			if existing != nil {
				delete(m.nodeIndex, nodeID)
			}
		}
		return
	}
	m.nodeIndex[nodeID] = conn
}

func (m *Manager) GetByDevice(devID string) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.devIndex[devID]
	return c, ok
}

func (m *Manager) UpdateDeviceIndex(devID string, conn core.IConnection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if devID == "" {
		return
	}
	if conn == nil {
		if existing, ok := m.devIndex[devID]; ok {
			if existing != nil {
				delete(m.devIndex, devID)
			}
		}
		return
	}
	m.devIndex[devID] = conn
}

func (m *Manager) Range(fn func(core.IConnection) bool) {
	m.mu.RLock()
	conns := make([]core.IConnection, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.RUnlock()
	for _, c := range conns {
		if !fn(c) {
			return
		}
	}
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

func (m *Manager) Broadcast(data []byte) error {
	m.Range(func(c core.IConnection) bool {
		_ = c.Send(data)
		return true
	})
	return nil
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	conns := make([]core.IConnection, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.conns = make(map[string]core.IConnection)
	h := m.hooks
	m.mu.Unlock()
	for _, c := range conns {
		if h.OnRemove != nil {
			h.OnRemove(c)
		}
		_ = c.Close()
	}
	return nil
}
