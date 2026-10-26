package desk

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDeleteChatHistoryOnPeerRemove 删配对时聊天记录文件一并清理（隐私）。
func TestDeleteChatHistoryOnPeerRemove(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "desk.json")
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	peer := Peer{ID: "aaaabbbbccccddddeeeeffff00001111", Name: "对端", PSK: "cGFyaw==", Enabled: true}
	if err := cfg.UpPeer(cfgPath, peer); err != nil {
		t.Fatal(err)
	}
	h := NewHub(cfg, cfgPath, Platform{}, nil)
	h.recordChat(peer.ID, ChatMsg{Text: "密谈内容", Ts: time.Now().UnixMilli()})

	chatDir := filepath.Join(dir, "chat")
	entries, _ := os.ReadDir(chatDir)
	if len(entries) != 1 {
		t.Fatalf("聊天文件应存在: %v", entries)
	}

	h.DeleteChatHistory(peer.ID)
	if _, err := os.Stat(chatDir + "/aaaabbbbccccddddeeeeffff00001111.json"); !os.IsNotExist(err) {
		t.Fatal("聊天文件未删除")
	}
	if len(h.ChatHistory(peer.ID)) != 0 {
		t.Fatal("内存历史未清")
	}
}

// TestDeleteChatHistoryRejectsBadID 非法 ID 不触碰文件系统。
func TestDeleteChatHistoryRejectsBadID(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "desk.json")
	cfg, _ := LoadConfig(cfgPath)
	h := NewHub(cfg, cfgPath, Platform{}, nil)
	h.DeleteChatHistory("../escape")
	h.DeleteChatHistory("")
	if _, err := os.Stat(filepath.Join(dir, "chat")); !os.IsNotExist(err) {
		t.Fatal("非法 ID 不应创建/触碰 chat 目录")
	}
}
