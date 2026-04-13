package subproto

// Context: This file provides shared Core framework logic around base_action.

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
)

// BaseAction 提供 SubProcessAction 的空实现，可嵌入具体动作以减少样板。
type BaseAction struct{}

func (BaseAction) Name() string      { return "BaseAction" }
func (BaseAction) RequireAuth() bool { return false }
func (BaseAction) Handle(context.Context, core.IConnection, core.IHeader, json.RawMessage) {
	// no-op
}
