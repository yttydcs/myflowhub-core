package builder

// 本文件承载 Core 框架中与 `env` 相关的通用逻辑。

import (
	"os"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/config"
)

// EnvBuilder loads config from environment variables.
// Rules:
// - Only keys with the given prefix are read (Prefix may be empty to read all).
// - Variable names are expected in upper case.
// - Normalization: trim prefix -> to lower -> "_" becomes "."; "__" becomes "_" to allow literal underscores.
type EnvBuilder struct {
	Prefix string
}

// Load 扫描环境变量并按约定规则归一成点分配置键。
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

// Reload 对环境变量构建器来说等价于重新执行一次全量读取。
func (b EnvBuilder) Reload() (core.IConfig, error) {
	return b.Load()
}

// normalizeEnvKey 把环境变量名映射为配置系统使用的点分键。
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
