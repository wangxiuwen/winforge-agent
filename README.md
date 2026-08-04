# WinForge Agent

从 macOS/Linux 安全地驱动一台 Windows 构建机，面向需要 Windows 厂商工具链、USB 烧录器和串口设备的嵌入式开发。

WinForge 是一个 Go 单二进制：同一程序在 Windows 上运行 Agent，在 Mac/Linux 上作为客户端使用。它不内置厂商 SDK、下载器、凭据或隐藏远控能力。

## 能力

- HTTPS + 自签名证书指纹固定；
- 256-bit 随机访问令牌；
- 文件访问限制在配置的 workspace root；
- 默认执行参数数组，不经过 shell；
- 实时返回 stdout/stderr，支持超时和取消；
- 原子上传、下载和目录创建；
- Windows 服务安装/卸载；
- Windows/macOS/Linux amd64/arm64 构建。

## Windows：初始化并启动

```powershell
winforge.exe init --config C:\ProgramData\WinForge\config.json `
  --root C:\WinForge --listen 0.0.0.0:9443

winforge.exe serve --config C:\ProgramData\WinForge\config.json
```

`init` 会打印一次配对命令。请通过可信通道复制到 Mac，不要把 token 写进聊天、工单或 Git。

安装为服务（在管理员终端执行）：

```powershell
winforge.exe service install --config C:\ProgramData\WinForge\config.json
```

## Mac：配对并操作

```bash
winforge pair windows-lab \
  --host https://192.0.2.50:9443 \
  --token '<Windows init 输出>' \
  --fingerprint '<Windows init 输出>'

winforge status --profile windows-lab
winforge mkdir --profile windows-lab firmware
winforge upload --profile windows-lab ./firmware.zip firmware/firmware.zip
winforge exec --profile windows-lab --cwd firmware -- powershell.exe -NoProfile -File build.ps1
winforge download --profile windows-lab firmware/out/app.bin ./app.bin
```

需要 shell 语法时必须显式调用 `cmd.exe /C` 或 `powershell.exe -Command`，Agent 不会偷偷拼 shell。

## 安全边界

WinForge 拥有启动进程和读写 workspace 的能力，因此只应部署在受信任网络中，并使用专门的低权限 Windows 账号运行。更多说明见 [SECURITY.md](SECURITY.md)。

## 开发

```bash
go test ./...
go vet ./...
make build
```
