package subproto

// 本文件承载 Core 框架中与 `base_action` 相关的通用逻辑。

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
)

// BaseAction 提供 SubProcessAction 的空实现，可嵌入具体动作以减少样板。
type BaseAction struct{}

// Name 返回占位动作名，真实 action 通常会覆盖它。
func (BaseAction) Name() string { return "BaseAction" }

// RequireAuth 默认声明不需要鉴权，供具体动作按需收紧。
func (BaseAction) RequireAuth() bool { return false }

// Handle 提供空实现，让嵌入者可以只覆写真正关心的方法。
func (BaseAction) Handle(context.Context, core.IConnection, core.IHeader, json.RawMessage) {
	// no-op
}
