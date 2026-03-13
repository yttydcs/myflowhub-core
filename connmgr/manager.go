package connmgr

import (
	"errors"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

var (
	errCompatLinkRequiresConnection = errors.New("compat link manager requires link to implement IConnection")

	_ core.IConnectionManager = (*Manager)(nil)
	_ core.ILinkManager       = (*Manager)(nil)
)

// Manager is the in-memory connection/link manager implementation.
//
// Compatibility notes:
//   - v1 storage is still centered on IConnection;
//   - link-oriented APIs are exposed on top of the same storage to support the
//     incremental transition from Connection -> Link without breaking callers.
type Manager struct {
	mu        sync.RWMutex
	conns     map[string]core.IConnection
	hooks     core.ConnectionHooks
	linkHooks core.LinkHooks
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

// SetHooks registers connection lifecycle hooks.
func (m *Manager) SetHooks(h core.ConnectionHooks) {
	m.mu.Lock()
	m.hooks = h
	m.mu.Unlock()
}

// SetLinkHooks registers link lifecycle hooks.
func (m *Manager) SetLinkHooks(h core.LinkHooks) {
	m.mu.Lock()
	m.linkHooks = h
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
	lh := m.linkHooks
	m.mu.Unlock()
	if h.OnAdd != nil {
		h.OnAdd(conn)
	}
	if lh.OnAdd != nil {
		lh.OnAdd(conn)
	}
	return nil
}

// AddLink adds a link through the compatibility manager.
func (m *Manager) AddLink(link core.ILink) error {
	conn, ok := core.ConnectionFromLink(link)
	if !ok {
		return errCompatLinkRequiresConnection
	}
	return m.Add(conn)
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
	lh := m.linkHooks
	m.mu.Unlock()
	if h.OnRemove != nil {
		h.OnRemove(conn)
	}
	if lh.OnRemove != nil {
		lh.OnRemove(conn)
	}
	return conn.Close()
}

// RemoveLink removes a link through the compatibility manager.
func (m *Manager) RemoveLink(id string) error {
	return m.Remove(id)
}

func (m *Manager) removeNodeIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
	for nid, c := range m.nodeIndex {
		if c == conn {
			delete(m.nodeIndex, nid)
		}
	}
}

func (m *Manager) removeDeviceIndexLocked(conn core.IConnection) {
	if conn == nil {
		return
	}
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

// GetLink returns the link view of a managed connection.
func (m *Manager) GetLink(id string) (core.ILink, bool) {
	conn, ok := m.Get(id)
	if !ok {
		return nil, false
	}
	return conn, true
}

func (m *Manager) GetByNode(id uint32) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.nodeIndex[id]
	return c, ok
}

// GetLinkByNode returns the link view for a node mapping.
func (m *Manager) GetLinkByNode(id uint32) (core.ILink, bool) {
	conn, ok := m.GetByNode(id)
	if !ok {
		return nil, false
	}
	return conn, true
}

func (m *Manager) UpdateNodeIndex(nodeID uint32, conn core.IConnection) {
	if nodeID == 0 {
		return
	}
	isDirectBind := func(c core.IConnection) bool {
		if c == nil {
			return false
		}
		if v, ok := c.GetMeta("nodeID"); ok {
			if nid, ok2 := asUint32(v); ok2 && nid != 0 && nid == nodeID {
				return true
			}
		}
		return false
	}

	var oldDirectConnID string
	m.mu.Lock()
	if conn == nil {
		delete(m.nodeIndex, nodeID)
		m.mu.Unlock()
		return
	}
	existing := m.nodeIndex[nodeID]
	m.nodeIndex[nodeID] = conn
	if existing != nil && existing != conn && isDirectBind(conn) && isDirectBind(existing) {
		oldDirectConnID = existing.ID()
	}
	m.mu.Unlock()

	if oldDirectConnID != "" {
		_ = m.Remove(oldDirectConnID)
	}
}

// UpdateNodeLink updates node->link mapping through the compatibility manager.
func (m *Manager) UpdateNodeLink(nodeID uint32, link core.ILink) error {
	if link == nil {
		m.UpdateNodeIndex(nodeID, nil)
		return nil
	}
	conn, ok := core.ConnectionFromLink(link)
	if !ok {
		return errCompatLinkRequiresConnection
	}
	m.UpdateNodeIndex(nodeID, conn)
	return nil
}

func (m *Manager) AddNodeIndex(nodeID uint32, conn core.IConnection) {
	m.UpdateNodeIndex(nodeID, conn)
}

// AddNodeLink appends node->link mapping through the compatibility manager.
func (m *Manager) AddNodeLink(nodeID uint32, link core.ILink) error {
	return m.UpdateNodeLink(nodeID, link)
}

func (m *Manager) RemoveNodeIndex(nodeID uint32) {
	m.UpdateNodeIndex(nodeID, nil)
}

// RemoveNodeLink removes a node->link mapping.
func (m *Manager) RemoveNodeLink(nodeID uint32) {
	m.RemoveNodeIndex(nodeID)
}

func (m *Manager) GetByDevice(devID string) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.devIndex[devID]
	return c, ok
}

// GetLinkByDevice returns the link view for a device mapping.
func (m *Manager) GetLinkByDevice(devID string) (core.ILink, bool) {
	conn, ok := m.GetByDevice(devID)
	if !ok {
		return nil, false
	}
	return conn, true
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

// UpdateDeviceLink updates device->link mapping through the compatibility manager.
func (m *Manager) UpdateDeviceLink(devID string, link core.ILink) error {
	if link == nil {
		m.UpdateDeviceIndex(devID, nil)
		return nil
	}
	conn, ok := core.ConnectionFromLink(link)
	if !ok {
		return errCompatLinkRequiresConnection
	}
	m.UpdateDeviceIndex(devID, conn)
	return nil
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

// RangeLinks iterates over managed links.
func (m *Manager) RangeLinks(fn func(core.ILink) bool) {
	m.Range(func(conn core.IConnection) bool {
		return fn(conn)
	})
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
	m.nodeIndex = make(map[uint32]core.IConnection)
	m.devIndex = make(map[string]core.IConnection)
	h := m.hooks
	lh := m.linkHooks
	m.mu.Unlock()
	for _, c := range conns {
		if h.OnRemove != nil {
			h.OnRemove(c)
		}
		if lh.OnRemove != nil {
			lh.OnRemove(c)
		}
		_ = c.Close()
	}
	return nil
}

func asUint32(v any) (uint32, bool) {
	switch vv := v.(type) {
	case uint32:
		return vv, true
	case uint64:
		return uint32(vv), true
	case int:
		if vv < 0 {
			return 0, false
		}
		return uint32(vv), true
	case int64:
		if vv < 0 {
			return 0, false
		}
		return uint32(vv), true
	default:
		return 0, false
	}
}
