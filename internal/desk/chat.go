package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// 聊天记录持久化：每个对端一个 JSON 文件（配置目录 chat/<peerID>.json），
// 每对端上限 500 条，写入时整文件重写。peerID 是 hex，文件名固定格式防穿越。

const chatKeepPerPeer = 500

// ChatMsg 是落盘与 UI 共用的消息记录。
type ChatMsg struct {
	Text     string `json:"text"`
	Ts       int64  `json:"ts"`
	Outgoing bool   `json:"outgoing"`
}

var chatFileRe = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// chatDir 记录存储目录（配置文件同级的 chat/）。
func (h *Hub) chatDir() string {
	return filepath.Join(filepath.Dir(h.cfgPath), "chat")
}

func (h *Hub) chatFilePath(peerID string) (string, bool) {
	if !chatFileRe.MatchString(peerID) {
		return "", false
	}
	return filepath.Join(h.chatDir(), peerID+".json"), true
}

// recordChat 记录一条消息（内存 + 落盘）。
func (h *Hub) recordChat(peerID string, m ChatMsg) {
	h.mu.Lock()
	list := append(h.chatHistory[peerID], m)
	if len(list) > chatKeepPerPeer {
		list = list[len(list)-chatKeepPerPeer:]
	}
	h.chatHistory[peerID] = list
	h.mu.Unlock()
	h.saveChatFile(peerID, list)
}

func (h *Hub) saveChatFile(peerID string, list []ChatMsg) {
	path, ok := h.chatFilePath(peerID)
	if !ok {
		return
	}
	if err := os.MkdirAll(h.chatDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// loadChatHistory 启动时从磁盘恢复。
func (h *Hub) loadChatHistory() {
	dir := h.chatDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chatHistory = map[string][]ChatMsg{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-len(".json")]
		path, ok := h.chatFilePath(id)
		if !ok {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var list []ChatMsg
		if json.Unmarshal(b, &list) == nil && len(list) > 0 {
			// 过滤未来时间戳与超长文本（防手改文件注入）。
			kept := list[:0]
			for _, m := range list {
				if m.Ts <= time.Now().UnixMilli()+time.Minute.Milliseconds() &&
					len(m.Text) <= maxChatLength {
					kept = append(kept, m)
				}
			}
			h.chatHistory[id] = kept
		}
	}
}

// DeleteChatHistory 删除指定对端的聊天记录（内存 + 磁盘文件）。
// 删除配对即"忘记这台设备"，聊天记录一并清理属隐私要求。
func (h *Hub) DeleteChatHistory(peerID string) {
	h.mu.Lock()
	delete(h.chatHistory, peerID)
	h.mu.Unlock()
	if path, ok := h.chatFilePath(peerID); ok {
		_ = os.Remove(path)
	}
}

// ChatHistory 返回指定对端的历史（UI 命令）。
func (h *Hub) ChatHistory(peerID string) []ChatMsg {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.chatHistory[peerID]
	out := make([]ChatMsg, len(list))
	copy(out, list)
	return out
}

// chatStore 供 hub 启动时持有锁初始化。
var _ sync.Locker = (*sync.Mutex)(nil)
