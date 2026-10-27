package desk

import (
	"strings"
	"testing"
	"time"
)

// 热键切换往返：idle → 按 → 控制 → 再按 → 停止（此前 0% 覆盖）。
func TestHotkeyTogglesControl(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)

	a.hotkeyPressed()
	waitFor(t, 5*time.Second, "热键后 A 进入控制", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ctl.Role == roleController
	})
	// R59 教训：必须等 B 侧真正被控再按第二次，否则 release 先于 take
	// 到达会被 B 丢弃（B 卡在被控）。
	waitFor(t, 5*time.Second, "B 进入被控", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.ctl.Role == roleControlled && b.ctl.PeerID == peerViewsBID(a)
	})

	a.hotkeyPressed()
	waitFor(t, 5*time.Second, "再按热键后 A 停止控制", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ctl.Role == roleIdle
	})
	waitFor(t, 5*time.Second, "B 侧同步回空闲", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.ctl.Role == roleIdle
	})
}

// 空闲且无配对设备：热键不动作，但应提示"没有可控制的配对设备"。
func TestHotkeyIdleWithoutPeers(t *testing.T) {
	pa, _ := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)

	a.hotkeyPressed()
	time.Sleep(300 * time.Millisecond)
	a.mu.Lock()
	role := a.ctl.Role
	a.mu.Unlock()
	if role != roleIdle {
		t.Fatalf("无对端时热键不应改变状态, role=%d", role)
	}
	found := false
	for _, ev := range emitterOf(a).all() {
		if ev.name == "desk:toast" {
			if mm, ok := ev.data.(map[string]string); ok && strings.Contains(mm["text"], "没有可控制的配对设备") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("应提示没有可控制的配对设备")
	}
}
