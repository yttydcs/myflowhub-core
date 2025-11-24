package builder

import (
	"errors"
	"os"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"gopkg.in/yaml.v3"
)

// YAMLBuilder loads config from a YAML file (flat string-to-string).
// Keys are expected in lowercase with dot separators, e.g. process.channel_count.
type YAMLBuilder struct {
	Path string
}

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

func (b YAMLBuilder) Reload() (core.IConfig, error) {
	return b.Load()
}
