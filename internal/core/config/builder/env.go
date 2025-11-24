package builder

import (
	"os"
	"strings"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
)

// EnvBuilder loads config from environment variables.
// Rules:
// - Only keys with the given prefix are read (Prefix may be empty to read all).
// - Variable names are expected in upper case.
// - Normalization: trim prefix -> to lower -> "_" becomes "."; "__" becomes "_" to allow literal underscores.
type EnvBuilder struct {
	Prefix string
}

func (b EnvBuilder) Load() (core.IConfig, error) {
	data := make(map[string]string)
	prefix := strings.TrimSpace(b.Prefix)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k, v := parts[0], parts[1]
		if prefix != "" {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			k = strings.TrimPrefix(k, prefix)
		}
		if k == "" {
			continue
		}
		norm := normalizeEnvKey(k)
		if norm == "" {
			continue
		}
		data[norm] = v
	}
	return config.NewMap(data), nil
}

func (b EnvBuilder) Reload() (core.IConfig, error) {
	return b.Load()
}

func normalizeEnvKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	k = strings.ToLower(k)
	var sb strings.Builder
	for i := 0; i < len(k); i++ {
		ch := k[i]
		if ch == '_' {
			if i+1 < len(k) && k[i+1] == '_' {
				sb.WriteByte('_')
				i++
			} else {
				sb.WriteByte('.')
			}
			continue
		}
		sb.WriteByte(ch)
	}
	return sb.String()
}
