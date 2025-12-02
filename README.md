# MyFlowHub-Core

面向 Hub/边端的轻量级 TCP 消息流核心库，提供 HeaderTcp 协议、连接管理、预路由+分发、发送调度等能力。业务层（handler、login_server、demo、debugclient、tests）已迁移到 **MyFlowHub-Server**，本仓库仅保留可复用的核心模块。

## 安装

```bash
go get github.com/yttydcs/myflowhub-core
```

## 目录

- `iface.go`、`contextutil.go`、`roles.go`、`util.go`：核心接口与工具
- `bootstrap/`：自注册辅助
- `config/`：配置与构建器
- `connmgr/`：内存连接管理器
- `header/`：HeaderTcp 定义与编解码
- `listener/tcp_listener/`：TCP 监听器与连接封装
- `process/`：预路由、分发、发送调度、策略
- `reader/`：基于 HeaderCodec 的读取循环
- `server/`：服务器编排

## 快速示例

```go
package main

import (
	"context"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/listener/tcp_listener"
	"github.com/yttydcs/myflowhub-core/process"
	"github.com/yttydcs/myflowhub-core/server"
)

func main() {
	cfg := config.NewMap(map[string]string{"addr": ":9000"})
	cm := connmgr.New()
	proc := process.NewSimple(nil)
	lis := tcp_listener.New(":9000", tcp_listener.Options{KeepAlive: true, KeepAlivePeriod: 30 * time.Second})
	codec := header.HeaderTcpCodec{}

	srv, _ := server.New(server.Options{
		Name:     "MyServer",
		Process:  proc,
		Codec:    codec,
		Listener: lis,
		Config:   cfg,
		Manager:  cm,
		NodeID:   1,
	})

	_ = srv.Start(context.Background())
}
```

## HeaderTcp 协议（概览）

- 24 字节固定头 + N 字节 payload  
- 字段：TypeFmt(major+subproto)、Flags、MsgID、Source、Target、Timestamp、PayloadLen、Reserved  
- 大类：0=OK、1=Err、2=Msg、3=Cmd

## 开发

```bash
go test ./...
```

## 许可证

MIT License

