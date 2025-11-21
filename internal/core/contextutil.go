package core

import "context"

type serverCtxKey struct{}

// WithServerContext 将 IServer 写入 context，供下游 handler/Process 读取。
func WithServerContext(ctx context.Context, srv IServer) context.Context {
	return context.WithValue(ctx, serverCtxKey{}, srv)
}

// ServerFromContext 从 context 中提取 IServer。
func ServerFromContext(ctx context.Context) IServer {
	if ctx == nil {
		return nil
	}
	if srv, ok := ctx.Value(serverCtxKey{}).(IServer); ok {
		return srv
	}
	return nil
}
