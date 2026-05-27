package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func EnsureRequestID(ctx context.Context, requestID string) (context.Context, string) {
	if requestID == "" {
		requestID = NewRequestID()
	}
	return WithRequestID(ctx, requestID), requestID
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func NewRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes[:])
}
