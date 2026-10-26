package desk

import (
	"context"
	"sync"
	"testing"
)

// 测试辅助：Hub → testEmitter 登记表 + 上下文。

var (
	testEmitterMu sync.Mutex
	testEmitters  = map[*Hub]*testEmitter{}
)

func registerTestEmitter(h *Hub, e *testEmitter) {
	testEmitterMu.Lock()
	testEmitters[h] = e
	testEmitterMu.Unlock()
}

func emitterOf(h *Hub) *testEmitter {
	testEmitterMu.Lock()
	defer testEmitterMu.Unlock()
	if e := testEmitters[h]; e != nil {
		return e
	}
	return &testEmitter{}
}

func (e *testEmitter) all() []capturedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]capturedEvent(nil), e.events...)
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
