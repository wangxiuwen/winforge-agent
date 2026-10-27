package desk

import (
	"fmt"
	"testing"
)

// 聊天历史上限：超过 chatKeepPerPeer 后裁旧留新，内存与落盘一致。
func TestChatHistoryCapTrim(t *testing.T) {
	pa, _ := newFakePlatform(), newFakePlatform()
	a, cfgPath := newTestHub(t, "主机A", pa)
	peer := "fedcba9876543210" // 满足 hex 校验的假对端 ID

	total := chatKeepPerPeer + 20
	for i := 0; i < total; i++ {
		a.recordChat(peer, ChatMsg{Text: fmt.Sprintf("m%d", i), Ts: int64(1000 + i)})
	}

	hist := a.ChatHistory(peer)
	if len(hist) != chatKeepPerPeer {
		t.Fatalf("历史应裁到 %d 条, got %d", chatKeepPerPeer, len(hist))
	}
	// 最早的 total-chatKeepPerPeer 条被裁掉，保留的是最新段
	if hist[0].Text != fmt.Sprintf("m%d", total-chatKeepPerPeer) {
		t.Fatalf("裁剪后首条应为 m%d, got %s", total-chatKeepPerPeer, hist[0].Text)
	}
	if hist[len(hist)-1].Text != fmt.Sprintf("m%d", total-1) {
		t.Fatalf("末条应为最新 m%d, got %s", total-1, hist[len(hist)-1].Text)
	}

	// 落盘文件同样裁剪（重启恢复不超过上限）
	b2, _ := NewHubFromExisting(t, a)
	hist2 := b2.ChatHistory(peer)
	if len(hist2) != chatKeepPerPeer {
		t.Fatalf("重启后历史应仍为 %d 条, got %d", chatKeepPerPeer, len(hist2))
	}
	_ = cfgPath
}
