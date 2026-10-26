# WinForge Desk 迭代日志（R 轮）

目标：`winforge desk` = 局域网多机键鼠共享托盘程序，三大能力 **共享（键鼠）/ 传输（消息+文件）/ 控制（互相控制）**，
配对 = 双端确认同一 6 位配对码。分支 `desk`，全部完成后合回 main。

## 功能清单

- [x] R1 核心全链路：UDP 发现 / 双确认码配对（X25519+ECDH 派生 PSK）/ AES-GCM 会话 /
      消息 / 文件传输（SHA-256 校验）/ 控制状态机（take/deny/release）/ 托盘 /
      Wails 桌面窗口 / 浏览器控制台 / go test -race 全绿 / 双实例真进程冒烟通过
- [ ] R2+ Wails 桌面窗口实机启动验证（窗口能开、前端绑定调用、事件到 UI）
- [ ] R2+ 托盘实机验证：图标显示、菜单动作、共享开关勾选态、tooltip 随控制态变化
- [ ] R2+ 真机键鼠共享实测：钩子捕获→对端注入（同机双实例无法测钩子，需真机或驱动级模拟）
- [ ] R2+ 热键 RegisterHotKey 实测（Ctrl+Alt+D 切换）
- [x] R2 断线重连（e2e：强制断开 TCP → 自动重连 → 消息恢复）；杀进程级+控制态不卡键待真机
- [x] R2 同机双实例发现：UDP SO_REUSEADDR（参考 internal/discovery/reuse_windows.go）
- [ ] R2+ `winforge desk autostart on|off`（HKCU Run 注册表，x/sys/windows/registry）
- [ ] R2+ 独立 GUI 构建 winforge-desk.exe（-H=windowsgui，无控制台黑窗；参见 monctl 经验）
- [ ] R2+ 传输增强：取消进行中传输、接收端进度限频、1GB 级大文件
- [ ] R2+ UI 打磨：拖拽文件发送、聊天记录持久化（会话级）、多对端消息隔离、连接状态动效
- [ ] R2+ 控制体验：控制时对端屏幕边缘提示条、光标进入动画、Esc 快捷交还
- [ ] R2+ 多对端拓扑（>2 台）：方位矩阵、链式切换
- [ ] R2+ 性能：输入延迟测量（目标 <30ms 局域网）、移动事件合帧
- [ ] R2+ 打包：Makefile release 增加 winforge-desk-windows 变体、SHA256SUMS
- [ ] R2+ 文档：README 截图位、SECURITY.md 补 desk 章节 threat model

## 每轮规则（给迭代轮）

1. 开工先 `git log --oneline -3` + `git status`：确认在 `desk` 分支、无别人未提交改动；
   发现有未提交的别人改动 → 只提交自己改的文件，绝不 `git add -A` 吸收。
2. 每轮只做清单里 1-2 项，做完必须：`go build ./...` + `go test ./internal/desk/ -count=1 -timeout 180s`
   （涉改共享代码再跑 `go test ./...`）+ 冒烟 `py -3 smoke_desk.py`（涉及传输/协议时）。
3. 本机 vet 对 input_windows.go 的 unsafe.Pointer 告警是 Win32 钩子惯用写法，CI(Linux) 不编译该文件，忽略。
4. `internal/agent` 存量测试在 Windows 本地失败（与 desk 无关，CI Linux 通过为准），不要顺手去修范围外代码。
5. GOPROXY 必须设 `https://mirrors.aliyun.com/goproxy/,direct`（本机 goproxy.cn DNS 不通，7897 代理不常开）。
6. 一轮一个 commit：`desk(R<N>): <做了什么>`，并把本轮记录追加到本文件"轮次记录"。
7. 完成一轮 push 一次：`git push origin desk`。不 force push、不 rebase、不动 main。
8. 用户插话优先于清单顺序；构建失败超过 3 次尝试就记录现状收尾，不硬提交坏状态。

## 轮次记录

### R2（2026-10-26）
- 发现服务改用 net.ListenConfig{Control: reuseControl}：Windows 只设 SO_REUSEADDR，
  unix 加 SO_REUSEPORT（照搬 internal/discovery 的成熟做法）→ 同机双实例可共用
  41832/UDP 互相广播发现。
- 冒烟升级：两个实例都用 41832，新增第 0 步"UDP 广播发现互通"断言，全流程通过
  （发现→配对→会话→消息→3MB 文件→控制状态机）。
- 新增 TestHubReconnectAfterDrop：强制断 TCP → 3s 退避重连 → 重连后消息可达。
- 注意点：ListenPacket 返回 net.PacketConn，要断言成 *net.UDPConn 才有 ReadFromUDP。

### R1（2026-10-26）
- 新增 internal/desk（~3200 行）：proto/crypto/pairing/session/hub(_msg/_ctl/_ui)/discovery/
  transfer/input(_windows/_other)/config/tray/web UI + cmd/winforge `desk` 子命令。
- 安全模型：配对码绑定 ECDH、PSK 会话证明、AES-256-GCM 双向计数器、UI 令牌鉴权、
  下载目录路径约束（穿越/保留名拒绝）、大小上限。
- 测试：crypto(HKDF RFC 向量/MITM 配对码/重放)/transfer(穿越)/discovery/ui(鉴权)/
  config + 双 Hub e2e（配对码一致、消息、3MB 文件、take/deny/release、删对端断连），
  `go test -race` 全绿。
- 真进程双实例冒烟 `smoke_desk.py`：配对码一致→配对→会话→消息→3MB 文件校验→
  关共享拒绝控制→take/release 状态机，全部通过。
- 修掉的坑：会话帧没套外层 [type][len] 头（nonce 被当帧头）；首帧计数器 0 被判重放；
  ToggleShare 持锁调加锁函数死锁；cleanup 重复 close 捕获通道 panic 死锁；
  发起方用 confirm 帧 ID（空）存对端；darwin 托盘 cgo 破坏交叉编译（拆 tray_systray/tray_stub）。
- 已知边界：键鼠钩子/注入仅 Windows 实机可验（本轮未测）；同机 UDP 发现端口冲突未做
  SO_REUSEADDR；`internal/agent` 存量 Windows 测试失败与本模块无关。
