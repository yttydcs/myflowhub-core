package builder

// Context: This file provides shared Core framework logic around builder.

import core "github.com/yttydcs/myflowhub-core"

// Builder 定义配置加载器接口，提供一次性加载与可选重载。
type Builder interface {
	Load() (core.IConfig, error)
	Reload() (core.IConfig, error)
}
