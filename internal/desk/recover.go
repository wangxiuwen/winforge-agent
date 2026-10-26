package desk

import "log"

// 长驻 goroutine 的 panic 防护：desk 是托盘常驻进程，任何一条后台协程
// panic（如平台 API 异常、协议分支漏判）都会带崩整个进程、丢掉用户会话。
// 关键后台协程统一用 safeGo 启动；panic 被捕获后记录日志，进程继续活着。

// safeGo 以带 recover 的方式运行 fn，进程不因单条协程崩溃而退出。
func safeGo(logger *log.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Printf("[panic] %s 协程崩溃已拦截: %v", name, r)
				}
			}
		}()
		fn()
	}()
}

// safeCall 在当前 goroutine 内同步执行 fn 并拦截 panic（用于回调入口）。
func safeCall(logger *log.Logger, name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger.Printf("[panic] %s 回调崩溃已拦截: %v", name, r)
			}
		}
	}()
	fn()
}
