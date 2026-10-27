package desk

import "time"

// HotkeyWarning 等待热键注册结果并返回失败警告（空串=正常）。
// 非平台宿主（HotkeyReady 为 nil，如非 Windows 桩）返回空串。
// 注册在平台线程上异步完成，这里最多等 2 秒。
func HotkeyWarning(p *Platform) string {
	if p == nil || p.HotkeyReady == nil {
		return ""
	}
	select {
	case <-p.HotkeyReady:
	case <-time.After(2 * time.Second):
		return "热键注册超时，已回退为界面按钮切换"
	}
	if p.HotkeyFailed != nil && p.HotkeyFailed() {
		return "热键注册失败（可能被占用），已回退为界面按钮切换"
	}
	return ""
}
