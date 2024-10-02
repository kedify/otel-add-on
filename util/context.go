package util

import (
	"context"

	"github.com/go-logr/logr"
)

type contextKey int

const (
	ckLogger contextKey = iota
	ckHTTPSO
	ckStream
	ckHealthCheck
)

func ContextWithLogger(ctx context.Context, logger logr.Logger) context.Context {
	return context.WithValue(ctx, ckLogger, logger)
}
