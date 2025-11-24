package builder

import core "MyFlowHub-Core/internal/core"

// Builder 定义配置加载器接口，提供一次性加载与可选重载。
type Builder interface {
	Load() (core.IConfig, error)
	Reload() (core.IConfig, error)
}
