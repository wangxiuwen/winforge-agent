package desk

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// 会话反复断开→重连 10 轮后 goroutine 数应收敛（泄漏会随轮数单调增长）。
// 三次采样间隔比较而非对比固定基线：前面测试遗留的常量 goroutine 不影响
// "是否增长"这个信号。
func TestSessionGoroutineHygiene(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	bSelf := peerViewsBID(b) // B 的 ID，即 A 侧会话表的键

	for i := 0; i < 10; i++ {
		a.mu.Lock()
		if s := a.sessions[bSelf]; s != nil {
			_ = s.conn.Close()
		}
		a.mu.Unlock()
		waitFor(t, 20*time.Second, fmt.Sprintf("第%d轮重连", i+1), func() bool {
			for _, p := range peerViews(a) {
				if p.ID == bSelf {
					return p.Online
				}
			}
			return false
		})
	}

	time.Sleep(3 * time.Second)
	s1 := runtime.NumGoroutine()
	time.Sleep(4 * time.Second)
	s2 := runtime.NumGoroutine()
	time.Sleep(4 * time.Second)
	s3 := runtime.NumGoroutine()
	t.Logf("goroutine 采样: %d → %d → %d", s1, s2, s3)
	if s3 > s1+6 {
		t.Fatalf("断开重连 10 轮后 goroutine 疑似泄漏: %d → %d → %d", s1, s2, s3)
	}
}
