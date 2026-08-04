# Security Policy

## 设计原则

- Agent 不提供匿名模式，也没有默认口令；
- 网络始终使用 TLS，客户端必须固定证书 SHA-256 指纹；
- Bearer token 只保存在配置文件中，日志会对认证头脱敏；
- 文件 API 使用规范化后的相对路径，并拒绝符号链接逃逸；
- 命令默认不经过 shell；
- 服务安装不会绕过 UAC，必须由管理员明确执行；
- 不支持隐藏窗口、键盘记录、屏幕抓取、凭据提取或持久化绕过；
- 默认不开放公网，也不提供 NAT 穿透。

## 部署建议

1. 用专门的非管理员 Windows 用户运行日常 Agent；只有安装 USB 驱动时临时提权。
2. Windows 防火墙只允许开发 Mac 所在的内网地址访问 9443。
3. 每台 Mac 使用独立 Agent 配置；怀疑泄露时重新运行 `rotate-token`。
4. 不把配置文件、token、私钥或固件签名密钥提交到 Git。

## 报告漏洞

请使用 GitHub Security Advisory 私下报告，不要在公开 Issue 中粘贴 token、证书私钥或设备密钥。

