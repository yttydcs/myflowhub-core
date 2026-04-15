package builder

// 本文件承载 Core 框架中与 `yaml` 相关的通用逻辑。

import (
	"errors"
	"os"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/config"
	"gopkg.in/yaml.v3"
)

// YAMLBuilder loads config from a YAML file (flat string-to-string).
// Keys are expected in lowercase with dot separators, e.g. process.channel_count.
type YAMLBuilder struct {
	Path string
}

// Load 从 YAML 文件读取平铺配置；缺文件时返回空配置而不是报错。
func (b YAMLBuilder) Load() (core.IConfig, error) {
	if b.Path == "" {
		return config.NewMap(nil), nil
	}
	content, err := os.ReadFile(b.Path)
	if errors.Is(err, os.ErrNotExist) {
		return config.NewMap(nil), nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]string
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, err
	}
	return config.NewMap(raw), nil
}

// Reload 对静态 YAML 构建器来说就是重新读取源文件。
func (b YAMLBuilder) Reload() (core.IConfig, error) {
	return b.Load()
}
