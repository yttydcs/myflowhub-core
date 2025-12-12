package permission

import (
	"reflect"
	"strconv"
	"strings"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const (
	Wildcard      = "*"
	AuthRevoke    = "auth.revoke"
	VarPrivateSet = "var.private_set"
	VarRevoke     = "var.revoke"
	VarSubscribe  = "var.subscribe"
)

// Snapshot captures the exported permission state for syncing.
type Snapshot struct {
	DefaultRole  string              `json:"default_role,omitempty"`
	DefaultPerms []string            `json:"default_perms,omitempty"`
	NodeRoles    map[uint32]string   `json:"node_roles,omitempty"`
	RolePerms    map[string][]string `json:"role_perms,omitempty"`
}

// Config stores role -> permission mappings and supports runtime updates.
type Config struct {
	mu           sync.RWMutex
	defaultRole  string
	defaultPerms []string
	nodeRoles    map[uint32]string
	rolePerms    map[string][]string
}

var sharedConfigs sync.Map

// NewConfig builds an isolated config instance.
func NewConfig(cfg core.IConfig) *Config {
	c := &Config{
		defaultRole:  "node",
		defaultPerms: []string{Wildcard},
		nodeRoles:    make(map[uint32]string),
		rolePerms:    make(map[string][]string),
	}
	c.Load(cfg)
	return c
}

// SharedConfig returns a singleton permission config for the provided core config pointer.
func SharedConfig(cfg core.IConfig) *Config {
	if cfg == nil {
		return NewConfig(nil)
	}
	key := sharedKey(cfg)
	if key == 0 {
		return NewConfig(cfg)
	}
	if existing, ok := sharedConfigs.Load(key); ok {
		if inst, ok2 := existing.(*Config); ok2 {
			return inst
		}
	}
	inst := NewConfig(cfg)
	actual, _ := sharedConfigs.LoadOrStore(key, inst)
	if cfgPtr, ok := actual.(*Config); ok {
		return cfgPtr
	}
	return inst
}

func sharedKey(cfg core.IConfig) uintptr {
	val := reflect.ValueOf(cfg)
	if !val.IsValid() {
		return 0
	}
	if val.Kind() != reflect.Pointer {
		return 0
	}
	return val.Pointer()
}

// Load hydrates the config from the provided source.
func (c *Config) Load(cfg core.IConfig) {
	if c == nil || cfg == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultRole); ok && strings.TrimSpace(raw) != "" {
		c.defaultRole = strings.TrimSpace(raw)
	} else if c.defaultRole == "" {
		c.defaultRole = "node"
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultPerms); ok {
		perms := parseList(raw)
		if len(perms) > 0 {
			c.defaultPerms = perms
		} else {
			c.defaultPerms = nil
		}
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthNodeRoles); ok {
		c.nodeRoles = parseNodeRoles(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRolePerms); ok {
		c.rolePerms = parseRolePerms(raw)
	}
	ensureMaps(c)
}

// Snapshot returns a deep copy of the current state.
func (c *Config) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Snapshot{
		DefaultRole:  c.defaultRole,
		DefaultPerms: cloneStrings(c.defaultPerms),
		NodeRoles:    cloneNodeRoles(c.nodeRoles),
		RolePerms:    cloneRolePerms(c.rolePerms),
	}
}

// ApplySnapshot overwrites the config with the provided snapshot.
func (c *Config) ApplySnapshot(s Snapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if strings.TrimSpace(s.DefaultRole) != "" {
		c.defaultRole = strings.TrimSpace(s.DefaultRole)
	}
	if s.DefaultPerms != nil {
		c.defaultPerms = cloneStrings(s.DefaultPerms)
	}
	if s.NodeRoles != nil {
		c.nodeRoles = cloneNodeRoles(s.NodeRoles)
	}
	if s.RolePerms != nil {
		c.rolePerms = cloneRolePerms(s.RolePerms)
	}
	ensureMaps(c)
	c.mu.Unlock()
}

// UpsertNode records the authoritative role/perms for the node.
func (c *Config) UpsertNode(nodeID uint32, role string, perms []string) {
	if c == nil || nodeID == 0 {
		return
	}
	role = strings.TrimSpace(role)
	c.mu.Lock()
	ensureMaps(c)
	if role == "" {
		delete(c.nodeRoles, nodeID)
		c.mu.Unlock()
		return
	}
	c.nodeRoles[nodeID] = role
	if len(perms) > 0 {
		c.rolePerms[role] = cloneStrings(perms)
	}
	c.mu.Unlock()
}

// InvalidateNodes removes cached role data for supplied node IDs (all if empty).
func (c *Config) InvalidateNodes(nodeIDs []uint32) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if len(nodeIDs) == 0 {
		c.nodeRoles = make(map[uint32]string)
		c.mu.Unlock()
		return
	}
	if c.nodeRoles != nil {
		for _, id := range nodeIDs {
			delete(c.nodeRoles, id)
		}
	}
	c.mu.Unlock()
}

// ResolveRole returns the effective role for the node (or default).
func (c *Config) ResolveRole(nodeID uint32) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resolveRoleLocked(nodeID)
}

func (c *Config) resolveRoleLocked(nodeID uint32) string {
	if nodeID != 0 {
		if role, ok := c.nodeRoles[nodeID]; ok && strings.TrimSpace(role) != "" {
			return strings.TrimSpace(role)
		}
	}
	return c.defaultRole
}

// ResolvePerms returns the permissions tied to the node's role.
func (c *Config) ResolvePerms(nodeID uint32) []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	role := c.resolveRoleLocked(nodeID)
	if perms, ok := c.rolePerms[role]; ok {
		return cloneStrings(perms)
	}
	return cloneStrings(c.defaultPerms)
}

// Has reports whether a node can perform the specified permission string.
func (c *Config) Has(nodeID uint32, perm string) bool {
	if perm == "" || nodeID == 0 {
		return true
	}
	perms := c.ResolvePerms(nodeID)
	if len(perms) == 0 {
		return false
	}
	for _, entry := range perms {
		if entry == Wildcard || entry == perm {
			return true
		}
	}
	return false
}

// NodeRoles returns a copy of current node-role mapping.
func (c *Config) NodeRoles() map[uint32]string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneNodeRoles(c.nodeRoles)
}

// SourceNodeID extracts node id from header or connection metadata.
func SourceNodeID(hdr core.IHeader, conn core.IConnection) uint32 {
	if hdr != nil && hdr.SourceID() != 0 {
		return hdr.SourceID()
	}
	if conn == nil {
		return 0
	}
	if v, ok := conn.GetMeta("nodeID"); ok {
		switch val := v.(type) {
		case uint32:
			return val
		case uint64:
			return uint32(val)
		case int:
			if val >= 0 {
				return uint32(val)
			}
		case int64:
			if val >= 0 {
				return uint32(val)
			}
		}
	}
	return 0
}

func parseList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseNodeRoles(raw string) map[uint32]string {
	m := make(map[uint32]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(kv[0]), 10, 32)
		role := strings.TrimSpace(kv[1])
		if err == nil && role != "" {
			m[uint32(id)] = role
		}
	}
	return m
}

func parseRolePerms(raw string) map[string][]string {
	m := make(map[string][]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		role := strings.TrimSpace(kv[0])
		if role == "" {
			continue
		}
		m[role] = parseList(kv[1])
	}
	return m
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func cloneNodeRoles(src map[uint32]string) map[uint32]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[uint32]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneRolePerms(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		out[k] = cloneStrings(v)
	}
	return out
}

func ensureMaps(c *Config) {
	if c.nodeRoles == nil {
		c.nodeRoles = make(map[uint32]string)
	}
	if c.rolePerms == nil {
		c.rolePerms = make(map[string][]string)
	}
}
