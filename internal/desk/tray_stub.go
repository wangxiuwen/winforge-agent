//go:build !windows && !linux

package desk

// 此平台（如 darwin 交叉编译 CGO=0）暂无系统托盘实现：
// desk 继续以浏览器控制台形态运行。

// RunTray 占位实现，永久阻塞。
func RunTray(h *Hub, openConsole func(), onExit func()) {
	select {}
}

// ShareEnabledSnapshot 并发安全的共享开关读取。
func (h *Hub) ShareEnabledSnapshot() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ShareEnabled
}
