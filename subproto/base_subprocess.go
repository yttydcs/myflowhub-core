package subproto

import (
	"context"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

// BaseSubProcess 提供 ISubProcess 的默认实现与可选方法实现，便于嵌入减少样板。
// 建议实际 handler 覆盖 SubProto/OnReceive/Init 等方法。
type BaseSubProcess struct{}

// 默认子协议编号 0（需实际 handler 覆盖）。
func (BaseSubProcess) SubProto() uint8 { return 0 }

// 默认 OnReceive 空实现（需实际 handler 覆盖）。
func (BaseSubProcess) OnReceive(context.Context, core.IConnection, core.IHeader, []byte) {}

// 默认 Init 直接返回 true。
func (BaseSubProcess) Init() bool { return true }

// 默认不截获 Cmd。
func (BaseSubProcess) AcceptCmd() bool { return false }

// 默认不允许 Source 与连接元数据不一致。
func (BaseSubProcess) AllowSourceMismatch() bool { return false }

// ActionBaseSubProcess 扩展 BaseSubProcess，内置 action 注册表与注册方法。
// 适用于 action+data 模式的子协议处理器。
type ActionBaseSubProcess struct {
	BaseSubProcess
	Actions map[string]core.SubProcessAction
}

// ResetActions 初始化或清空内置 action 表。
func (a *ActionBaseSubProcess) ResetActions() {
	a.Actions = make(map[string]core.SubProcessAction)
}

// RemoveAction 按名称移除已注册的 action（名称大小写不敏感）。
func (a *ActionBaseSubProcess) RemoveAction(name string) {
	if a == nil || len(a.Actions) == 0 {
		return
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	delete(a.Actions, key)
}

// RegisterAction 按名称注册 action，名称统一为小写键。
func (a *ActionBaseSubProcess) RegisterAction(act core.SubProcessAction) {
	if act == nil || act.Name() == "" {
		return
	}
	if a.Actions == nil {
		a.Actions = make(map[string]core.SubProcessAction)
	}
	a.Actions[strings.ToLower(act.Name())] = act
}

// LookupAction 根据名称查找已注册的 action（名称大小写不敏感）。
func (a *ActionBaseSubProcess) LookupAction(name string) (core.SubProcessAction, bool) {
	if a == nil || len(a.Actions) == 0 {
		return nil, false
	}
	act, ok := a.Actions[strings.ToLower(strings.TrimSpace(name))]
	return act, ok
}
