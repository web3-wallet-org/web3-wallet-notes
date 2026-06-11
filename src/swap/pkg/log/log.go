package log

import (
	"context"

	"go.uber.org/zap"
)

type contextKey struct{}

var global *zap.SugaredLogger

func init() {
	l, _ := zap.NewProduction()
	global = l.Sugar()
}

// Init 在 main 启动时调用一次，用配置好的 logger 替换默认全局 logger。
func Init(l *zap.Logger) {
	global = l.Sugar()
}

// WithContext 将 logger 绑定到 ctx，供中间件注入 request-scoped 字段。
func WithContext(ctx context.Context, l *zap.SugaredLogger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext 取出 request-scoped logger；若不存在则返回全局 logger。
func FromContext(ctx context.Context) *zap.SugaredLogger {
	if l, ok := ctx.Value(contextKey{}).(*zap.SugaredLogger); ok {
		return l
	}
	return global
}
