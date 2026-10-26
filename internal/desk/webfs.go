package desk

import (
	"embed"
	"io/fs"
)

// 控制台前端资源（Wails 桌面窗口与浏览器模式共用）。

//go:embed web
var webFS embed.FS

// WebFS 返回以 web/ 为根的前端文件系统。
func WebFS() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return webFS
	}
	return sub
}
