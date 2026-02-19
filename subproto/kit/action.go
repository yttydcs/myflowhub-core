package kit

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

// ActionKind 表示 action 在子协议内的语义分类。
// 注意：kind 仅用于工程组织/可观测，不参与 wire/路由/转发语义。
type ActionKind uint8

const (
	ActionKindUnknown ActionKind = iota
	ActionKindLocal
	ActionKindAssist
	ActionKindUp
	ActionKindNotify
)

func (k ActionKind) String() string {
	switch k {
	case ActionKindLocal:
		return "local"
	case ActionKindAssist:
		return "assist"
	case ActionKindUp:
		return "up"
	case ActionKindNotify:
		return "notify"
	default:
		return "unknown"
	}
}

// KindFromName 根据 action 名称推导语义分类。
// 默认规则（wire 不变）：
// - assist_* -> Assist
// - up_* -> Up
// - notify_* -> Notify
// - 其它 -> Local
func KindFromName(name string) ActionKind {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(n, "assist_"):
		return ActionKindAssist
	case strings.HasPrefix(n, "up_"):
		return ActionKindUp
	case strings.HasPrefix(n, "notify_"):
		return ActionKindNotify
	case n != "":
		return ActionKindLocal
	default:
		return ActionKindUnknown
	}
}

type ActionHandler func(context.Context, core.IConnection, core.IHeader, json.RawMessage)

// FuncAction 是函数式 action：用闭包代替大量样板结构体。
// 它实现 core.SubProcessAction。
type FuncAction struct {
	name        string
	requireAuth bool
	kind        ActionKind
	handle      ActionHandler
}

func (a *FuncAction) Name() string { return a.name }
func (a *FuncAction) RequireAuth() bool {
	return a != nil && a.requireAuth
}
func (a *FuncAction) Kind() ActionKind {
	if a == nil {
		return ActionKindUnknown
	}
	return a.kind
}
func (a *FuncAction) Handle(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if a == nil || a.handle == nil {
		return
	}
	a.handle(ctx, conn, hdr, data)
}

type ActionOption func(*FuncAction)

func WithRequireAuth(required bool) ActionOption {
	return func(a *FuncAction) {
		if a != nil {
			a.requireAuth = required
		}
	}
}

func WithKind(kind ActionKind) ActionOption {
	return func(a *FuncAction) {
		if a != nil {
			a.kind = kind
		}
	}
}

// NewAction 构造一个函数式 action。
// - 默认 requireAuth=false
// - 默认 kind 由 KindFromName 推导（可用 WithKind 覆盖）
func NewAction(name string, handler ActionHandler, opts ...ActionOption) core.SubProcessAction {
	name = strings.TrimSpace(name)
	act := &FuncAction{
		name:   name,
		kind:   KindFromName(name),
		handle: handler,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(act)
		}
	}
	if act.name == "" {
		return nil
	}
	return act
}

