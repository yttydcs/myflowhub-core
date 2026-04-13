package config

// Context: This file provides shared Core framework logic around config.

import (
	"sort"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

// MapConfig stores key/value pairs in memory and implements core.IConfig.
type MapConfig struct {
	mu   sync.RWMutex
	data map[string]string
}

const (
	KeyProcChannelCount                   = "process.channel_count"
	KeyProcWorkersPerChan                 = "process.workers_per_channel"
	KeyProcChannelBuffer                  = "process.channel_buffer"
	KeyAuthDefaultRole                    = "auth.default_role"
	KeyAuthDefaultPerms                   = "auth.default_perms"
	KeyAuthNodeRoles                      = "auth.node_roles" // 格式：1:admin;2:node
	KeyAuthRolePerms                      = "auth.role_perms" // 格式：admin:p1,p2;node:p3
	KeyAuthRegisterRequireApproval        = "auth.register.require_approval"
	KeyAuthRegisterPendingTTLSec          = "auth.register.pending_ttl_sec"
	KeyAuthRegisterPermitTTLSec           = "auth.register.permit_ttl_sec"
	KeyAuthBootstrapFirstRegisterEnable   = "auth.bootstrap.first_register.enabled"
	KeyAuthBootstrapFirstRegisterRole     = "auth.bootstrap.first_register.role"
	KeyAuthBootstrapFirstRegisterDeviceID = "auth.bootstrap.first_register.device_id"
	KeyAuthBootstrapFirstRegisterPubKey   = "auth.bootstrap.first_register.pubkey"
	KeyAuthBootstrapFirstRegisterEpoch    = "auth.bootstrap.first_register.epoch"
	KeySendChannelCount                   = "send.channel_count"
	KeySendWorkersPerChan                 = "send.workers_per_channel"
	KeySendChannelBuffer                  = "send.channel_buffer"
	KeySendConnBuffer                     = "send.conn_buffer"
	KeySendEnqueueTimeoutMS               = "send.enqueue_timeout_ms"
	KeyRoutingForwardRemote               = "routing.forward_remote"
	KeyProcQueueStrategy                  = "process.queue_strategy" // conn|subproto|source_target|roundrobin
	KeyDefaultForwardEnable               = "routing.default_forward_enable"
	KeyDefaultForwardTarget               = "routing.default_forward_target"
	KeyDefaultForwardMap                  = "routing.default_forward_map"
	KeyParentEnable                       = "parent.enable"
	KeyParentAddr                         = "parent.addr"
	KeyParentJoinPermit                   = "parent.join_permit"
	KeyParentReconnectSec                 = "parent.reconnect_sec"
	KeyAuthNodePrivKey                    = "auth.node_privkey" // base64 DER p256 private key
	KeyAuthNodePubKey                     = "auth.node_pubkey"  // base64 DER p256 public key
	KeyAuthTrustedNodes                   = "auth.trusted_nodes"
)

const (
	DefaultAuthRolePerms                  = "superadmin:*;admin:file.read,file.write,flow.set,flow.delete,flow.run,flow.read,exec.call,exec.cap.query,exec.cap.sync,var.private_set,var.revoke,var.subscribe,auth.revoke,auth.pending.list,auth.register.approve,auth.register.reject,auth.permit.issue,auth.permit.revoke;node:file.read,file.write,flow.set,flow.run,flow.read,exec.call,exec.cap.query,exec.cap.sync"
	DefaultAuthBootstrapFirstRegisterRole = "superadmin"
)

// NewMap builds a MapConfig from the provided data and fills defaults for missing keys.
func NewMap(data map[string]string) *MapConfig {
	mc := &MapConfig{data: make(map[string]string)}
	for k, v := range data {
		mc.data[k] = v
	}
	ensureDefault(mc.data, KeyProcChannelCount, "1")
	ensureDefault(mc.data, KeyProcWorkersPerChan, "1")
	ensureDefault(mc.data, KeyProcChannelBuffer, "64")
	ensureDefault(mc.data, KeyAuthDefaultRole, "node")
	ensureDefault(mc.data, KeyAuthDefaultPerms, "")
	ensureDefault(mc.data, KeyAuthNodeRoles, "")
	ensureDefault(mc.data, KeyAuthRolePerms, DefaultAuthRolePerms)
	ensureDefault(mc.data, KeyAuthRegisterRequireApproval, "false")
	ensureDefault(mc.data, KeyAuthRegisterPendingTTLSec, "86400")
	ensureDefault(mc.data, KeyAuthRegisterPermitTTLSec, "3600")
	ensureDefault(mc.data, KeyAuthBootstrapFirstRegisterEnable, "false")
	ensureDefault(mc.data, KeyAuthBootstrapFirstRegisterRole, DefaultAuthBootstrapFirstRegisterRole)
	ensureDefault(mc.data, KeyAuthBootstrapFirstRegisterDeviceID, "")
	ensureDefault(mc.data, KeyAuthBootstrapFirstRegisterPubKey, "")
	ensureDefault(mc.data, KeyAuthBootstrapFirstRegisterEpoch, "0")
	ensureDefault(mc.data, KeySendChannelCount, "1")
	ensureDefault(mc.data, KeySendWorkersPerChan, "1")
	ensureDefault(mc.data, KeySendChannelBuffer, "64")
	ensureDefault(mc.data, KeySendConnBuffer, "64")
	ensureDefault(mc.data, KeySendEnqueueTimeoutMS, "100")
	ensureDefault(mc.data, KeyRoutingForwardRemote, "true")
	ensureDefault(mc.data, KeyProcQueueStrategy, "conn")
	ensureDefault(mc.data, KeyDefaultForwardEnable, "")
	ensureDefault(mc.data, KeyDefaultForwardTarget, "")
	ensureDefault(mc.data, KeyDefaultForwardMap, "")
	ensureDefault(mc.data, KeyParentEnable, "false")
	ensureDefault(mc.data, KeyParentAddr, "")
	ensureDefault(mc.data, KeyParentJoinPermit, "")
	ensureDefault(mc.data, KeyParentReconnectSec, "3")
	return mc
}

func ensureDefault(m map[string]string, key, val string) {
	if _, ok := m[key]; !ok {
		m[key] = val
	}
}

func (m *MapConfig) MergeFile(data map[string]string) {
	if m == nil || data == nil {
		return
	}
	m.mu.Lock()
	for k, v := range data {
		m.data[k] = v
	}
	m.mu.Unlock()
}

// Get returns value and existence flag.
func (m *MapConfig) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	return val, ok
}

// Set updates a key at runtime.
func (m *MapConfig) Set(key, val string) {
	m.mu.Lock()
	m.data[key] = val
	m.mu.Unlock()
}

// Merge overlays another config into current config and returns itself.
func (m *MapConfig) Merge(other core.IConfig) core.IConfig {
	if m == nil || other == nil {
		return m
	}
	if o, ok := other.(*MapConfig); ok && o != nil {
		o.mu.RLock()
		m.mu.Lock()
		for k, v := range o.data {
			m.data[k] = v
		}
		m.mu.Unlock()
		o.mu.RUnlock()
		return m
	}
	// Unable to enumerate other implementations; return as is.
	return m
}

// Keys 返回当前存储的全部配置键（有序）。
func (m *MapConfig) Keys() []string {
	m.mu.RLock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	m.mu.RUnlock()
	sort.Strings(keys)
	return keys
}
