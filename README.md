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
- 局域网 mDNS/DNS-SD 发现，构建机重启换 IP 后自动重新定位；
- Windows/macOS/Linux amd64/arm64 构建。

## Windows：初始化并启动

```powershell
winforge.exe init --config C:\ProgramData\WinForge\config.json `
  --root C:\WinForge --listen 0.0.0.0:9443

winforge.exe serve --config C:\ProgramData\WinForge\config.json
```

配对用 6 位数字，不用搬运长字符串：服务起来后在 Windows 上执行

```powershell
winforge.exe pair-code --config C:\ProgramData\WinForge\config.json
```

它会打印一个 **6 位配对码**（10 分钟内有效、只能用一次、错 5 次作废）。把这 6 个数字
念给对面就行——真正的 token 和证书指纹由两端在配对时自动交换和校验，不需要人经手。

`init` 也会打印长 token 和指纹，那是不方便用 `pair-code` 时的后备方案；一旦打印出来，
就请通过可信通道传递，不要写进聊天、工单或 Git。

安装为服务（在管理员终端执行）：

```powershell
winforge.exe service install --config C:\ProgramData\WinForge\config.json
```

## Mac：配对并操作

```bash
winforge discover                     # 列出局域网内的 Agent

winforge pair windows-lab \
  --instance '<discover 里的实例名>' \
  --code 123456                       # Windows 上 winforge pair-code 打印的 6 位数字

winforge status --profile windows-lab
winforge mkdir --profile windows-lab firmware
winforge upload --profile windows-lab ./firmware.zip firmware/firmware.zip
winforge exec --profile windows-lab --cwd firmware -- powershell.exe -NoProfile -File build.ps1
winforge download --profile windows-lab firmware/out/app.bin ./app.bin
```

固定 IP 也可以：把 `--instance` 换成 `--host https://192.0.2.50:9443`。两个一起给，
则平时走固定地址，连不上时才回退到发现。

不方便用配对码时（比如两端不在同一个局域网），仍可搬运长凭证：
`--token '<init 输出>' --fingerprint '<init 输出>'`，与 `--code` 二选一。

### 6 位数字为什么够安全

配对码只是"人来传递"的那一段，长期凭证仍是 32 字节随机 token，只是不再经人手。
6 位数字本身只有一百万种，所以它同时受三道限制：**10 分钟过期、只能用一次、错 5 次作废**，
在线爆破没有机会。

它还顺手解决了指纹的问题。以前要人把 64 个十六进制字符抄到另一台机器上核对——
抄错了会失败，不抄照样"能用"，于是实际上没人真的核对，防中间人就是一句空话。
现在服务端用配对码作密钥、对自己的证书指纹做 HMAC 返回，客户端拿**握手时实际看到的**
指纹算一遍比对：中间人不知道那 6 位数字，就伪造不出这个证明，配对会直接中止。
人少记一样东西，安全性反而更高。

## 局域网发现

Agent 默认用 mDNS/DNS-SD 公告 `_winforge._tcp`，实例名取主机名，可用 `instance_name` 配置项改，
`"advertise": false` 可完全关闭。公告内容只有实例名、主机名、端口和证书指纹，不含 token 和任何路径。

profile 里记了实例名后，客户端在原地址连不上时会自动在局域网里重新找一次——这正是构建机
重启后 DHCP 换了地址的场景。发现结果按不可信数据处理：候选地址必须先通过固定的证书指纹校验，
再成功调用一次 `status`，才会被写回 profile；指纹对不上的实例直接跳过，token 不会发给它。
指纹本身永远来自 Agent `init` 的带外输出，不从公告里采信。

多播不跨路由，也可能被 AP 的客户端隔离挡掉；这种情况下继续用 `--host`。

需要 shell 语法时必须显式调用 `cmd.exe /C` 或 `powershell.exe -Command`，Agent 不会偷偷拼 shell。

## 安全边界

WinForge 拥有启动进程和读写 workspace 的能力，因此只应部署在受信任网络中，并使用专门的低权限 Windows 账号运行。更多说明见 [SECURITY.md](SECURITY.md)。

## 开发

```bash
go test ./...
go vet ./...
make build
```
