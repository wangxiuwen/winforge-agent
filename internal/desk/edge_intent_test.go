package desk

import (
	"testing"
	"time"
)

// onEdge：核心交互判定——推过边缘控制对方 / 被控时推回拿回。此前 0% 覆盖。

// 空闲时朝"右方位对端"对应的右边推 → 发起控制成功（role→1）。
func TestOnEdgeIdleTakesControl(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	if err := a.PeerPosition(peerB.ID, "right"); err != nil {
		t.Fatal(err)
	}

	a.onEdge(EdgeRight, 0.5)
	waitFor(t, 5*time.Second, "推边后 A 进入控制", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ctl.Role == roleController
	})
}

// 空闲时推"与对端方位不符的边" → 不动作。
func TestOnEdgeWrongEdgeDoesNothing(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	_ = a.PeerPosition(peerB.ID, "right")

	a.onEdge(EdgeLeft, 0.5) // 对端在右，推左不应触发
	time.Sleep(500 * time.Millisecond)
	a.mu.Lock()
	role := a.ctl.Role
	a.mu.Unlock()
	if role != roleIdle {
		t.Fatalf("推错边不应发起控制, role=%d", role)
	}
}

// 被控时朝控制器方位的边推 → 拿回（role 回 0，控制器侧也回 0）。
func TestOnEdgeControlledPushBackReleases(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	_ = a.PeerPosition(peerB.ID, "right")

	if err := a.TakeControl(peerB.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	// 必须等 B 侧真正进入被控（ctl.PeerID 已设置）再推边，否则 onEdge
	// 会走 roleIdle 分支把这次推边当成控制请求。
	waitFor(t, 5*time.Second, "B 进入被控", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.ctl.Role == roleControlled && b.ctl.PeerID == peerViewsBID(a)
	})

	// B（被控，对端 A 无方位设置时）没有匹配边——给 B 视角的对端 A 设方位，
	// 使 B 的"控制器方位边"可判定。
	peerA := peerViews(b)[0]
	_ = b.PeerPosition(peerA.ID, "left")
	b.onEdge(EdgeLeft, 0.5)
	waitFor(t, 5*time.Second, "B 推回后拿回", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.ctl.Role == roleIdle
	})
	waitFor(t, 5*time.Second, "A 侧控制同步结束", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ctl.Role == roleIdle
	})
}
