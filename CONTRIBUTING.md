# Contributing

1. 先开 Issue 描述 Windows 工具链或设备场景。
2. 从 `main` 创建分支，提交小而可审查的改动。
3. 运行 `go test -race ./...`、`go vet ./...` 和 `govulncheck ./...`。
4. 涉及网络、认证、路径和进程执行的改动必须增加安全回归测试。
5. 厂商 SDK、闭源下载器、固件签名密钥和真实访问令牌不得进入仓库。

提交信息建议使用 Conventional Commits，例如 `feat: add serial monitor transport`。
